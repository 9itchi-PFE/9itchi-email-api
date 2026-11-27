package recruiters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApolloSearchAndReveal(t *testing.T) {
	reveals := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" || r.Method != http.MethodPost {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		switch r.URL.Path {
		case "/api/v1/mixed_people/api_search":
			assert.Equal(t, []string{"acme.tn"}, q["q_organization_domains_list[]"])
			assert.Contains(t, q["person_titles[]"], "talent acquisition")
			_, _ = w.Write([]byte(`{"people":[{"id":"p1","first_name":"Karim","title":"CTO","has_email":true},
				{"id":"p2","first_name":"Sarra","title":"Talent Acquisition","has_email":true},
				{"id":"p3","first_name":"Ali","title":"Recruiter","has_email":true},
				{"id":"p4","first_name":"Nour","title":"Recruiter","has_email":false}]}`))
		case "/api/v1/people/match":
			reveals++
			assert.Equal(t, "false", q.Get("reveal_personal_emails"))
			switch q.Get("id") {
			case "p2":
				_, _ = w.Write([]byte(`{"person":{"email":"Sarra@acme.tn","email_status":"verified","first_name":"Sarra","last_name":"Gharbi","title":"Talent Acquisition","linkedin_url":"https://www.linkedin.com/in/sarra"}}`))
			case "p3":
				_, _ = w.Write([]byte(`{"person":{"email":"email_not_unlocked@domain.com"}}`))
			default:
				_, _ = w.Write([]byte(`{"person":{"email":"karim@gmail.com"}}`))
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	a := NewApollo(Config{ApolloAPIKey: "key", ApolloURL: srv.URL})

	got, err := a.Find(context.Background(), core.ContactQuery{Domain: "acme.tn", MaxCredits: 50})
	require.NoError(t, err)
	assert.Equal(t, 2, reveals, "at most 2 reveals per company, recruiters first")
	assert.Equal(t, 2, got.Credits)
	assert.Equal(t, []core.Contact{{Email: "sarra@acme.tn", FirstName: "Sarra", LastName: "Gharbi", Title: "Talent Acquisition",
		Department: "hr", LinkedInURL: "https://www.linkedin.com/in/sarra", Confidence: 90, Verification: "valid"}}, got.Contacts,
		"a locked address is dropped")

	reveals = 0
	got, err = a.Find(context.Background(), core.ContactQuery{Domain: "acme.tn", MaxCredits: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, reveals, "never more than the credits left")
	assert.Equal(t, 1, got.Credits)

	got, err = a.Find(context.Background(), core.ContactQuery{Company: "Acme", MaxCredits: 5})
	require.NoError(t, err)
	assert.Empty(t, got.Contacts, "apollo needs a domain")

	_, err = NewApollo(Config{ApolloAPIKey: "wrong", ApolloURL: srv.URL}).Find(context.Background(), core.ContactQuery{Domain: "acme.tn", MaxCredits: 5})
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}
