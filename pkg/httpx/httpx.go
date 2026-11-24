// Package httpx holds the JSON helper shared by the peer clients.
package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"9itchi-email-api/core"
)

// Request is one JSON call to a peer service.
type Request struct {
	Method string
	URL    string
	// APIKey is sent as x-api-key (internal routes), Bearer as Authorization (user routes).
	APIKey string
	Bearer string
	Body   any
}

// Do sends the request and decodes the JSON reply into out (skipped when out is nil). Bodies are never logged.
func Do(ctx context.Context, hc *http.Client, r Request, out any) error {
	var reader io.Reader
	if r.Body != nil {
		b, err := json.Marshal(r.Body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, reader)
	if err != nil {
		return err
	}
	if r.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if r.APIKey != "" {
		req.Header.Set("x-api-key", r.APIKey)
	}
	if r.Bearer != "" {
		req.Header.Set("Authorization", "Bearer "+r.Bearer)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return core.ErrPeerNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("%w: status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// URL joins a base URL and a path.
func URL(base, path string) string { return strings.TrimRight(base, "/") + path }
