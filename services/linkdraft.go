package services

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

const (
	maxLinkLength = 2048
	// maxLinkDescription is how much of an offer found elsewhere the AI reads.
	maxLinkDescription = 20000
	// linkTitleFallback names an offer whose title could not be read.
	linkTitleFallback = "Stage PFE"
	// SourceItchi is the source of a link to an offer published on 9itchi.
	SourceItchi = "9itchi"
)

// offerPathRe is the path of an offer page of the client: /offres/<uuid>.
var offerPathRe = regexp.MustCompile(`^/offres/([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})/?$`)

// normalizeLink keeps an http(s) link without its fragment nor its utm_* parameters, host lower case.
func normalizeLink(raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return nil, false
	}
	u.Host, u.Fragment, u.RawFragment = strings.ToLower(u.Host), "", ""
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if strings.HasPrefix(strings.ToLower(k), "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	if len(u.String()) > maxLinkLength {
		return nil, false
	}
	return u, true
}

// itchiJobID is the offer id when the link is an offer page of 9itchi itself (CLIENT_URL, with or without www.).
func (s *emailService) itchiJobID(u *url.URL) string {
	client, err := url.Parse(s.Config.ClientURL)
	if err != nil || client.Hostname() == "" {
		return ""
	}
	if strings.TrimPrefix(u.Hostname(), "www.") != strings.TrimPrefix(strings.ToLower(client.Hostname()), "www.") {
		return ""
	}
	m := offerPathRe.FindStringSubmatch(u.Path)
	if m == nil {
		return ""
	}
	return strings.ToLower(m[1])
}

// linkSource is "linkedin" for a LinkedIn link, else the host without www.
func linkSource(u *url.URL) string {
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if host == "linkedin.com" || strings.HasSuffix(host, ".linkedin.com") {
		return "linkedin"
	}
	return host
}

// clip cuts s to at most n bytes, on a character boundary.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// DraftFromLink writes the letter for an offer given by its link. A 9itchi offer page is the usual draft of that
// offer. Any other link is read by job-api (never stored there); its letter is kept under the link, so asking
// again gives it back without counting it. The plan is checked before anything is read: with no letter left this
// month, the last letter written for the link comes back, else 402.
func (s *emailService) DraftFromLink(ctx context.Context, userID string, req api.DraftFromLinkRequest) (*api.DraftFromLinkResponse, *models.ApiError) {
	u, ok := normalizeLink(req.URL)
	if !ok {
		return nil, apiError(http.StatusBadRequest, "invalid offer link")
	}
	language, tone := req.Language, req.Tone
	if language == "" {
		language = "fr"
	}
	if tone == "" {
		tone = "formal"
	}
	if jobID := s.itchiJobID(u); jobID != "" {
		out, job, apiErr := s.draftForJob(ctx, userID, api.DraftEmailRequest{
			JobID: jobID, ResumeID: req.ResumeID, Language: language, Tone: tone, Fresh: req.Fresh,
		})
		if apiErr != nil {
			return nil, apiErr
		}
		return &api.DraftFromLinkResponse{
			Subject: out.Subject, Body: out.Body, Limit: out.Limit, Used: out.Used, JobID: &jobID,
			Offer: api.LinkOffer{Source: SourceItchi, Title: job.Title, Company: job.Company},
		}, nil
	}
	link := u.String()

	cached, apiErr := s.sourceQuota(ctx, userID, link, language, tone, req.Fresh)
	if apiErr != nil || cached != nil {
		if cached == nil {
			return nil, apiErr
		}
		return &api.DraftFromLinkResponse{
			Subject: cached.Subject, Body: cached.Body, Limit: cached.Limit, Used: cached.Used, Offer: api.LinkOffer{Source: linkSource(u)},
		}, nil
	}

	offer, err := s.Offers.ReadOffer(ctx, link, strings.TrimSpace(req.OfferText))
	if errors.Is(err, core.ErrPeerUnprocessable) {
		return nil, apiError(http.StatusUnprocessableEntity, "this offer cannot be read: paste its text")
	}
	if err != nil {
		return nil, peerError(err, "offer not found")
	}
	title := strings.TrimSpace(offer.Title)
	if title == "" {
		title = linkTitleFallback
	}
	shown := api.LinkOffer{Source: offer.Source, Title: title, Company: offer.Company}
	if shown.Source == "" {
		shown.Source = linkSource(u)
	}
	out, apiErr := s.draftForSource(ctx, userID, link, req.ResumeID, req.Fresh, core.DraftInput{
		JobTitle: title, Company: offer.Company, JobDescription: clip(offer.Description, maxLinkDescription),
		Language: language, Tone: tone,
	})
	if apiErr != nil {
		return nil, apiErr
	}
	return &api.DraftFromLinkResponse{Subject: out.Subject, Body: out.Body, Limit: out.Limit, Used: out.Used, Offer: shown}, nil
}

