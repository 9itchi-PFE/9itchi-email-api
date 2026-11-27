package recruiters

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"9itchi-email-api/core"
)

const (
	websiteMaxPages  = 6
	websitePageBytes = 512 << 10
	websitePageTime  = 5 * time.Second
	websiteMaxHops   = 3
	websiteUserAgent = "Mozilla/5.0 (compatible; 9itchiBot/1.0)"
)

// websitePaths are the pages read, home first.
var websitePaths = []string{"/", "/contact", "/contact-us", "/nous-contacter", "/careers", "/carrieres", "/jobs", "/recrutement"}

// websiteMailboxes are the local parts kept, best first, with the label the person gets.
var websiteMailboxes = []struct{ local, label, title string }{
	{"rh", "Service RH", "Ressources humaines"},
	{"recrutement", "Service recrutement", "Recrutement"},
	{"recruitment", "Service recrutement", "Recrutement"},
	{"jobs", "Service carrières", "Recrutement"},
	{"careers", "Service carrières", "Recrutement"},
	{"talent", "Service recrutement", "Talent acquisition"},
	{"hr", "Service RH", "Ressources humaines"},
	{"emploi", "Service carrières", "Recrutement"},
	{"contact", "Contact", "Contact général"},
}

var (
	// hostRe: plain DNS names only (no port, userinfo, IP literal or underscore).
	hostRe  = regexp.MustCompile(`^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+(?:[a-z]{2,24}|xn--[a-z0-9-]{2,20})$`)
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,24}`)
	// errNotPublic: the address is private, loopback, link-local... (SSRF guard).
	errNotPublic = errors.New("non-public address")
)

// Website reads the company's own site (free): home, contact and careers pages, honoring robots.txt, and keeps
// the recruiting mailboxes on the company's domain. Only public hosts are reached, checked again at connect time.
type Website struct {
	client    *http.Client
	scheme    string
	checkHost func(ctx context.Context, host string) error
}

var _ core.ContactFinder = (*Website)(nil)

func NewWebsite() *Website {
	return &Website{client: safeClient(), scheme: "https", checkHost: publicHost}
}

func (w *Website) Name() string { return "website" }

// Ready: the website needs no key.
func (w *Website) Ready() bool { return true }

func (w *Website) Find(ctx context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(q.Domain)), "www.")
	if domain == "" {
		return &core.ContactLookup{}, nil
	}
	if !hostRe.MatchString(domain) || len(domain) > 253 {
		return &core.ContactLookup{Domain: domain}, nil
	}
	if err := w.checkHost(ctx, domain); err != nil {
		return nil, fmt.Errorf("%w: website host: %v", core.ErrPeerUnavailable, err)
	}
	status, robots, err := w.fetch(ctx, domain, "/robots.txt")
	if err != nil || status >= 500 || status == http.StatusTooManyRequests {
		return nil, fmt.Errorf("%w: robots.txt unavailable", core.ErrPeerUnavailable)
	}
	var disallow []string
	if status == http.StatusOK {
		disallow = robotsDisallow(robots)
	}
	var emails []string
	pages, failed := 0, 0
	for _, path := range websitePaths {
		if pages >= websiteMaxPages {
			break
		}
		if disallowed(path, disallow) {
			continue
		}
		pages++
		status, body, err := w.fetch(ctx, domain, path)
		if err != nil {
			failed++
			continue
		}
		if status == http.StatusOK {
			emails = append(emails, pageEmails(body, domain)...)
		}
	}
	if pages > 0 && failed == pages {
		return nil, fmt.Errorf("%w: website unreachable", core.ErrPeerUnavailable)
	}
	return &core.ContactLookup{Domain: domain, Contacts: websiteContacts(emails)}, nil
}

// fetch GETs one page of the site, following at most 3 redirects within the site (domain or www.domain), within 5 s,
// reading at most 512 KB.
func (w *Website) fetch(ctx context.Context, domain, path string) (int, string, error) {
	ctx, cancel := context.WithTimeout(ctx, websitePageTime)
	defer cancel()
	target := w.scheme + "://" + domain + path
	for hop := 0; hop <= websiteMaxHops; hop++ {
		u, err := url.Parse(target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
			return 0, "", errors.New("bad url")
		}
		host := strings.ToLower(u.Hostname())
		if host != domain && host != "www."+domain {
			return 0, "", errors.New("redirect off site")
		}
		if hop > 0 {
			if err := w.checkHost(ctx, host); err != nil {
				return 0, "", err
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		if err != nil {
			return 0, "", err
		}
		req.Header.Set("User-Agent", websiteUserAgent)
		resp, err := w.client.Do(req)
		if err != nil {
			return 0, "", err
		}
		if loc := resp.Header.Get("Location"); resp.StatusCode >= 300 && resp.StatusCode < 400 && loc != "" {
			resp.Body.Close()
			next, err := u.Parse(loc)
			if err != nil {
				return 0, "", err
			}
			target = next.String()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, websitePageBytes))
		resp.Body.Close()
		if err != nil {
			return 0, "", err
		}
		return resp.StatusCode, string(body), nil
	}
	return 0, "", errors.New("too many redirects")
}

// robotsDisallow is the Disallow rules of the groups for every robot ("*") or for 9itchiBot. Wildcards are cut to
// the prefix before them (stricter than the standard, never looser).
func robotsDisallow(robots string) []string {
	var out []string
	applies, inAgents := false, false
	sc := bufio.NewScanner(strings.NewReader(robots))
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		switch key {
		case "user-agent":
			if !inAgents {
				applies = false
			}
			inAgents = true
			agent := strings.ToLower(value)
			if agent == "*" || strings.Contains(agent, "9itchibot") {
				applies = true
			}
		case "disallow":
			inAgents = false
			if !applies || value == "" {
				continue
			}
			if i := strings.IndexAny(value, "*$"); i >= 0 {
				value = value[:i]
			}
			if value == "" {
				value = "/"
			}
			out = append(out, value)
		default:
			inAgents = false
		}
	}
	return out
}

func disallowed(path string, rules []string) bool {
	for _, r := range rules {
		if strings.HasPrefix(path, r) {
			return true
		}
	}
	return false
}

// pageEmails are the addresses of a page on the domain (or one of its subdomains), lower case.
func pageEmails(page, domain string) []string {
	var out []string
	for _, e := range emailRe.FindAllString(html.UnescapeString(page), -1) {
		e = strings.ToLower(e)
		_, host, _ := strings.Cut(e, "@")
		host = strings.TrimPrefix(host, "www.")
		if host == domain || strings.HasSuffix(host, "."+domain) {
			out = append(out, e)
		}
	}
	return out
}

// websiteContacts keeps the recruiting mailboxes found, best first, each once.
func websiteContacts(emails []string) []core.Contact {
	var out []core.Contact
	seen := map[string]bool{}
	for _, m := range websiteMailboxes {
		for _, e := range emails {
			local, _, _ := strings.Cut(e, "@")
			if local != m.local || seen[e] {
				continue
			}
			seen[e] = true
			out = append(out, core.Contact{Email: e, FirstName: m.label, Title: m.title, Department: "hr", Confidence: 50})
		}
	}
	return out
}

// publicHost resolves the host and fails unless every address is public.
func publicHost(ctx context.Context, host string) error {
	if !hostRe.MatchString(host) {
		return errors.New("not a host name")
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return err
	}
	if len(addrs) == 0 {
		return errors.New("no address")
	}
	for _, a := range addrs {
		if !publicIP(a.IP) {
			return errNotPublic
		}
	}
	return nil
}

var reservedNets = func() []*net.IPNet {
	var out []*net.IPNet
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96"} {
		_, n, _ := net.ParseCIDR(cidr)
		out = append(out, n)
	}
	return out
}()

// publicIP is true for a globally routable unicast address (not private, loopback, link-local, CGNAT, reserved).
func publicIP(ip net.IP) bool {
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	return !slices.ContainsFunc(reservedNets, func(n *net.IPNet) bool { return n.Contains(ip) })
}

// safeClient connects only to public addresses, checked on the address actually dialed (DNS rebinding), never
// through a proxy, and does not follow redirects itself (fetch checks each hop).
func safeClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: websitePageTime,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip == nil || !publicIP(ip) {
				return errNotPublic
			}
			return nil
		},
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: nil, DialContext: dialer.DialContext, TLSHandshakeTimeout: websitePageTime,
			ResponseHeaderTimeout: websitePageTime, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
