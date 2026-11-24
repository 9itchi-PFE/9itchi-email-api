// Package tracker is the HTTP client for 9itchi-tracker-api. Base URL comes from env, never hard-coded.
package tracker

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
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

var _ core.Tracker = (*Client)(nil)

// Register calls POST /internal/tracking and returns the tracking id and the pixel path.
func (c *Client) Register(ctx context.Context, emailID, userID string) (string, string, error) {
	var out struct {
		TrackingID   string `json:"tracking_id"`
		PixelURLPath string `json:"pixel_url_path"`
	}
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodPost, URL: httpx.URL(c.BaseURL, "/internal/tracking"), APIKey: c.APIKey,
		Body: map[string]string{"email_id": emailID, "user_id": userID},
	}, &out)
	return out.TrackingID, out.PixelURLPath, err
}
