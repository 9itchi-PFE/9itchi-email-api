package services

import (
	"context"
	"net/http"
	"testing"

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
