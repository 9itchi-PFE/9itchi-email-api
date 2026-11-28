package resume

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummaryTextKeepsFiguresAndCredentials(t *testing.T) {
	var v resumeView
	require.NoError(t, json.Unmarshal([]byte(`{"content":{
		"contact":{"name":"Ibrahim Benlakhal","headline":"Élève ingénieur"},
		"education":[{"school":"ENIS","degree":"Génie informatique","start":"2022","end":"2028"},{"school":"Old","hidden":true}],
		"experience":[{"company":"Telnet","title":"Stagiaire","kind":"stage d'été","start":"2025-07","end":"2025-08",
			"description":["Réduit la latence de 2 s à moins de 100 ms", " "]}],
		"projects":[{"name":"9itchi","tech":"Go, Next.js","description":["Conçu 11 microservices"]}],
		"certifications":[{"name":"CTI / EUR-ACE","issuer":"ENIS"}],
		"languages":["Anglais: B2"],
		"extra":[{"title":"Situation","items":["Carte de séjour Talent"]},{"title":"Vide","items":[]}],
		"skills":["C++","Go"]}}`), &v))

	got := summaryText(&v)
	for _, want := range []string{
		"Name: Ibrahim Benlakhal\n",
		"Education: Génie informatique, ENIS (2022 - 2028)\n",
		"Experience: Stagiaire (stage d'été) at Telnet (2025-07 - 2025-08)\n  - Réduit la latence de 2 s à moins de 100 ms\n",
		"Project: 9itchi (Go, Next.js)\n  - Conçu 11 microservices\n",
		"Certification: CTI / EUR-ACE, ENIS\n",
		"Languages: Anglais: B2\n",
		"Situation: Carte de séjour Talent\n",
		"Skills: C++, Go\n",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "Old", "hidden entries stay out")
	assert.NotContains(t, got, "Vide")
	assert.NotContains(t, got, "  - \n", "blank bullets dropped")
}

func TestSummaryTextCutsOnALineEnd(t *testing.T) {
	var v resumeView
	v.Content.Skills = []string{strings.Repeat("x", 100)}
	for i := 0; i < 200; i++ {
		v.Content.Languages = append(v.Content.Languages, "Français")
	}
	v.Content.Summary = strings.Repeat("a", maxSummaryBytes)
	got := summaryText(&v)
	assert.LessOrEqual(t, len(got), maxSummaryBytes)
	assert.True(t, strings.HasSuffix(got, "\n"))
}

// The CV keeps the name resume-api gives it (the UTF-8 filename* wins), the imported file its own extension.
func TestFilesTakeTheirNameFromResumeAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "k", r.Header.Get("x-api-key"))
		switch r.URL.Path {
		case "/internal/resumes/r1/pdf":
			w.Header().Set("Content-Disposition", `attachment; filename="Developpeuse_React.pdf"; filename*=UTF-8''D%C3%A9veloppeuse%20React.pdf`)
			_, _ = w.Write([]byte("%PDF cv"))
		case "/internal/resumes/r2/pdf":
			_, _ = w.Write([]byte("%PDF cv"))
		case "/internal/resumes/r1/original":
			w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
			w.Header().Set("Content-Disposition", `attachment; filename="Mon CV.docx"`)
			_, _ = w.Write([]byte("PK"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k")
	ctx := context.Background()

	pdf, err := c.PDF(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "Développeuse React.pdf", pdf.Filename)
	assert.Equal(t, "application/pdf", pdf.ContentType)
	pdf, err = c.PDF(ctx, "r2")
	require.NoError(t, err)
	assert.Equal(t, "CV.pdf", pdf.Filename, "no name given")
	pdf, err = c.PDF(ctx, "gone")
	require.NoError(t, err)
	assert.Nil(t, pdf)

	orig, err := c.Original(ctx, "r1")
	require.NoError(t, err)
	assert.Equal(t, "Mon CV.docx", orig.Filename)
	assert.Equal(t, []byte("PK"), orig.Data)
}

func TestLetterPDF(t *testing.T) {
	var got map[string]any
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/internal/letters/pdf", r.URL.Path)
		assert.Equal(t, "k", r.Header.Get("x-api-key"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, _ = w.Write([]byte("%PDF letter"))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k")

	data, err := c.LetterPDF(context.Background(), core.LetterRequest{ResumeID: "r1", Language: "fr", Subject: "Objet", Body: "Bonjour", SenderEmail: "me@gmail.com"})
	require.NoError(t, err)
	assert.Equal(t, []byte("%PDF letter"), data)
	assert.Equal(t, map[string]any{"resume_id": "r1", "language": "fr", "subject": "Objet", "body": "Bonjour", "sender_email": "me@gmail.com"}, got)

	got = nil
	_, err = c.LetterPDF(context.Background(), core.LetterRequest{Language: "ar", Body: "x"})
	require.NoError(t, err)
	assert.NotContains(t, got, "resume_id", "no CV: no contact block")

	status = http.StatusBadRequest
	_, err = c.LetterPDF(context.Background(), core.LetterRequest{Language: "fr", Body: "x"})
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}
