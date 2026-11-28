package recruiters

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"9itchi-email-api/core"
)

const quickEnrichBaseURL = "https://app.quickenrich.io"

// quickEnrichMaxContacts caps the people kept from one page (20 at most), hiring titles first.
const quickEnrichMaxContacts = 10

// QuickEnrich is quickenrich.io: Domain Search (/api/employees/dataset-search) lists up to 20 people of a company
// with their email. Without a title filter it costs a flat 1 credit per page that returns someone, so Find never
// filters by title on the API side (that would be 1 credit per person) and ranks the titles itself. The key goes
// in the Authorization header (Bearer), never in a URL; it is never logged.
type QuickEnrich struct {
	cfg Config
}

var _ core.ContactFinder = (*QuickEnrich)(nil)

func NewQuickEnrich(cfg Config) *QuickEnrich { return &QuickEnrich{cfg: cfg} }

func (e *QuickEnrich) Name() string { return "quickenrich" }

func (e *QuickEnrich) Ready() bool { return len(e.keys()) > 0 }

func (e *QuickEnrich) keys() []string { return e.cfg.keys("quickenrich", e.cfg.QuickEnrichAPIKey) }

func (e *QuickEnrich) base() string {
	if e.cfg.QuickEnrichURL != "" {
		return strings.TrimRight(e.cfg.QuickEnrichURL, "/")
	}
	return quickEnrichBaseURL
}

type quickEnrichPerson struct {
	FirstName             string `json:"first_name"`
	LastName              string `json:"last_name"`
	Title                 string `json:"title"`
	Email                 string `json:"email"`
	EmployeeLinkedin      string `json:"employee_linkedin"`
	EmailVerificationDate string `json:"email_verification_date"`
	CompanyName           string `json:"company_name"`
}

// Find reads the first page of the company's people that have an email (1 credit when someone is listed). It needs
// a domain: none, nothing.
func (e *QuickEnrich) Find(ctx context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	if q.Domain == "" || q.MaxCredits < 1 {
		return &core.ContactLookup{}, nil
	}
	params := url.Values{"company_url": {q.Domain}, "has_email": {"true"}, "page": {"1"}}
	resp, err := e.cfg.send("quickenrich", e.keys(), func(key string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, e.base()+"/api/employees/dataset-search?"+params.Encode(), nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Set("Accept", "application/json")
		}
		return req, err
	})
	if err != nil {
		return &core.ContactLookup{}, fmt.Errorf("%w: quickenrich unreachable", core.ErrPeerUnavailable)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &core.ContactLookup{}, fmt.Errorf("%w: quickenrich key rejected", core.ErrPeerUnavailable)
	case resp.StatusCode == http.StatusPaymentRequired:
		return &core.ContactLookup{}, fmt.Errorf("%w: quickenrich credits exhausted", core.ErrPeerUnavailable)
	case resp.StatusCode == http.StatusTooManyRequests:
		return &core.ContactLookup{}, fmt.Errorf("%w: quickenrich rate limit", core.ErrPeerUnavailable)
	case resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound ||
		resp.StatusCode == http.StatusUnprocessableEntity:
		// A company QuickEnrich does not know: nobody, no credit.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return &core.ContactLookup{Domain: q.Domain}, nil
	case resp.StatusCode != http.StatusOK:
		return &core.ContactLookup{}, fmt.Errorf("%w: quickenrich status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	var out struct {
		Data json.RawMessage `json:"data"`
		Meta struct {
			CreditsUsed *int `json:"credits_used"`
		} `json:"meta"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&out); err != nil {
		return &core.ContactLookup{}, fmt.Errorf("%w: quickenrich reply unreadable", core.ErrPeerUnavailable)
	}
	// data is a list of people, or an empty list / object when nobody was found.
	var people []quickEnrichPerson
	_ = json.Unmarshal(out.Data, &people)
	lookup := &core.ContactLookup{Domain: q.Domain}
	if len(people) > 0 {
		lookup.Credits = 1
	}
	if out.Meta.CreditsUsed != nil && *out.Meta.CreditsUsed >= 0 {
		lookup.Credits = *out.Meta.CreditsUsed
	}
	slices.SortStableFunc(people, func(x, y quickEnrichPerson) int {
		return cmp.Compare(apolloTitleRank(y.Title), apolloTitleRank(x.Title))
	})
	for _, p := range people {
		if len(lookup.Contacts) == quickEnrichMaxContacts {
			break
		}
		email := strings.ToLower(strings.TrimSpace(p.Email))
		_, host, ok := strings.Cut(email, "@")
		if !ok || host == "" || freeMail[host] {
			continue
		}
		if lookup.Company == "" {
			lookup.Company = p.CompanyName
		}
		c := core.Contact{
			Email: email, FirstName: p.FirstName, LastName: p.LastName, Title: p.Title,
			Department: apolloDepartment(p.Title), LinkedInURL: linkedinURL(p.EmployeeLinkedin), Confidence: 60,
		}
		if strings.TrimSpace(p.EmailVerificationDate) != "" {
			c.Verification, c.Confidence = "valid", 85
		}
		lookup.Contacts = append(lookup.Contacts, c)
	}
	return lookup, nil
}
