package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const otherUID = "6b0d9f5e-2c8a-4f7e-9a51-3d2c1b0a9f8e"

func companyReq(domain string) api.CompanyRecruiterRequest {
	return api.CompanyRecruiterRequest{Company: " Datanomix ", Domain: domain, Goal: "Software Engineer"}
}

func TestRecruiterAtCompanyFindsSavesAndCounts(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 3})
	out, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq("https://www.Datanomix.io/careers"))
	require.Nil(t, apiErr)

	require.Len(t, out.Items, 2)
	assert.Equal(t, "Sarra Gharbi", out.Items[0].Name, "the recruiter first")
	assert.Equal(t, "datanomix.io", out.Items[0].Domain)
	assert.Equal(t, "Datanomix", out.Items[0].Company)
	assert.Equal(t, api.ProspectStats{CompaniesDiscovered: 1, ContactsFound: 2}, out.Stats)
	assert.Equal(t, 1, out.Allowance.Used, "one search counted")
	assert.Equal(t, 1, h.finder.calls)

	require.Len(t, h.pool.companies, 1, "the company joins the pool")
	assert.Equal(t, "link", h.pool.companies[0].Source)
	assert.Len(t, h.pool.contacts, 2, "the people join the pool")
	require.Len(t, h.repo.people, 2, "the people join the student's recruiters")
	assert.Contains(t, string(h.repo.searches[0].Query), `"goal":"software engineer"`)

	// The same company again today: given back, not counted, nobody asked.
	again, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq("datanomix.io"))
	require.Nil(t, apiErr)
	assert.True(t, again.Cached)
	assert.Equal(t, 1, again.Allowance.Used)
	assert.Equal(t, 1, h.finder.calls)

	// Another student: the pool answers, no provider asked.
	other, apiErr := h.svc.AtCompany(context.Background(), otherUID, companyReq("datanomix.io"))
	require.Nil(t, apiErr)
	require.Len(t, other.Items, 2)
	assert.Equal(t, api.ProspectStats{CompaniesFromPool: 1, ContactsFromPool: 2}, other.Stats)
	assert.Equal(t, 1, other.Allowance.Used, "found in the pool: counted like a lookup")
	assert.Equal(t, 1, h.finder.calls)
}

func TestRecruiterAtCompanyByNameFromPool(t *testing.T) {
	h := newRecruiterHarness(nil)
	co, _ := h.pool.SaveCompany(context.Background(), &db.Company{Domain: "datanomix.io", Name: "DATANOMIX", Source: "hunter"})
	_, _ = h.pool.SaveContact(context.Background(), &db.Contact{CompanyID: co.ID, Email: "greg@datanomix.io", FirstName: "Greg",
		Title: "Founder & CEO", Department: "executive", Confidence: 90, Source: "apollo"})

	out, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq(""))
	require.Nil(t, apiErr)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "greg@datanomix.io", out.Items[0].Email)
	assert.Zero(t, h.finder.calls, "the pool knew the company by name")
}

func TestRecruiterAtCompanyNobodyIsNotCounted(t *testing.T) {
	h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: 3})
	h.finder.err = core.ErrPeerUnavailable
	_, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq("datanomix.io"))
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)
	assert.Empty(t, h.repo.searches, "nothing found: no search counted")
}

func TestRecruiterAtCompanyRefusals(t *testing.T) {
	h := newRecruiterHarness(nil)
	for name, req := range map[string]api.CompanyRecruiterRequest{
		"no company": {Company: "  "},
		"bad domain": {Company: "Acme", Domain: "not a domain"},
	} {
		_, apiErr := h.svc.AtCompany(context.Background(), uid, req)
		require.NotNil(t, apiErr, name)
		assert.Equal(t, http.StatusBadRequest, apiErr.Code, name)
	}

	for name, limit := range map[string]int{"not in the plan": 0, "used up": 1} {
		h := newRecruiterHarness(map[string]int{core.QuotaRecruiterSearches: limit})
		if limit == 1 {
			_, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq("acme.tn"))
			require.Nil(t, apiErr)
		}
		_, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq("datanomix.io"))
		require.NotNil(t, apiErr, name)
		assert.Equal(t, http.StatusPaymentRequired, apiErr.Code, name)
	}
	assert.Zero(t, h.finder.calls)
}

