// Package ai is the HTTP client for 9itchi-ai-api. Base URL comes from env, never hard-coded.
package ai

import (
	"context"
	"net/http"
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

var _ core.Drafter = (*Client)(nil)

// Draft calls POST /ai/emails/draft on behalf of the user.
func (c *Client) Draft(ctx context.Context, bearer string, in core.DraftInput) (string, string, error) {
	var out struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodPost, URL: httpx.URL(c.BaseURL, "/ai/emails/draft"), Bearer: bearer,
		Body: map[string]string{
			"job_title": in.JobTitle, "company": in.Company, "job_description": in.JobDescription,
			"candidate_summary": in.CandidateSummary, "language": in.Language, "tone": in.Tone,
		},
	}, &out)
	return out.Subject, out.Body, err
}
