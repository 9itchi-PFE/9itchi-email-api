package recruiters

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"9itchi-email-api/core"
)

const hunterBaseURL = "https://api.hunter.io"

var defaultHTTP = &http.Client{Timeout: 20 * time.Second}

// Hunter is hunter.io: Domain Search finds the people of a company (1 credit per search that returns someone),
// Discover finds companies (free). The key goes in the X-API-KEY header, never in a URL; it is never logged.
type Hunter struct {
	cfg Config
}

var (
	_ core.ContactFinder     = (*Hunter)(nil)
	_ core.CompanyDiscoverer = (*Hunter)(nil)
)

func NewHunter(cfg Config) *Hunter { return &Hunter{cfg: cfg} }

func (h *Hunter) Name() string { return "hunter" }

func (h *Hunter) Ready() bool { return h.key() != "" }

func (h *Hunter) key() string { return h.cfg.key("hunter", h.cfg.HunterAPIKey) }

func (h *Hunter) base() string {
	if h.cfg.HunterURL != "" {
		return strings.TrimRight(h.cfg.HunterURL, "/")
	}
	return hunterBaseURL
}

type hunterEmail struct {
	Value        string `json:"value"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Position     string `json:"position"`
	Department   string `json:"department"`
	Linkedin     string `json:"linkedin"`
	Confidence   int    `json:"confidence"`
	Verification struct {
		Status string `json:"status"`
	} `json:"verification"`
}

type hunterDomainData struct {
	Domain       string        `json:"domain"`
	Organization string        `json:"organization"`
	Emails       []hunterEmail `json:"emails"`
}

// Find runs a Domain Search for the HR people of the company (by domain, else by name); when it lists nobody,
// one wider search of the whole domain (small companies rarely list HR staff), credits allowing.
func (h *Hunter) Find(ctx context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	target := url.Values{}
	switch {
	case q.Domain != "":
		target.Set("domain", q.Domain)
	case q.Company != "":
		target.Set("company", q.Company)
	default:
		return &core.ContactLookup{}, nil
	}
	if q.MaxCredits < 1 {
		return &core.ContactLookup{}, nil
	}
	params := url.Values{"department": {"hr"}, "type": {"personal"}, "limit": {"10"}}
	for k, v := range target {
		params[k] = v
	}
	data, credits, err := h.domainSearch(ctx, params)
	if err != nil || data == nil {
		return &core.ContactLookup{Credits: credits}, err
	}
	out := &core.ContactLookup{Credits: credits, Domain: strings.ToLower(data.Domain), Company: data.Organization}
	if q.Domain != "" {
		out.Domain = q.Domain // a domain given wins over Hunter's guess
	}
	if len(data.Emails) == 0 && out.Domain != "" && q.MaxCredits > credits {
		wider, more, err := h.domainSearch(ctx, url.Values{"domain": {out.Domain}, "limit": {"10"}})
		out.Credits += more
		if err != nil {
			return out, err
		}
		if wider != nil {
			data.Emails = wider.Emails
		}
	}
	for _, e := range data.Emails {
		if e.Value == "" {
			continue
		}
		out.Contacts = append(out.Contacts, core.Contact{
			Email: strings.ToLower(e.Value), FirstName: e.FirstName, LastName: e.LastName, Title: e.Position,
			Department: hunterDepartment(e.Department), LinkedInURL: linkedinURL(e.Linkedin),
			Confidence: min(max(e.Confidence, 0), 100), Verification: verification(e.Verification.Status),
		})
	}
	return out, nil
}

// domainSearch is one Domain Search call. A company Hunter does not know (400, 404) is nil data, not an error. The
// credits are Hunter's X-Credits-Used header when sent, else 1 when the search returned someone.
func (h *Hunter) domainSearch(ctx context.Context, params url.Values) (*hunterDomainData, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.base()+"/v2/domain-search?"+params.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("X-API-KEY", h.key())
	resp, err := h.cfg.client().Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: hunter unreachable", core.ErrPeerUnavailable)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, 0, fmt.Errorf("%w: hunter key rejected", core.ErrPeerUnavailable)
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, 0, fmt.Errorf("%w: hunter rate limit", core.ErrPeerUnavailable)
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, 0, nil
	case resp.StatusCode != http.StatusOK:
		return nil, 0, fmt.Errorf("%w: hunter status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	var out struct {
		Data hunterDomainData `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, 0, fmt.Errorf("%w: hunter reply unreadable", core.ErrPeerUnavailable)
	}
	credits := 0
	if len(out.Data.Emails) > 0 {
		credits = 1
	}
	if n, err := strconv.Atoi(resp.Header.Get("X-Credits-Used")); err == nil && n >= 0 {
		credits = n
	}
	return &out.Data, credits, nil
}

// Discover asks Hunter's Discover API (free) for companies whose keywords match any of the terms, headquartered in
// the country (and city) asked for.
func (h *Hunter) Discover(ctx context.Context, q core.CompanyQuery) ([]core.Company, error) {
	if len(q.Terms) == 0 {
		return nil, nil
	}
	body := map[string]any{
		"keywords": map[string]any{"include": q.Terms, "match": "any"},
		"limit":    min(max(q.Limit, 1), 100),
	}
	if q.Country != "" {
		loc := map[string]string{"country": q.Country}
		if q.City != "" {
			loc["city"] = q.City
		}
		body["headquarters_location"] = map[string]any{"include": []map[string]string{loc}}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base()+"/v2/discover", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-KEY", h.key())
	resp, err := h.cfg.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: hunter unreachable", core.ErrPeerUnavailable)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("%w: hunter discover status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	var out struct {
		Data []struct {
			Domain       string `json:"domain"`
			Organization string `json:"organization"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: hunter reply unreadable", core.ErrPeerUnavailable)
	}
	companies := make([]core.Company, 0, len(out.Data))
	for _, d := range out.Data {
		domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(d.Domain)), "www.")
		if domain == "" {
			continue
		}
		companies = append(companies, core.Company{Domain: domain, Name: d.Organization, Country: q.Country, City: q.City})
		if q.Limit > 0 && len(companies) == q.Limit {
			break
		}
	}
	return companies, nil
}

// hunterDepartment maps Hunter's departments to the pool's.
func hunterDepartment(d string) string {
	switch d {
	case "hr", "management", "executive":
		return d
	case "it":
		return "engineering"
	}
	return "other"
}

// verification keeps the statuses the pool stores; any other (webmail, disposable...) counts as unknown.
func verification(s string) string {
	switch s {
	case "valid", "accept_all", "unknown", "invalid", "":
		return s
	}
	return "unknown"
}

func linkedinURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return s
	}
	return ""
}