// namedFinder answers a search by name with the domain and name it knows (or nothing), and one by domain with people.
type namedFinder struct {
	name, domain, company string
	got                   []core.ContactQuery
}

func (f *namedFinder) Name() string { return f.name }

func (f *namedFinder) Ready() bool { return true }

func (f *namedFinder) Find(_ context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	f.got = append(f.got, q)
	if q.Domain == "" {
		return &core.ContactLookup{Domain: f.domain, Company: f.company}, nil
	}
	return &core.ContactLookup{Credits: 1, Contacts: []core.Contact{{Email: "greg@" + q.Domain, FirstName: "Greg",
		Title: "Founder & CEO", Department: "executive", Confidence: 90}}}, nil
}

func TestRecruiterAtCompanyTakesTurnsBetweenProviders(t *testing.T) {
	h := newRecruiterHarness(nil)
	apollo := &testFinder{name: "apollo", ready: true, credits: 1}
	h.svc.Finders = []core.ContactFinder{h.finder, apollo}
	h.svc.Caps = map[string]int{"hunter": 50, "apollo": 50}
	h.pool.lookups["other.io|hunter"] = memLookup{credits: 10, at: h.now.Add(-time.Hour)}

	_, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq("datanomix.io"))
	require.Nil(t, apiErr)
	assert.Equal(t, 1, apollo.calls, "apollo used less of its cap: asked first")
	assert.Zero(t, h.finder.calls)
	require.Len(t, apollo.got, 1)
	assert.Equal(t, 50, apollo.got[0].MaxCredits)
}

func TestRecruiterAtCompanyByNameWaitsForTheDomain(t *testing.T) {
	h := newRecruiterHarness(nil)
	apollo := &namedFinder{name: "apollo"}
	hunter := &namedFinder{name: "hunter", domain: "datanomix.io", company: "DATANOMIX, Inc."}
	h.svc.Finders = []core.ContactFinder{hunter, apollo}
	h.svc.Caps = map[string]int{"hunter": 50, "apollo": 50}
	h.pool.lookups["other.io|hunter"] = memLookup{credits: 10, at: h.now.Add(-time.Hour)}

	out, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq(""))
	require.Nil(t, apiErr)
	require.Len(t, out.Items, 1)
	assert.Equal(t, "greg@datanomix.io", out.Items[0].Email)
	require.Len(t, apollo.got, 2, "apollo needs a domain: asked again once hunter told it")
	assert.Empty(t, apollo.got[0].Domain)
	assert.Equal(t, "datanomix.io", apollo.got[1].Domain)
}

func TestRecruiterAtCompanyDropsAnotherCompany(t *testing.T) {
	h := newRecruiterHarness(nil)
	h.svc.Finders = []core.ContactFinder{&namedFinder{name: "hunter", domain: "datanomics.com", company: "Datanomics Labs"}}

	_, apiErr := h.svc.AtCompany(context.Background(), uid, companyReq(""))
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)
	assert.Empty(t, h.pool.companies, "a close name is not the company")
}

func TestCompanyKeyAndNamedLookup(t *testing.T) {
	assert.Equal(t, companyKey("Datanomix"), companyKey("DATANOMIX, Inc."))
	assert.Equal(t, companyKey("datanomix"), companyKey("Data Nomix SARL"))
	assert.Equal(t, "group", companyKey("Group"), "a lone legal word stays")
	assert.NotEqual(t, companyKey("Datanomix"), companyKey("Datanomics"))

	h := newRecruiterHarness(nil)
	_, _ = h.pool.SaveCompany(context.Background(), &db.Company{Domain: "datanomix.io", Name: "DATANOMIX, Inc.", Source: "hunter"})
	_, _ = h.pool.SaveCompany(context.Background(), &db.Company{Domain: "datanomics.com", Name: "Datanomics", Source: "hunter"})
	got, err := h.svc.companyNamed(context.Background(), "Datanomix")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "datanomix.io", got.Domain)
	none, err := h.svc.companyNamed(context.Background(), "Datanom")
	require.NoError(t, err)
	assert.Nil(t, none)
}
