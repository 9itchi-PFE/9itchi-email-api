// Package statistics is the HTTP client for the admin credit tracker of 9itchi-statistics-api: it reports what a
// provider consumed and fetches the API token an admin set. Base URL comes from env, never hard-coded.
package statistics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const tokenTTL = time.Minute

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client

	mu     sync.Mutex
	tokens map[string]cachedToken
}

type cachedToken struct {
	value   string
	expires time.Time
}

// New returns a client; an empty baseURL makes it a no-op.
func New(baseURL, apiKey string) *Client {
	return &Client{BaseURL: baseURL, APIKey: apiKey, HTTP: &http.Client{Timeout: 3 * time.Second}, tokens: map[string]cachedToken{}}
}

// Enabled tells whether the tracker is reachable by configuration.
func (c *Client) Enabled() bool { return c != nil && c.BaseURL != "" }

// Report adds a consumption to the provider's monthly total. It never blocks the caller nor fails it.
func (c *Client) Report(provider string, amount int64) {
	if !c.Enabled() || amount <= 0 {
		return
	}
	go func() {
		body, _ := json.Marshal(map[string]int64{"amount": amount})
		req, err := http.NewRequest(http.MethodPost, c.endpoint(provider, "usage"), bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", c.APIKey)
		if resp, err := c.HTTP.Do(req); err == nil {
			resp.Body.Close()
		}
	}()
}

// Token returns the token an admin set for the provider, "" when none (or the tracker is down). Cached for a
// minute, including the absence of a token, so a down tracker never slows a request.
func (c *Client) Token(provider string) string {
	if !c.Enabled() {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if t, ok := c.tokens[provider]; ok && time.Now().Before(t.expires) {
		return t.value
	}
	value := c.fetchToken(provider)
	c.tokens[provider] = cachedToken{value: value, expires: time.Now().Add(tokenTTL)}
	return value
}

func (c *Client) fetchToken(provider string) string {
	req, err := http.NewRequest(http.MethodGet, c.endpoint(provider, "token"), nil)
	if err != nil {
		return ""
	}
	req.Header.Set("x-api-key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var out struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil {
		return ""
	}
	return out.Token
}

func (c *Client) endpoint(provider, leaf string) string {
	return fmt.Sprintf("%s/internal/integrations/%s/%s", c.BaseURL, url.PathEscape(provider), leaf)
}
