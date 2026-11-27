package recruiters

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

func TestHunterDomainSearch(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "key" || r.URL.Query().Get("api_key") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, "/v2/domain-search", r.URL.Path)
		q := r.URL.Query()
		calls = append(calls, q.Encode())
		switch {
		case q.Get("domain") == "gone.tn":
			w.WriteHeader(http.StatusNotFound)
		case q.Get("domain") == "small.tn" && q.Get("department") == "hr":
			_, _ = w.Write([]byte(`{"data":{"domain":"small.tn","emails":[]}}`))
		case q.Get("domain") == "small.tn":
			_, _ = w.Write([]byte(`{"data":{"domain":"small.tn","emails":[{"value":"Ali@small.tn","first_name":"Ali","position":"CEO","department":"executive","confidence":70}]}}`))
		case q.Get("company") == "Acme":
			w.Header().Set("X-Credits-Used", "1")
			_, _ = w.Write([]byte(`{"data":{"domain":"acme.tn","organization":"Acme SA","emails":[]}}`))
		default:
			require.Equal(t, "hr", q.Get("department"))
			require.Equal(t, "personal", q.Get("type"))
			require.Equal(t, "10", q.Get("limit"))
			_, _ = w.Write([]byte(`{"data":{"domain":"x.tn","emails":[{"value":"Sarra@X.tn","first_name":"Sarra","last_name":"Gharbi",
				"position":"HR Manager","department":"hr","linkedin":"https://www.linkedin.com/in/sarra","confidence":91,
				"verification":{"date":"2026-01-01","status":"valid"}},{"value":"","first_name":"nobody"}]}}`))
		}
	}))
	defer srv.Close()
	h := NewHunter(Config{HunterAPIKey: "key", HunterURL: srv.URL})

	got, err := h.Find(context.Background(), core.ContactQuery{Domain: "x.tn", MaxCredits: 5})
	require.NoError(t, err)
	assert.Equal(t, &core.ContactLookup{Credits: 1, Domain: "x.tn", Contacts: []core.Contact{{
		Email: "sarra@x.tn", FirstName: "Sarra", LastName: "Gharbi", Title: "HR Manager", Department: "hr",
		LinkedInURL: "https://www.linkedin.com/in/sarra", Confidence: 91, Verification: "valid",
	}}}, got)

	got, err = h.Find(context.Background(), core.ContactQuery{Domain: "small.tn", MaxCredits: 5})
	require.NoError(t, err)
	require.Len(t, got.Contacts, 1, "nobody in HR: one wider search")
	assert.Equal(t, "executive", got.Contacts[0].Department)
	assert.Equal(t, 1, got.Credits, "an empty search costs nothing")

	calls = nil
	got, err = h.Find(context.Background(), core.ContactQuery{Domain: "small.tn", MaxCredits: 0})
	require.NoError(t, err)
	assert.Empty(t, got.Contacts)
	assert.Empty(t, calls, "no credit left: not asked")

	got, err = h.Find(context.Background(), core.ContactQuery{Company: "Acme", MaxCredits: 1})
	require.NoError(t, err)
	assert.Equal(t, "acme.tn", got.Domain, "asked by name, Hunter tells the domain")
	assert.Equal(t, "Acme SA", got.Company)
	assert.Equal(t, 1, got.Credits, "X-Credits-Used wins")

	got, err = h.Find(context.Background(), core.ContactQuery{Domain: "gone.tn", MaxCredits: 5})
	require.NoError(t, err, "a company Hunter does not know is an empty answer")
	assert.Empty(t, got.Contacts)

	_, err = NewHunter(Config{HunterAPIKey: "wrong", HunterURL: srv.URL}).Find(context.Background(), core.ContactQuery{Domain: "x.tn", MaxCredits: 5})
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}

func TestHunterDiscover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-KEY") != "key" || r.Method != http.MethodPost || r.URL.Path != "/v2/discover" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Keywords struct {
				Include []string `json:"include"`
				Match   string   `json:"match"`
			} `json:"keywords"`
			Location struct {
				Include []map[string]string `json:"include"`
			} `json:"headquarters_location"`
			Limit int `json:"limit"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, []string{"data", "python"}, body.Keywords.Include)
		assert.Equal(t, "any", body.Keywords.Match)
		assert.Equal(t, []map[string]string{{"country": "TN", "city": "Sfax"}}, body.Location.Include)
		assert.Equal(t, 20, body.Limit)
		_, _ = w.Write([]byte(`{"data":[{"domain":"WWW.Acme.tn","organization":"Acme","emails_count":{"total":3}},{"domain":"","organization":"x"}],"meta":{"results":2}}`))
	}))
	defer srv.Close()

	got, err := NewHunter(Config{HunterAPIKey: "key", HunterURL: srv.URL}).Discover(context.Background(),
		core.CompanyQuery{Terms: []string{"data", "python"}, Country: "TN", City: "Sfax", Limit: 20})
	require.NoError(t, err)
	assert.Equal(t, []core.Company{{Domain: "acme.tn", Name: "Acme", Country: "TN", City: "Sfax"}}, got)

	_, err = NewHunter(Config{HunterAPIKey: "wrong", HunterURL: srv.URL}).Discover(context.Background(),
		core.CompanyQuery{Terms: []string{"data"}, Country: "TN", Limit: 20})
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}
