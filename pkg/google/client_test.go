package google

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthURL(t *testing.T) {
	c := New("id", "secret", "http://localhost:8085/gmail/callback")
	u, err := url.Parse(c.AuthURL("st"))
	require.NoError(t, err)
	q := u.Query()
	assert.Equal(t, "id", q.Get("client_id"))
	assert.Equal(t, "st", q.Get("state"))
	assert.Equal(t, "offline", q.Get("access_type"))
	assert.Contains(t, q.Get("scope"), "gmail.send")
	assert.NotContains(t, q.Get("scope"), "gmail.readonly")
}

func TestBuildMIME(t *testing.T) {
	raw, err := BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "Candidature é", HTML: "<p>hi</p>",
		Attachments: []core.Attachment{{Filename: "CV.pdf", Data: []byte("%PDF-1.4")}}})
	require.NoError(t, err)
	s := string(raw)
	assert.Contains(t, s, "To: b@y.tn")
	assert.Contains(t, s, "multipart/mixed")
	assert.Contains(t, s, "filename=\"CV.pdf\"")
	assert.Contains(t, s, "Subject: =?utf-8?q?")

	assert.Contains(t, s, "Content-Type: application/pdf; name=\"CV.pdf\"")

	// The imported file keeps its own type; a type that cannot be read never reaches the headers as it came.
	const docx = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	raw, err = BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "x",
		Attachments: []core.Attachment{{Filename: "cv.docx", ContentType: docx, Data: []byte("PK")}}})
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Content-Type: "+docx+"; name=\"cv.docx\"")
	raw, err = BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "x",
		Attachments: []core.Attachment{{Filename: "cv.bin", ContentType: "x\r\nBcc: evil@z.tn", Data: []byte("PK")}}})
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Content-Type: application/octet-stream; name=\"cv.bin\"")
	assert.NotContains(t, string(raw), "evil@z.tn")

	plain, err := BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "x"})
	require.NoError(t, err)
	assert.NotContains(t, string(plain), "multipart")

	_, err = BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn\r\nBcc: evil@z.tn", Subject: "s"})
	assert.Error(t, err)
}

func TestExchangeRefreshSend(t *testing.T) {
	var sentRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.NoError(t, r.ParseForm())
			if r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "revoked" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"acc","refresh_token":"ref","expires_in":3600}`))
		case "/send":
			if r.Header.Get("Authorization") != "Bearer acc" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			sentRaw = body["raw"]
			_, _ = w.Write([]byte(`{"id":"1"}`))
		case "/userinfo":
			_, _ = w.Write([]byte(`{"email":"me@gmail.com"}`))
		}
	}))
	defer srv.Close()
	c := New("id", "secret", "http://cb")
	c.TokenURL, c.SendURL, c.UserInfoURL = srv.URL+"/token", srv.URL+"/send", srv.URL+"/userinfo"

	tok, err := c.Exchange(context.Background(), "code")
	require.NoError(t, err)
	assert.Equal(t, "acc", tok.AccessToken)
	assert.Equal(t, "ref", tok.RefreshToken)

	addr, err := c.UserEmail(context.Background(), "acc")
	require.NoError(t, err)
	assert.Equal(t, "me@gmail.com", addr)

	_, err = c.Refresh(context.Background(), "revoked")
	assert.ErrorIs(t, err, core.ErrGmailAuth)

	require.NoError(t, c.Send(context.Background(), "acc", core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "hi"}))
	decoded, err := base64.RawURLEncoding.DecodeString(sentRaw)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(decoded), "To: b@y.tn"))

	assert.ErrorIs(t, c.Send(context.Background(), "bad", core.MailMessage{From: "a", To: "b", Subject: "s"}), core.ErrGmailAuth)
}

// The CV and the cover letter leave as two parts, in order; a non-ASCII name gets its UTF-8 form too, and a name
// never breaks out of its header.
func TestBuildMIMEAttachments(t *testing.T) {
	raw, err := BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "x", Attachments: []core.Attachment{
		{Filename: "Développeuse React.pdf", Data: []byte("%PDF cv")},
		{Filename: "رسالة_تحفيزية.pdf", ContentType: "application/pdf", Data: []byte("%PDF letter")},
	}})
	require.NoError(t, err)
	s := string(raw)
	assert.Equal(t, 2, strings.Count(s, "Content-Disposition: attachment"))
	assert.Less(t, strings.Index(s, "filename*=UTF-8''D%C3%A9veloppeuse%20React.pdf"), strings.Index(s, "filename*=UTF-8''%D8%B1"))
	assert.Contains(t, s, "filename=\"=?utf-8?q?")
	assert.Equal(t, 1, strings.Count(s, "--\r\n"), "one closing boundary")

	raw, err = BuildMIME(core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "x", Attachments: []core.Attachment{
		{Filename: "cv.pdf\r\nBcc: evil@z.tn\".pdf", Data: []byte("%PDF")},
	}})
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "\r\nBcc:")
	assert.Contains(t, string(raw), "filename=\"cv.pdfBcc: evil@z.tn.pdf\"")
	assert.NotContains(t, string(raw), "filename*=", "an ASCII name needs no UTF-8 form")
}

func TestSendTooLarge(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer srv.Close()
	c := New("id", "secret", "http://cb")
	c.SendURL = srv.URL
	big := core.Attachment{Filename: "cv.pdf", Data: make([]byte, core.MaxMailBytes)}
	err := c.Send(context.Background(), "acc", core.MailMessage{From: "a@x.tn", To: "b@y.tn", Subject: "s", HTML: "x", Attachments: []core.Attachment{big}})
	assert.ErrorIs(t, err, core.ErrMailTooLarge)
	assert.False(t, called)
}
