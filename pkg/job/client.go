// Package job is the HTTP client for 9itchi-job-api. Base URL comes from env, never hard-coded.
package job

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/pkg/httpx"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTP:    &http.Client{Timeout: 50 * time.Second},
	}
}

var (
	_ core.JobSource          = (*Client)(nil)
	_ core.OfferReader        = (*Client)(nil)
	_ core.OfferCompanySource = (*Client)(nil)
)

// ReadOffer calls POST /internal/offers/read: the offer behind a link the student found elsewhere, or in the text
// they pasted (then nothing is fetched). An offer job-api cannot read is core.ErrPeerUnprocessable.
func (c *Client) ReadOffer(ctx context.Context, rawURL, text string) (*core.OfferText, error) {
	var out struct {
		Source      string `json:"source"`
		Title       string `json:"title"`
		Company     string `json:"company"`
		Location    string `json:"location"`
		Description string `json:"description"`
		ApplyEmail  string `json:"apply_email"`
	}
	body := struct {
		URL  string `json:"url"`
		Text string `json:"text,omitempty"`
	}{URL: rawURL, Text: text}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodPost, URL: httpx.URL(c.BaseURL, "/internal/offers/read"), APIKey: c.APIKey, Body: body,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &core.OfferText{
		Source: out.Source, Title: out.Title, Company: out.Company, Description: out.Description, ApplyEmail: out.ApplyEmail,
	}, nil
}

// Job calls GET /jobs/:id on behalf of the user.
func (c *Client) Job(ctx context.Context, bearer, id string) (*core.JobInfo, error) {
	var out struct {
		Title       string   `json:"title"`
		Company     string   `json:"company"`
		Description string   `json:"description"`
		Skills      []string `json:"skills"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodGet, URL: httpx.URL(c.BaseURL, "/jobs/"+url.PathEscape(id)), Bearer: bearer,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &core.JobInfo{Title: out.Title, Company: out.Company, Description: out.Description, Skills: out.Skills}, nil
}

// ApplyInfo calls GET /internal/jobs/:id: the offer with the address it takes applications at.
func (c *Client) ApplyInfo(ctx context.Context, id string) (*core.JobInfo, error) {
	var out struct {
		Title       string `json:"title"`
		Company     string `json:"company"`
		Description string `json:"description"`
		ApplyEmail  string `json:"apply_email"`
		Open        bool   `json:"open"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodGet, URL: httpx.URL(c.BaseURL, "/internal/jobs/"+url.PathEscape(id)), APIKey: c.APIKey,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &core.JobInfo{Title: out.Title, Company: out.Company, Description: out.Description, ApplyEmail: out.ApplyEmail, Open: out.Open}, nil
}

// OfferCompanies calls POST /internal/companies/search: the companies of published, still-open offers that match
// the terms (full text over title and description), with the domain job-api could tell (never an apply address).
func (c *Client) OfferCompanies(ctx context.Context, terms []string, country string, limit int) ([]core.OfferCompany, error) {
	var out struct {
		Items []struct {
			Company      string   `json:"company"`
			Domain       string   `json:"domain"`
			Description  string   `json:"description"`
			City         string   `json:"city"`
			Country      string   `json:"country"`
			Titles       []string `json:"titles"`
			MatchedTerms []string `json:"matched_terms"`
		} `json:"items"`
	}
	body := struct {
		Terms   []string `json:"terms"`
		Country string   `json:"country"`
		Limit   int      `json:"limit"`
	}{Terms: terms, Country: country, Limit: limit}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodPost, URL: httpx.URL(c.BaseURL, "/internal/companies/search"), APIKey: c.APIKey, Body: body,
	}, &out)
	if err != nil {
		return nil, err
	}
	items := make([]core.OfferCompany, 0, len(out.Items))
	for _, it := range out.Items {
		items = append(items, core.OfferCompany{
			Company: it.Company, Domain: it.Domain, Description: it.Description, City: it.City, Country: it.Country,
			Titles: it.Titles, MatchedTerms: it.MatchedTerms,
		})
	}
	return items, nil
}
