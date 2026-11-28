// Package resume is the HTTP client for 9itchi-resume-api. Base URL comes from env, never hard-coded.
package resume

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
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

var (
	_ core.ResumeSource = (*Client)(nil)
	_ core.SkillSource  = (*Client)(nil)
)

// maxPDFBytes is the largest attachment accepted from the resume service (Gmail allows 25 MB per email).
const maxPDFBytes = 10 << 20

// resumeView is the part of GET /resumes/:id read here.
type resumeView struct {
	Title   string `json:"title"`
	Content struct {
		Contact struct {
			Name     string `json:"name"`
			Headline string `json:"headline"`
		} `json:"contact"`
		Summary    string   `json:"summary"`
		Skills     []string `json:"skills"`
		Languages  []string `json:"languages"`
		Experience []struct {
			Company     string   `json:"company"`
			Title       string   `json:"title"`
			Kind        string   `json:"kind"`
			Hidden      bool     `json:"hidden"`
			Start       string   `json:"start"`
			End         string   `json:"end"`
			Description []string `json:"description"`
		} `json:"experience"`
		Education []struct {
			School string `json:"school"`
			Degree string `json:"degree"`
			Hidden bool   `json:"hidden"`
			Start  string `json:"start"`
			End    string `json:"end"`
		} `json:"education"`
		Projects []struct {
			Name        string   `json:"name"`
			Tech        string   `json:"tech"`
			Hidden      bool     `json:"hidden"`
			Description []string `json:"description"`
		} `json:"projects"`
		Certifications []struct {
			Name   string `json:"name"`
			Issuer string `json:"issuer"`
			Hidden bool   `json:"hidden"`
			Year   string `json:"year"`
		} `json:"certifications"`
		Extra []struct {
			Title string   `json:"title"`
			Items []string `json:"items"`
		} `json:"extra"`
	} `json:"content"`
}

// get calls GET /resumes/:id on behalf of the user.
func (c *Client) get(ctx context.Context, bearer, id string) (*resumeView, error) {
	var out resumeView
	err := httpx.Do(ctx, c.HTTP, httpx.Request{
		Method: http.MethodGet, URL: httpx.URL(c.BaseURL, "/resumes/"+url.PathEscape(id)), Bearer: bearer,
	}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Skills calls GET /resumes/:id on behalf of the user: the skills the CV lists.
func (c *Client) Skills(ctx context.Context, bearer, id string) ([]string, error) {
	out, err := c.get(ctx, bearer, id)
	if err != nil {
		return nil, err
	}
	return out.Content.Skills, nil
}

// maxSummaryBytes keeps the CV text under the 8000 bytes ai-api accepts for candidate_summary.
const maxSummaryBytes = 7500

// Summary calls GET /resumes/:id on behalf of the user and flattens the CV to text for the AI.
func (c *Client) Summary(ctx context.Context, bearer, id string) (string, error) {
	out, err := c.get(ctx, bearer, id)
	if err != nil {
		return "", err
	}
	return summaryText(out), nil
}

// summaryText is the CV as text: identity, education with dates, experience with its bullets (where the figures
// are), projects, certifications, languages, other sections and skills. Hidden entries stay out. The text is cut
// on a line end at maxSummaryBytes.
func summaryText(out *resumeView) string {
	var b strings.Builder
	k := out.Content
	fmt.Fprintf(&b, "Name: %s\nHeadline: %s\nSummary: %s\n", k.Contact.Name, k.Contact.Headline, k.Summary)
	for _, e := range k.Education {
		if !e.Hidden {
			fmt.Fprintf(&b, "Education: %s, %s%s\n", e.Degree, e.School, period(e.Start, e.End))
		}
	}
	for _, e := range k.Experience {
		if e.Hidden {
			continue
		}
		kind := ""
		if e.Kind != "" {
			kind = " (" + e.Kind + ")"
		}
		fmt.Fprintf(&b, "Experience: %s%s at %s%s\n", e.Title, kind, e.Company, period(e.Start, e.End))
		writeBullets(&b, e.Description)
	}
	for _, p := range k.Projects {
		if p.Hidden {
			continue
		}
		tech := ""
		if p.Tech != "" {
			tech = " (" + p.Tech + ")"
		}
		fmt.Fprintf(&b, "Project: %s%s\n", p.Name, tech)
		writeBullets(&b, p.Description)
	}
	for _, c := range k.Certifications {
		if !c.Hidden {
			fmt.Fprintf(&b, "Certification: %s\n", strings.Join(nonEmpty(c.Name, c.Issuer, c.Year), ", "))
		}
	}
	if len(k.Languages) > 0 {
		fmt.Fprintf(&b, "Languages: %s\n", strings.Join(k.Languages, ", "))
	}
	for _, x := range k.Extra {
		if len(x.Items) > 0 {
			fmt.Fprintf(&b, "%s: %s\n", x.Title, strings.Join(x.Items, "; "))
		}
	}
	if len(k.Skills) > 0 {
		fmt.Fprintf(&b, "Skills: %s\n", strings.Join(k.Skills, ", "))
	}
	text := b.String()
	if len(text) > maxSummaryBytes {
		text = text[:strings.LastIndexByte(text[:maxSummaryBytes], '\n')+1]
	}
	return text
}

func writeBullets(b *strings.Builder, lines []string) {
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			fmt.Fprintf(b, "  - %s\n", l)
		}
	}
}

