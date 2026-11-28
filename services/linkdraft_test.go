package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeOffers struct {
	calls   int
	gotURL  string
	gotText string
	err     error
	offer   *core.OfferText
}

func (f *fakeOffers) ReadOffer(_ context.Context, rawURL, text string) (*core.OfferText, error) {
	f.calls++
	f.gotURL, f.gotText = rawURL, text
	if f.err != nil {
		return nil, f.err
	}
	if f.offer != nil {
		return f.offer, nil
	}
	return &core.OfferText{Source: "jobs.example", Title: "Stage Go", Company: "Acme", Description: "Backend Go"}, nil
}

func linkHarness(quotas map[string]int) (*harness, *emailService, *fakeOffers) {
	h := newHarness(nil, unlimited)
	svc := h.svc.(*emailService)
	offers := &fakeOffers{}
	svc.Offers = offers
	svc.Quotas = mapQuota(quotas)
	return h, svc, offers
}

func TestNormalizeLink(t *testing.T) {
	u, ok := normalizeLink(" HTTPS://Jobs.Example/offre/1?utm_source=li&id=7&UTM_medium=x#apply ")
	require.True(t, ok)
	assert.Equal(t, "https://jobs.example/offre/1?id=7", u.String())
	for _, bad := range []string{"", "ftp://jobs.example/1", "jobs.example/1", "https://", "https://u:p@jobs.example/", "https://jobs.example/" + strings.Repeat("a", 2048)} {
		_, ok := normalizeLink(bad)
		assert.False(t, ok, bad)
	}
}

func TestDraftFromLinkToA9itchiOffer(t *testing.T) {
	h, svc, offers := linkHarness(nil)
	out, apiErr := svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "http://www.client/offres/" + strings.ToUpper(jobID) + "/?utm_source=x"})
	require.Nil(t, apiErr)
	require.NotNil(t, out.JobID)
	assert.Equal(t, jobID, *out.JobID)
	assert.Equal(t, api.LinkOffer{Source: "9itchi", Title: "Stage React", Company: "Acme"}, out.Offer)
	assert.Zero(t, offers.calls, "a 9itchi offer is not read through job-api's link reader")
	assert.Equal(t, 1, h.draft.calls)
	assert.Equal(t, "desc", h.draft.in.JobDescription)

	_, apiErr = svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "http://client/offres/not-an-id"})
	require.Nil(t, apiErr)
	assert.Equal(t, 1, offers.calls, "any other page of the client is a link like any other")
}

func TestDraftFromLinkReadsKeepsAndCounts(t *testing.T) {
	h, svc, offers := linkHarness(map[string]int{core.QuotaAIDrafts: 2})
	req := api.DraftFromLinkRequest{URL: "https://jobs.example/1#top", ResumeID: resumID, Language: "en"}
	out, apiErr := svc.DraftFromLink(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Nil(t, out.JobID)
	assert.Equal(t, api.LinkOffer{Source: "jobs.example", Title: "Stage Go", Company: "Acme"}, out.Offer)
	assert.Equal(t, "https://jobs.example/1", offers.gotURL)
	assert.Equal(t, core.DraftInput{JobTitle: "Stage Go", Company: "Acme", JobDescription: "Backend Go", CandidateSummary: "Ines, ENIS", Language: "en", Tone: "formal"}, h.draft.in)
	assert.Equal(t, 1, out.Used)
	saved := svc.Drafts.(*memDrafts).rows[0]
	assert.Nil(t, saved.JobID)
	require.NotNil(t, saved.SourceURL)
	assert.Equal(t, "https://jobs.example/1", *saved.SourceURL)

	again, apiErr := svc.DraftFromLink(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, out.Body, again.Body, "the same link gives the letter back")
	assert.Equal(t, 1, h.draft.calls)
	assert.Equal(t, 1, again.Used, "without counting it")

	fresh := req
	fresh.Fresh = true
	out, apiErr = svc.DraftFromLink(context.Background(), uid, fresh)
	require.Nil(t, apiErr)
	assert.Equal(t, "Bonjour (2)", out.Body)
	assert.Equal(t, 2, out.Used)

	// The month's letters are used up: the last letter for the link comes back, without reading the link again.
	offers.calls = 0
	again, apiErr = svc.DraftFromLink(context.Background(), uid, req)
	require.Nil(t, apiErr)
	assert.Equal(t, "Bonjour (2)", again.Body)
	assert.Equal(t, "jobs.example", again.Offer.Source)
	assert.Zero(t, offers.calls)
	_, apiErr = svc.DraftFromLink(context.Background(), uid, fresh)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	_, apiErr = svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "https://jobs.example/2"})
	require.NotNil(t, apiErr, "another link has no letter to give back")
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Equal(t, 2, h.draft.calls)
}

func TestDraftFromLinkNotInThePlanReadsNothing(t *testing.T) {
	h, svc, offers := linkHarness(map[string]int{core.QuotaAIDrafts: 0})
	_, apiErr := svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "https://jobs.example/1"})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Zero(t, offers.calls)
	assert.Zero(t, h.draft.calls)
}