// sourceQuota checks the plan's AI letters before a letter is written for a source (a link, a recruiter): none in
// the plan is 402; none left this month gives back the last letter written for the source (not fresh), else 402.
// Both nil: a letter may be written.
func (s *emailService) sourceQuota(ctx context.Context, userID, source, language, tone string, fresh bool) (*api.DraftEmailResponse, *models.ApiError) {
	limit, used, apiErr := s.monthlyUse(ctx, userID, core.QuotaAIDrafts, s.Drafts.CountSince)
	if apiErr != nil {
		return nil, apiErr
	}
	switch {
	case limit != nil && *limit == 0:
		return nil, apiError(http.StatusPaymentRequired, "the AI letter is part of the subscription")
	case limit != nil && used >= *limit:
		if !fresh {
			d, err := s.Drafts.LatestAnyByURL(ctx, userID, source, language, tone)
			if err == nil {
				return &api.DraftEmailResponse{Subject: d.Subject, Body: d.Body, Limit: limit, Used: used}, nil
			}
			if !errors.Is(err, repositories.ErrNotFound) {
				return nil, repoError(err, "draft not found")
			}
		}
		return nil, apiError(http.StatusPaymentRequired, "monthly limit of AI letters of your plan reached")
	}
	return nil, nil
}

// draftForSource writes (or, not fresh, gives back) the letter for a source, kept in ai_drafts under it. The CV
// summary is read when resumeID is given.
func (s *emailService) draftForSource(ctx context.Context, userID, source, resumeID string, fresh bool, in core.DraftInput) (*api.DraftEmailResponse, *models.ApiError) {
	token, err := s.bearer(userID)
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "internal error")
	}
	if resumeID != "" {
		if in.CandidateSummary, err = s.Resumes.Summary(ctx, token, resumeID); err != nil {
			return nil, peerError(err, "resume not found")
		}
	}
	hash := digest(in.JobTitle, in.Company, in.JobDescription, in.CandidateSummary)
	if len(in.Keywords) > 0 {
		// Only letters with keywords hash them, so the letters already kept stay valid.
		hash = digest(in.JobTitle, in.Company, in.JobDescription, in.CandidateSummary, strings.Join(in.Keywords, ","))
	}
	if in.Kind != "" {
		// Only a follow-up hashes its kind and first email, so the letters already kept stay valid.
		hash = digest(in.JobTitle, in.Company, in.JobDescription, in.CandidateSummary, in.Kind, in.Previous)
	}
	if !fresh {
		d, err := s.Drafts.LatestByURL(ctx, userID, source, hash, in.Language, in.Tone)
		switch {
		case err == nil:
			return s.draftResponse(ctx, userID, d.Subject, d.Body)
		case !errors.Is(err, repositories.ErrNotFound):
			return nil, repoError(err, "draft not found")
		}
	}
	subject, body, err := s.Drafter.Draft(ctx, token, in)
	if err != nil {
		return nil, peerError(err, "offer not found")
	}
	if _, err := s.Drafts.Save(ctx, &db.AIDraft{
		UserID: userID, SourceURL: &source, Language: in.Language, Tone: in.Tone, InputHash: hash, Subject: subject, Body: body,
	}); err != nil {
		return nil, repoError(err, "draft not found")
	}
	return s.draftResponse(ctx, userID, subject, body)
}
