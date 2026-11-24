// Package schedule is the HTTP client for 9itchi-schedule-api. Base URL comes from env, never hard-coded.
package schedule

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

var _ core.Scheduler = (*Client)(nil)

// ScheduleEmail registers an email_dispatch task: schedule-api calls back POST /internal/emails/:id/dispatch.
func (c *Client) ScheduleEmail(ctx context.Context, emailID string, runAt time.Time) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodPost, URL: httpx.URL(c.BaseURL, "/internal/schedules"), APIKey: c.APIKey,
		Body: map[string]any{"type": "email_dispatch", "payload": map[string]string{"email_id": emailID}, "run_at": runAt.UTC()},
	}, &out)
	return out.ID, err
}

// Cancel calls DELETE /internal/schedules/:id.
func (c *Client) Cancel(ctx context.Context, taskID string) error {
	return httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodDelete, URL: httpx.URL(c.BaseURL, "/internal/schedules/"+url.PathEscape(taskID)), APIKey: c.APIKey,
	}, nil)
}
