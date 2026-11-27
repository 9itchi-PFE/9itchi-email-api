package recruiters

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"9itchi-email-api/core"
)

const apolloBaseURL = "https://api.apollo.io"

// apolloTitles filter the people search: recruiters first, then those who decide in a small company.
var apolloTitles = []string{"recruiter", "technical recruiter", "talent acquisition", "hr manager", "human resources",
	"people partner", "founder", "cto"}

// apolloRevealsPerCompany caps the paid reveals for one company.
const apolloRevealsPerCompany = 2

// freeMail are personal mailboxes, never a company's address.
var freeMail = map[string]bool{
	"gmail.com": true, "googlemail.com": true, "yahoo.com": true, "yahoo.fr": true, "hotmail.com": true, "hotmail.fr": true,
	"outlook.com": true, "outlook.fr": true, "live.com": true, "icloud.com": true, "gmx.com": true, "gmx.fr": true,
	"proton.me": true, "protonmail.com": true, "topnet.tn": true,
}

// Apollo is apollo.io: the people search is free and returns no address; revealing one person's work address
// (people/match, reveal_personal_emails=false) costs 1 credit. The key goes in the x-api-key header, never logged.
type Apollo struct {
	cfg Config
}

var _ core.ContactFinder = (*Apollo)(nil)

func NewApollo(cfg Config) *Apollo { return &Apollo{cfg: cfg} }

func (a *Apollo) Name() string { return "apollo" }

func (a *Apollo) Ready() bool { return a.key() != "" }

func (a *Apollo) key() string { return a.cfg.key("apollo", a.cfg.ApolloAPIKey) }

func (a *Apollo) base() string {
	if a.cfg.ApolloURL != "" {
		return strings.TrimRight(a.cfg.ApolloURL, "/")
	}
	return apolloBaseURL
}

type apolloPerson struct {
	ID          string `json:"id"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Title       string `json:"title"`
	HasEmail    bool   `json:"has_email"`
	Email       string `json:"email"`
	EmailStatus string `json:"email_status"`
	LinkedinURL string `json:"linkedin_url"`
}

// Find searches the people of the domain with a hiring title, then reveals the best ones (at most 2, within the
// credits left). Apollo needs a domain: none, nothing.
func (a *Apollo) Find(ctx context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	if q.Domain == "" || q.MaxCredits < 1 {
		return &core.ContactLookup{}, nil
	}
	params := url.Values{"q_organization_domains_list[]": {q.Domain}, "person_titles[]": apolloTitles, "per_page": {"25"}}
	var found struct {
		People []apolloPerson `json:"people"`
	}
	if err := a.post(ctx, "/api/v1/mixed_people/api_search", params, &found); err != nil {
		return &core.ContactLookup{}, err
	}
	people := make([]apolloPerson, 0, len(found.People))
	for _, p := range found.People {
		if p.ID != "" && p.HasEmail {
			people = append(people, p)
		}
	}
	slices.SortStableFunc(people, func(x, y apolloPerson) int { return cmp.Compare(apolloTitleRank(y.Title), apolloTitleRank(x.Title)) })
	out := &core.ContactLookup{Domain: q.Domain}
	for _, p := range people {
		if out.Credits >= min(apolloRevealsPerCompany, q.MaxCredits) {
			break
		}
		var match struct {
			Person apolloPerson `json:"person"`
		}
		err := a.post(ctx, "/api/v1/people/match", url.Values{"id": {p.ID}, "reveal_personal_emails": {"false"}}, &match)
		if err != nil {
			var charged *apolloCharged
			if errors.As(err, &charged) {
				out.Credits++ // Apollo may have charged (5xx): keep the credit counted
			}
			return out, err
		}
		out.Credits++
		email := strings.ToLower(strings.TrimSpace(match.Person.Email))
		local, host, ok := strings.Cut(email, "@")
		if !ok || strings.HasPrefix(local, "email_not_unlocked") || freeMail[host] {
			continue
		}
		c := core.Contact{
			Email: email, FirstName: cmp.Or(match.Person.FirstName, p.FirstName), LastName: match.Person.LastName,
			Title: cmp.Or(match.Person.Title, p.Title), LinkedInURL: linkedinURL(match.Person.LinkedinURL), Confidence: 70,
			Department: apolloDepartment(cmp.Or(match.Person.Title, p.Title)),
		}
		if match.Person.EmailStatus == "verified" {
			c.Verification, c.Confidence = "valid", 90
		}
		out.Contacts = append(out.Contacts, c)
	}
	return out, nil
}

// apolloCharged is a server error after which Apollo may have charged the reveal.
type apolloCharged struct{ status int }

func (e *apolloCharged) Error() string { return fmt.Sprintf("apollo status %d", e.status) }

func (e *apolloCharged) Unwrap() error { return core.ErrPeerUnavailable }

func (a *Apollo) post(ctx context.Context, path string, params url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.base()+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", a.key())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := a.cfg.client().Do(req)
	if err != nil {
		return fmt.Errorf("%w: apollo unreachable", core.ErrPeerUnavailable)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 500:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return &apolloCharged{status: resp.StatusCode}
	default: // 401/403 key or plan, 402 out of credits, 422, 429: nothing charged
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("%w: apollo status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return fmt.Errorf("%w: apollo reply unreadable", core.ErrPeerUnavailable)
	}
	return nil
}

// apolloTitleRank puts recruiters before founders and technical leads.
func apolloTitleRank(title string) int {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "recruit"), strings.Contains(t, "talent"), strings.Contains(t, "human resources"),
		strings.Contains(t, "hr "), strings.HasPrefix(t, "hr"), strings.Contains(t, "people"):
		return 2
	case strings.Contains(t, "founder"), strings.Contains(t, "cto"):
		return 1
	}
	return 0
}

func apolloDepartment(title string) string {
	t := strings.ToLower(title)
	switch {
	case apolloTitleRank(title) == 2:
		return "hr"
	case strings.Contains(t, "founder"), strings.Contains(t, "ceo"):
		return "executive"
	case strings.Contains(t, "cto"), strings.Contains(t, "engineering"):
		return "engineering"
	}
	return "other"
}
