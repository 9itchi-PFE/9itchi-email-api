package recruiters

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuickEnrichFind(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		require.Equal(t, "/api/employees/dataset-search", r.URL.Path)
		q := r.URL.Query()
		require.Empty(t, q.Get("title"), "a title filter costs 1 credit per person")
		require.Equal(t, "true", q.Get("has_email"))
		switch q.Get("company_url") {
		case "gone.tn":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"success":false,"message":"not found","code":404}`))
		case "empty.tn":
			_, _ = w.Write([]byte(`{"success":true,"code":200,"data":[],"meta":{"credits_used":0,"reason":"EMPLOYEE_NOT_FOUND"}}`))
		case "broke.tn":
			w.WriteHeader(http.StatusPaymentRequired)
		default:
			_, _ = w.Write([]byte(`{"success":true,"code":200,"data":[
				{"first_name":"Ali","last_name":"Ben","title":"Software Engineer","email":"ali@x.tn"},
				{"first_name":"Sarra","last_name":"Gharbi","title":"HR Manager","email":"Sarra@X.tn",
				 "employee_linkedin":"https://www.linkedin.com/in/sarra","email_verification_date":"2026-01-15","company_name":"X SA"},
				{"first_name":"Moez","title":"Founder","email":"moez@gmail.com"},
				{"first_name":"Nobody","title":"CEO","email":""}
			],"meta":{"page":1,"per_page":20,"total":3,"credits_used":1,"remaining_credits":99}}`))
		}
	}))
	defer srv.Close()
	e := NewQuickEnrich(Config{QuickEnrichAPIKey: "key", QuickEnrichURL: srv.URL})
	ctx := context.Background()

	got, err := e.Find(ctx, core.ContactQuery{Domain: "x.tn", MaxCredits: 5})
	require.NoError(t, err)
	assert.Equal(t, &core.ContactLookup{Credits: 1, Domain: "x.tn", Company: "X SA", Contacts: []core.Contact{
		{Email: "sarra@x.tn", FirstName: "Sarra", LastName: "Gharbi", Title: "HR Manager", Department: "hr",
			LinkedInURL: "https://www.linkedin.com/in/sarra", Confidence: 85, Verification: "valid"},
		{Email: "ali@x.tn", FirstName: "Ali", LastName: "Ben", Title: "Software Engineer", Department: "other", Confidence: 60},
	}}, got, "HR first, personal mailboxes and empty emails dropped")

	got, err = e.Find(ctx, core.ContactQuery{Domain: "empty.tn", MaxCredits: 5})
	require.NoError(t, err)
	assert.Zero(t, got.Credits)
	assert.Empty(t, got.Contacts)

	got, err = e.Find(ctx, core.ContactQuery{Domain: "gone.tn", MaxCredits: 5})
	require.NoError(t, err, "an unknown company is nobody, not an error")
	assert.Empty(t, got.Contacts)

	_, err = e.Find(ctx, core.ContactQuery{Domain: "broke.tn", MaxCredits: 5})
	assert.True(t, errors.Is(err, core.ErrPeerUnavailable))

	got, err = e.Find(ctx, core.ContactQuery{Company: "Acme", MaxCredits: 5})
	require.NoError(t, err)
	assert.Equal(t, &core.ContactLookup{}, got, "no domain, no call")

	got, err = e.Find(ctx, core.ContactQuery{Domain: "x.tn", MaxCredits: 0})
	require.NoError(t, err)
	assert.Equal(t, &core.ContactLookup{}, got, "no credit left, no call")

	_, err = NewQuickEnrich(Config{QuickEnrichAPIKey: "bad", QuickEnrichURL: srv.URL}).Find(ctx, core.ContactQuery{Domain: "x.tn", MaxCredits: 5})
	assert.True(t, errors.Is(err, core.ErrPeerUnavailable))
	assert.False(t, NewQuickEnrich(Config{}).Ready())
}
