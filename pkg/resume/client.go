// Package resume is the HTTP client for 9itchi-resume-api. Base URL comes from env, never hard-coded.
package resume

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

var _ core.ResumeSource = (*Client)(nil)

// maxPDFBytes is the largest attachment accepted from the resume service (Gmail allows 25 MB per email).
const maxPDFBytes = 10 << 20

// Summary calls GET /resumes/:id on behalf of the user and flattens the CV to a few lines of text.
func (c *Client) Summary(ctx context.Context, bearer, id string) (string, error) {
	var out struct {
		Title   string `json:"title"`
		Content struct {
			Contact struct {
				Name     string `json:"name"`
				Headline string `json:"headline"`
			} `json:"contact"`
			Summary    string   `json:"summary"`
			Skills     []string `json:"skills"`
			Experience []struct {
				Company string `json:"company"`
				Title   string `json:"title"`
			} `json:"experience"`
			Education []struct {
				School string `json:"school"`
				Degree string `json:"degree"`
			} `json:"education"`
		} `json:"content"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodGet, URL: httpx.URL(c.BaseURL, "/resumes/"+url.PathEscape(id)), Bearer: bearer,
	}, &out)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	k := out.Content
	fmt.Fprintf(&b, "Name: %s\nHeadline: %s\nSummary: %s\n", k.Contact.Name, k.Contact.Headline, k.Summary)
	for _, e := range k.Education {
		fmt.Fprintf(&b, "Education: %s, %s\n", e.Degree, e.School)
	}
	for _, e := range k.Experience {
		fmt.Fprintf(&b, "Experience: %s at %s\n", e.Title, e.Company)
	}
	if len(k.Skills) > 0 {
		fmt.Fprintf(&b, "Skills: %s\n", strings.Join(k.Skills, ", "))
	}
	return b.String(), nil
}

// PDF calls GET /internal/resumes/:id/pdf: the CV as it is now. It returns nil when the CV does not exist.
func (c *Client) PDF(ctx context.Context, id string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, httpx.URL(c.BaseURL, "/internal/resumes/"+url.PathEscape(id)+"/pdf"), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("%w: status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPDFBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxPDFBytes {
		return nil, nil
	}
	return data, nil
}
