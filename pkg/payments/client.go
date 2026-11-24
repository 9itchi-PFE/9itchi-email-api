// Package payments is the HTTP client for 9itchi-payments-api. Base URL comes from env, never hard-coded.
package payments

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
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

var _ core.QuotaSource = (*Client)(nil)

// Limit calls GET /internal/quotas/:userId (active plan, else Free).
func (c *Client) Limit(ctx context.Context, userID, key string) (int, bool, error) {
	var out struct {
		Quotas map[string]int `json:"quotas"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodGet, URL: httpx.URL(c.BaseURL, "/internal/quotas/"+url.PathEscape(userID)), APIKey: c.APIKey,
	}, &out)
	if err != nil {
		return 0, false, err
	}
	limit, ok := out.Quotas[key]
	return limit, ok, nil
}
