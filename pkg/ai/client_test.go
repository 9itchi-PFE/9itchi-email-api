package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchKeywordsPassThrough(t *testing.T) {
	reply := `{"score":68,"level":"good","strengths":["a"],"gaps":[],"advice":"x","keywords":{"matched":["React"],"missing":["Docker","C++"]},"keyword_coverage":33}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ai/jobs/match" || r.Header.Get("x-api-key") != "key" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()

	res, err := New(srv.URL, "key").Match(context.Background(), core.MatchInput{JobTitle: "Stage", Candidate: "React", Language: "fr"})
	require.NoError(t, err)
	assert.Equal(t, []string{"React"}, res.KeywordsMatched)
	assert.Equal(t, []string{"Docker", "C++"}, res.KeywordsMissing)
	assert.Equal(t, 33, res.KeywordCoverage)

	reply = `{"score":68,"level":"good","strengths":["a"],"gaps":[],"advice":"x","keyword_coverage":400}`
	res, err = New(srv.URL, "key").Match(context.Background(), core.MatchInput{JobTitle: "Stage", Candidate: "React"})
	require.NoError(t, err, "an older ai-api without keywords still works")
	assert.Empty(t, res.KeywordsMatched)
	assert.Zero(t, res.KeywordCoverage, "an out-of-range coverage is dropped")
}
func TestDraftSendsTheGoal(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ai/emails/draft" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"subject":"Candidature","body":"Bonjour"}`))
	}))
	defer srv.Close()

	subject, body, err := New(srv.URL, "key").Draft(context.Background(), "token", core.DraftInput{JobTitle: "Stage", Goal: "Un appel"})
	require.NoError(t, err)
	assert.Equal(t, "Candidature", subject)
	assert.Equal(t, "Bonjour", body)
	assert.Equal(t, "Un appel", got["goal"])
}
