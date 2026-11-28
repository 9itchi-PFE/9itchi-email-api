// Package google is the Google side of the Gmail integration: OAuth2 (web server flow) and the Gmail send API.
package google

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"9itchi-email-api/core"
)

// Scopes: send-only Gmail access plus the address of the connected account.
var Scopes = []string{"https://www.googleapis.com/auth/gmail.send", "openid", "email"}

type Client struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	AuthEndpoint string
	TokenURL     string
	RevokeURL    string
	UserInfoURL  string
	SendURL      string
	HTTP         *http.Client
}

func New(clientID, clientSecret, redirectURL string) *Client {
	return &Client{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		AuthEndpoint: "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:     "https://oauth2.googleapis.com/token",
		RevokeURL:    "https://oauth2.googleapis.com/revoke",
		UserInfoURL:  "https://openidconnect.googleapis.com/v1/userinfo",
		SendURL:      "https://gmail.googleapis.com/gmail/v1/users/me/messages/send",
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

var _ core.Mailer = (*Client)(nil)

func (c *Client) Configured() bool { return c.ClientID != "" && c.ClientSecret != "" }

func (c *Client) AuthURL(state string) string {
	q := url.Values{}
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURL)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(Scopes, " "))
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	q.Set("state", state)
	return c.AuthEndpoint + "?" + q.Encode()
}

type tokenReply struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

func (c *Client) token(ctx context.Context, form url.Values) (*core.GoogleToken, error) {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, core.ErrGmailAuth
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, fmt.Errorf("%w: google token status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	var out tokenReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.AccessToken == "" {
		return nil, fmt.Errorf("%w: unreadable google token reply", core.ErrPeerUnavailable)
	}
	return &core.GoogleToken{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}, nil
}

func (c *Client) Exchange(ctx context.Context, code string) (*core.GoogleToken, error) {
	return c.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {c.RedirectURL}})
}

// Refresh returns a new access token; Google does not resend the refresh token, so it is carried over.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*core.GoogleToken, error) {
	t, err := c.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	if err != nil {
		return nil, err
	}
	t.RefreshToken = refreshToken
	return t, nil
}

func (c *Client) UserEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.UserInfoURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("%w: google userinfo status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	var out struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	return out.Email, nil
}

func (c *Client) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.RevokeURL, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return nil
}

func (c *Client) Send(ctx context.Context, accessToken string, msg core.MailMessage) error {
	raw, err := BuildMIME(msg)
	if err != nil {
		return err
	}
	if len(raw) > core.MaxMailBytes {
		return core.ErrMailTooLarge
	}
	body, _ := json.Marshal(map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.SendURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return core.ErrGmailAuth
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("%w: gmail send status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	return nil
}

// NewState returns a random OAuth state value.
func NewState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// BuildMIME builds the RFC 5322 message: HTML body, plus each attachment as a base64 part, in order.
// Header values never contain line breaks (header injection); file names lose them, and their quotes.
func BuildMIME(m core.MailMessage) ([]byte, error) {
	for _, v := range []string{m.From, m.To, m.Subject} {
		if strings.ContainsAny(v, "\r\n") {
			return nil, fmt.Errorf("invalid header value")
		}
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\n", m.From, m.To, mime.QEncoding.Encode("utf-8", m.Subject))
	html := base64Lines([]byte(m.HTML))
	if len(m.Attachments) == 0 {
		fmt.Fprintf(&b, "Content-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s", html)
		return b.Bytes(), nil
	}
	boundary, err := NewState()
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/html; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n", boundary, html)
	for _, a := range m.Attachments {
		clean := cleanFilename(a.Filename)
		name := mime.QEncoding.Encode("utf-8", clean)
		disposition := fmt.Sprintf("attachment; filename=\"%s\"", name)
		if name != clean {
			// Q-encoded name for older readers, RFC 2231 UTF-8 name for the others (Gmail, Outlook).
			disposition += "; filename*=UTF-8''" + percentEncode(clean)
		}
		// The type comes from resume-api, which only stores PDF and DOCX; anything unexpected is sent as a plain file.
		ctype := "application/pdf"
		if t := a.ContentType; t != "" {
			if parsed, _, err := mime.ParseMediaType(t); err == nil {
				ctype = parsed
			} else {
				ctype = "application/octet-stream"
			}
		}
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s; name=\"%s\"\r\nContent-Disposition: %s\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n",
			boundary, ctype, name, disposition, base64Lines(a.Data))
	}
	fmt.Fprintf(&b, "--%s--\r\n", boundary)
	return b.Bytes(), nil
}

// cleanFilename drops what could break the header out of a file name: control characters (line breaks), quotes
// and backslashes.
func cleanFilename(name string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			return -1
		}
		return r
	}, name)
}

func base64Lines(data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.String()
}

// percentEncode is the RFC 5987 value of a UTF-8 file name: attr-chars as they are, other bytes as %XX.
func percentEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("!#$&+-.^_`|~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}
