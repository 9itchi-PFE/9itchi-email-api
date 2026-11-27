package job

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

func TestApplyInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/internal/jobs/j1":
			_, _ = w.Write([]byte(`{"id":"j1","title":"Stage","company":"Acme","description":"d","apply_email":"rh@acme.tn","closes_on":"2027-01-10","open":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	info, err := New(srv.URL, "key").ApplyInfo(context.Background(), "j1")
	require.NoError(t, err)
	assert.Equal(t, &core.JobInfo{Title: "Stage", Company: "Acme", Description: "d", ApplyEmail: "rh@acme.tn", Open: true}, info)

	_, err = New(srv.URL, "key").ApplyInfo(context.Background(), "gone")
	assert.ErrorIs(t, err, core.ErrPeerNotFound)

	_, err = New(srv.URL, "wrong").ApplyInfo(context.Background(), "j1")
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}

func TestReadOffer(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" || r.Method != http.MethodPost || r.URL.Path != "/internal/offers/read" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		got = map[string]string{}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["url"] == "https://www.linkedin.com/jobs/view/1" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"error":"this site cannot be read: paste the offer text"}`))
			return
		}
		_, _ = w.Write([]byte(`{"source":"jobs.example","title":"Stage Go","company":"Acme","location":"Tunis","description":"d","apply_email":"rh@acme.example"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "key")

	offer, err := c.ReadOffer(context.Background(), "https://jobs.example/1", "")
	require.NoError(t, err)
	assert.Equal(t, &core.OfferText{Source: "jobs.example", Title: "Stage Go", Company: "Acme", Description: "d", ApplyEmail: "rh@acme.example"}, offer)
	_, sent := got["text"]
	assert.False(t, sent, "no text is not sent")

	_, err = c.ReadOffer(context.Background(), "https://jobs.example/1", "Stage Go chez Acme")
	require.NoError(t, err)
	assert.Equal(t, "Stage Go chez Acme", got["text"])

	_, err = c.ReadOffer(context.Background(), "https://www.linkedin.com/jobs/view/1", "")
	assert.ErrorIs(t, err, core.ErrPeerUnprocessable)

	_, err = New(srv.URL, "wrong").ReadOffer(context.Background(), "https://jobs.example/1", "")
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}

func TestOfferCompanies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" || r.URL.Path != "/internal/companies/search" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Terms   []string `json:"terms"`
			Country string   `json:"country"`
			Limit   int      `json:"limit"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Country != "TN" || body.Limit != 30 || len(body.Terms) != 2 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"items":[{"company":"Acme","company_key":"acme","domain":"acme.tn","description":"d","city":"Tunis","country":"TN","titles":["Stage data"],"matched_terms":["data"]}]}`))
	}))
	defer srv.Close()

	got, err := New(srv.URL, "key").OfferCompanies(context.Background(), []string{"data", "python"}, "TN", 30)
	require.NoError(t, err)
	assert.Equal(t, []core.OfferCompany{{Company: "Acme", Domain: "acme.tn", Description: "d", City: "Tunis", Country: "TN",
		Titles: []string{"Stage data"}, MatchedTerms: []string{"data"}}}, got)

	_, err = New(srv.URL, "wrong").OfferCompanies(context.Background(), []string{"data"}, "TN", 30)
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}