// period is " (start - end)", " (start)" or "".
func period(start, end string) string {
	if p := strings.Join(nonEmpty(start, end), " - "); p != "" {
		return " (" + p + ")"
	}
	return ""
}

func nonEmpty(in ...string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// Names calls GET /resumes/:id on behalf of the user: the CV's title and the name in its contact block.
func (c *Client) Names(ctx context.Context, bearer, id string) (*core.ResumeNames, error) {
	out, err := c.get(ctx, bearer, id)
	if err != nil {
		return nil, err
	}
	return &core.ResumeNames{Title: out.Title, ContactName: out.Content.Contact.Name}, nil
}

// PDF calls GET /internal/resumes/:id/pdf: the CV as it is now, under the name resume-api gives it (the CV's
// title). It returns nil when the CV does not exist.
func (c *Client) PDF(ctx context.Context, id string) (*core.Attachment, error) {
	data, h, err := c.file(ctx, http.MethodGet, "/internal/resumes/"+url.PathEscape(id)+"/pdf", nil)
	if err != nil || data == nil {
		return nil, err
	}
	return &core.Attachment{Filename: filename(h, "CV.pdf"), ContentType: "application/pdf", Data: data}, nil
}

// Original calls GET /internal/resumes/:id/original: the file the student imported, as it came, with its own
// name and type. It returns nil when the CV has none.
func (c *Client) Original(ctx context.Context, id string) (*core.Attachment, error) {
	data, h, err := c.file(ctx, http.MethodGet, "/internal/resumes/"+url.PathEscape(id)+"/original", nil)
	if err != nil || data == nil {
		return nil, err
	}
	return &core.Attachment{Filename: filename(h, "CV"), ContentType: h.Get("Content-Type"), Data: data}, nil
}

// letterBody is the body of POST /internal/letters/pdf.
type letterBody struct {
	ResumeID    string `json:"resume_id,omitempty"`
	Language    string `json:"language"`
	Subject     string `json:"subject,omitempty"`
	Body        string `json:"body"`
	SenderEmail string `json:"sender_email,omitempty"`
}

// LetterPDF calls POST /internal/letters/pdf: the cover letter rendered as a PDF.
func (c *Client) LetterPDF(ctx context.Context, in core.LetterRequest) ([]byte, error) {
	data, _, err := c.file(ctx, http.MethodPost, "/internal/letters/pdf", letterBody{
		ResumeID: in.ResumeID, Language: in.Language, Subject: in.Subject, Body: in.Body, SenderEmail: in.SenderEmail,
	})
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: no letter", core.ErrPeerUnavailable)
	}
	return data, nil
}

// filename is the file name of a Content-Disposition header (filename*, the UTF-8 one, wins over filename), else
// fallback.
func filename(h http.Header, fallback string) string {
	if _, params, err := mime.ParseMediaType(h.Get("Content-Disposition")); err == nil && strings.TrimSpace(params["filename"]) != "" {
		return params["filename"]
	}
	return fallback
}

// file reads one file from resume-api; body, when not nil, is sent as JSON. Nil data without an error means there
// is no such file (404) or it is larger than an email can carry.
func (c *Client) file(ctx context.Context, method, path string, body any) ([]byte, http.Header, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, httpx.URL(c.BaseURL, path), reader)
	if err != nil {
		return nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("x-api-key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, nil, core.ErrPeerUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil, nil, fmt.Errorf("%w: status %d", core.ErrPeerUnavailable, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPDFBytes+1))
	if err != nil {
		return nil, nil, err
	}
	if len(data) > maxPDFBytes {
		return nil, nil, nil
	}
	return data, resp.Header, nil
}
