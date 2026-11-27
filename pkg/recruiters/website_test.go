package recruiters

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// siteHarness serves a fake company site; every host name reaches it.
func siteHarness(t *testing.T, pages map[string]string, status map[string]int) (*Website, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		if code, ok := status[r.URL.Path]; ok {
			if loc, ok := pages[r.URL.Path]; ok && code >= 300 && code < 400 {
				w.Header().Set("Location", loc)
			}
			w.WriteHeader(code)
			return
		}
		page, ok := pages[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)
	addr := srv.Listener.Addr().String()
	client := &http.Client{
		Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	w := &Website{client: client, scheme: "http", checkHost: func(context.Context, string) error { return nil }}
	return w, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestWebsiteFindsRecruitingMailboxes(t *testing.T) {
	w, seen := siteHarness(t, map[string]string{
		"/robots.txt": "User-agent: Googlebot\nDisallow: /\n\nUser-agent: *\nDisallow: /contact\nDisallow: /jobs$\n",
		"/":           `<a href="mailto:contact@acme.tn">x</a> <p>info@other.com, emploi@careers.acme.tn, rh&#64;acme.tn</p> evil@acme.tn.evil.com`,
		"/contact":    "recrutement@acme.tn",
		"/careers":    "Écrivez à RH@acme.tn ou jobs@acme.tn",
	}, nil)
	got, err := w.Find(context.Background(), core.ContactQuery{Domain: "www.acme.tn"})
	require.NoError(t, err)
	emails := []string{}
	for _, c := range got.Contacts {
		emails = append(emails, c.Email)
		assert.Equal(t, "hr", c.Department)
		assert.Equal(t, 50, c.Confidence)
	}
	assert.Equal(t, []string{"rh@acme.tn", "jobs@acme.tn", "emploi@careers.acme.tn", "contact@acme.tn"}, emails,
		"same domain only, best mailbox first, each once")
	assert.Equal(t, "Service RH", got.Contacts[0].FirstName)
	assert.Equal(t, "acme.tn", got.Domain)
	paths := seen()
	assert.NotContains(t, paths, "/contact", "robots.txt disallows it")
	assert.NotContains(t, paths, "/contact-us", "robots.txt disallows it (prefix)")
	assert.NotContains(t, paths, "/jobs", "a wildcard rule is read as its prefix")
	assert.LessOrEqual(t, len(paths)-1, websiteMaxPages, "robots.txt plus at most 6 pages")
}

func TestWebsiteHonorsRobots(t *testing.T) {
	w, seen := siteHarness(t, map[string]string{"/robots.txt": "User-agent: *\nDisallow: /\n", "/": "rh@acme.tn"}, nil)
	got, err := w.Find(context.Background(), core.ContactQuery{Domain: "acme.tn"})
	require.NoError(t, err)
	assert.Empty(t, got.Contacts)
	assert.Equal(t, []string{"/robots.txt"}, seen(), "nothing read when every path is disallowed")

	w, seen = siteHarness(t, map[string]string{"/": "rh@acme.tn"}, map[string]int{"/robots.txt": http.StatusInternalServerError})
	_, err = w.Find(context.Background(), core.ContactQuery{Domain: "acme.tn"})
	assert.ErrorIs(t, err, core.ErrPeerUnavailable, "robots.txt error: the site is skipped")
	assert.Equal(t, []string{"/robots.txt"}, seen())

	w, _ = siteHarness(t, map[string]string{"/": "rh@acme.tn"}, nil)
	got, err = w.Find(context.Background(), core.ContactQuery{Domain: "acme.tn"})
	require.NoError(t, err, "no robots.txt: everything allowed")
	require.Len(t, got.Contacts, 1)
}

func TestWebsiteStaysOnTheSite(t *testing.T) {
	w, seen := siteHarness(t, map[string]string{"/": "https://evil.example/steal", "/careers": "/jobs", "/jobs": "rh@acme.tn"},
		map[string]int{"/": http.StatusFound, "/careers": http.StatusMovedPermanently})
	got, err := w.Find(context.Background(), core.ContactQuery{Domain: "acme.tn"})
	require.NoError(t, err)
	require.Len(t, got.Contacts, 1, "a redirect within the site is followed")
	assert.Contains(t, seen(), "/jobs")

	w, _ = siteHarness(t, nil, nil)
	got, err = w.Find(context.Background(), core.ContactQuery{Domain: "127.0.0.1"})
	require.NoError(t, err)
	assert.Empty(t, got.Contacts, "an IP literal is not a host name")
}

func TestWebsiteRejectsPrivateAddresses(t *testing.T) {
	for ip, public := range map[string]bool{
		"8.8.8.8": true, "2a00:1450:4007::1": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false,
		"172.16.0.1": false, "169.254.169.254": false, "100.64.0.1": false, "0.0.0.0": false, "::1": false, "fe80::1": false,
		"fd00::1": false, "::ffff:127.0.0.1": false,
	} {
		assert.Equal(t, public, publicIP(net.ParseIP(ip)), ip)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("rh@acme.tn")) }))
	defer srv.Close()
	_, err := safeClient().Get(srv.URL)
	require.Error(t, err, "the safe client never connects to a loopback address")
	assert.Contains(t, err.Error(), "non-public")

	assert.Error(t, publicHost(context.Background(), "localhost"), "not a public host name")
}

func TestPageEmails(t *testing.T) {
	assert.Equal(t, []string{"rh@acme.tn", "jobs@eu.acme.tn"},
		pageEmails("RH@acme.tn x@notacme.tn jobs@eu.acme.tn y@acme.tn.evil.com", "acme.tn"))
}