func TestDraftFromLinkUnreadable(t *testing.T) {
	h, svc, offers := linkHarness(nil)
	offers.err = fmt.Errorf("%w: status 422", core.ErrPeerUnprocessable)
	_, apiErr := svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "https://www.linkedin.com/jobs/view/1"})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusUnprocessableEntity, apiErr.Code)
	assert.Equal(t, "this offer cannot be read: paste its text", apiErr.Error.Error())

	// The pasted text goes to job-api; a missing title gets a fallback and a long text is cut for the AI.
	offers.err = nil
	offers.offer = &core.OfferText{Source: "linkedin", Description: strings.Repeat("é", 15000)}
	out, apiErr := svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "https://www.linkedin.com/jobs/view/1", OfferText: "  Stage chez Acme  "})
	require.Nil(t, apiErr)
	assert.Equal(t, "Stage chez Acme", offers.gotText)
	assert.Equal(t, "Stage PFE", h.draft.in.JobTitle)
	assert.Equal(t, "linkedin", out.Offer.Source)
	assert.LessOrEqual(t, len(h.draft.in.JobDescription), 20000)
	assert.True(t, strings.HasSuffix(h.draft.in.JobDescription, "é"), "cut on a character boundary")

	offers.err = core.ErrPeerUnavailable
	_, apiErr = svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "https://jobs.example/9"})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.Code)

	_, apiErr = svc.DraftFromLink(context.Background(), uid, api.DraftFromLinkRequest{URL: "javascript:alert(1)"})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)
}

func TestDraftForRecruiter(t *testing.T) {
	h, svc, _ := linkHarness(map[string]int{core.QuotaAIDrafts: 1})
	recs := &memRecruiters{at: func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) }}
	svc.Recruiters = recs
	_, _, _ = recs.SaveSearch(context.Background(), &db.RecruiterSearch{UserID: uid, Provider: "chain", Query: []byte(`{"contract":"pfe","keywords":["C++","paiement"]}`)}, []db.Recruiter{
		{Name: "Sarra Gharbi", Title: "Responsable RH", Company: "Acme", Email: "sarra@acme.example", DedupeKey: "sarra@acme.example",
			CompanySummary: "Acme builds data platforms."},
	})
	id := "44444444-4444-4444-4444-444444444444"
	recs.people[0].ID = id

	_, apiErr := svc.DraftForRecruiter(context.Background(), "55555555-5555-5555-5555-555555555555", id, api.RecruiterDraftRequest{})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code, "another student's recruiter")
	_, apiErr = svc.DraftForRecruiter(context.Background(), uid, "nope", api.RecruiterDraftRequest{})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.Code)

	out, apiErr := svc.DraftForRecruiter(context.Background(), uid, id, api.RecruiterDraftRequest{ResumeID: resumID, Goal: " Stage  PFE data "})
	require.Nil(t, apiErr)
	assert.Equal(t, "Bonjour (1)", out.Body)
	assert.Equal(t, 1, out.Used)
	assert.Equal(t, "Stage PFE data", h.draft.in.JobTitle)
	assert.Equal(t, "Stage PFE data", h.draft.in.Goal, "the goal also reaches the AI as the aim of the email")
	assert.Equal(t, "Acme", h.draft.in.Company)
	assert.Contains(t, h.draft.in.JobDescription, "Responsable RH")
	assert.Contains(t, h.draft.in.JobDescription, "Stage PFE data")
	assert.Contains(t, h.draft.in.JobDescription, "Contract wanted: pfe", "the contract comes from the search")
	assert.Contains(t, h.draft.in.JobDescription, "Acme builds data platforms.", "the company summary is context")
	assert.NotContains(t, h.draft.in.JobDescription, "Sarra", "the recruiter's name never reaches the AI")
	assert.NotContains(t, h.draft.in.JobDescription, "sarra@")
	assert.Equal(t, "Ines, ENIS", h.draft.in.CandidateSummary)
	assert.Equal(t, []string{"C++", "paiement"}, h.draft.in.Keywords, "the search keywords reach the AI")
	saved := svc.Drafts.(*memDrafts).rows[0]
	require.NotNil(t, saved.SourceURL)
	assert.Equal(t, "recruiter:"+id, *saved.SourceURL)

	again, apiErr := svc.DraftForRecruiter(context.Background(), uid, id, api.RecruiterDraftRequest{})
	require.Nil(t, apiErr, "at the limit, the last letter to this recruiter comes back")
	assert.Equal(t, "Bonjour (1)", again.Body)
	_, apiErr = svc.DraftForRecruiter(context.Background(), uid, id, api.RecruiterDraftRequest{Fresh: true})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)
	assert.Equal(t, 1, h.draft.calls)

	svc.Quotas = mapQuota{core.QuotaAIDrafts: 0}
	_, apiErr = svc.DraftForRecruiter(context.Background(), uid, id, api.RecruiterDraftRequest{})
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusPaymentRequired, apiErr.Code)

	svc.Quotas = mapQuota{}
	out, apiErr = svc.DraftForRecruiter(context.Background(), uid, id, api.RecruiterDraftRequest{})
	require.Nil(t, apiErr)
	assert.Equal(t, "Candidature spontanée", h.draft.in.JobTitle, "no goal")
	assert.Equal(t, "Bonjour (2)", out.Body)
}
