package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
)

// FollowUpAfter is how long an application waits for an answer before its follow-up (« relance ») can be written.
const FollowUpAfter = 7 * 24 * time.Hour

// followUpSubject is the subject of a follow-up: the first one, as a reply.
func followUpSubject(first string) string {
	first = strings.TrimSpace(first)
	if strings.HasPrefix(strings.ToLower(first), "re:") {
		return first
	}
	return "Re: " + first
}

// DraftFollowUp writes the follow-up of an application with the AI, under the source "followup:<id>": asked again,
// the last letter comes back without being counted.
func (s *emailService) DraftFollowUp(ctx context.Context, userID, applicationID string, fresh bool, in core.DraftInput) (*api.DraftEmailResponse, *models.ApiError) {
	source := "followup:" + applicationID
	if cached, apiErr := s.sourceQuota(ctx, userID, source, in.Language, in.Tone, fresh); apiErr != nil || cached != nil {
		if cached == nil {
			return nil, apiErr
		}
		return cached, nil
	}
	return s.draftForSource(ctx, userID, source, "", fresh, in)
}

// SendFollowUp sends a follow-up now. Unlike an application, it is part of the subscription only (a student without
// one keeps their few free applications for offers) and never carries a CV.
func (s *emailService) SendFollowUp(ctx context.Context, userID string, req api.SendEmailRequest) (*db.Email, *models.ApiError) {
	if _, _, apiErr := s.Gmail.Credentials(ctx, userID); apiErr != nil {
		return nil, apiErr
	}
	a, apiErr := s.allowance(ctx, userID)
	if apiErr != nil {
		return nil, apiErr
	}
	switch {
	case a.Mode != AllowPlan:
		return nil, apiError(http.StatusPaymentRequired, "the follow-up is part of the subscription")
	case a.Limit != nil && a.Used >= *a.Limit:
		return nil, apiError(http.StatusPaymentRequired, "monthly email limit of your plan reached")
	}
	e, err := s.Repo.Create(ctx, &db.Email{
		UserID: userID, JobID: optional(req.JobID), Recipient: req.To,
		Subject: strings.TrimSpace(req.Subject), Body: req.Body, Status: db.EmailDraft,
	})
	if err != nil {
		return nil, repoError(err, "email not found")
	}
	return s.deliver(ctx, e.ID, false)
}

// followUpReady says why an application cannot be followed up yet, or nil. A follow-up answers silence: the
// application left (and was not cancelled), nobody answered, a week went by, and none was sent.
func followUpReady(a *db.Application, e *db.Email, status string, now time.Time) *models.ApiError {
	switch {
	case a.FollowUpEmailID != nil || a.FollowUpSentAt != nil:
		return apiError(http.StatusConflict, "a follow-up was already sent for this application")
	case status != db.AppSent && status != db.AppOpened:
		return apiError(http.StatusConflict, "only an application waiting for an answer can be followed up")
	}
	left := a.CreatedAt
	if e != nil && e.SentAt != nil {
		left = *e.SentAt
	}
	if now.Sub(left) < FollowUpAfter {
		return apiError(http.StatusConflict, "a follow-up can be written a week after the application")
	}
	return nil
}

// DraftFollowUp: POST /applications/:id/follow-up/draft. An application the student sent themselves has no first
// email: the AI writes from the offer alone.
func (s *applicationService) DraftFollowUp(ctx context.Context, userID, id string, req api.FollowUpDraftRequest) (*api.DraftEmailResponse, *models.ApiError) {
	a, e, apiErr := s.load(ctx, userID, id)
	if apiErr != nil {
		return nil, apiErr
	}
	now := s.now()
	if apiErr := followUpReady(a, e, shownStatus(a, e), now); apiErr != nil {
		return nil, apiErr
	}
	language, tone := req.Language, req.Tone
	if language == "" {
		language = "fr"
	}
	if tone == "" {
		tone = "formal"
	}
	in := core.DraftInput{Kind: "follow_up", JobTitle: a.JobTitle, Company: a.Company, Language: language, Tone: tone}
	left := a.CreatedAt
	if e != nil {
		in.Previous = "Subject: " + e.Subject + "\n\n" + e.Body
		if e.SentAt != nil {
			left = *e.SentAt
		}
	}
	in.DaysSince = int(now.Sub(left).Hours() / 24)
	out, apiErr := s.emails.DraftFollowUp(ctx, userID, a.ID, req.Fresh, in)
	if apiErr != nil {
		return nil, apiErr
	}
	if e != nil {
		// The follow-up answers the first email: its subject says so, whatever the AI proposed.
		out.Subject = followUpSubject(e.Subject)
	}
	return out, nil
}

// FollowUp: POST /applications/:id/follow-up. Only an application sent from Gmail: the follow-up goes to the same
// recipient, so the student never types (nor sees changed) an address here.
func (s *applicationService) FollowUp(ctx context.Context, userID, id string, req api.FollowUpRequest) (*api.ApplicationResponse, *models.ApiError) {
	a, e, apiErr := s.load(ctx, userID, id)
	if apiErr != nil {
		return nil, apiErr
	}
	if a.Via != db.ViaGmail || e == nil {
		return nil, apiError(http.StatusUnprocessableEntity, "this application was not sent from Gmail: send the follow-up yourself")
	}
	if apiErr := followUpReady(a, e, shownStatus(a, e), s.now()); apiErr != nil {
		return nil, apiErr
	}
	if strings.TrimSpace(req.Subject) == "" || strings.TrimSpace(req.Body) == "" {
		return nil, apiError(http.StatusBadRequest, "subject and body are required")
	}
	sent, apiErr := s.emails.SendFollowUp(ctx, userID, api.SendEmailRequest{JobID: a.JobID, To: e.Recipient, Subject: req.Subject, Body: req.Body})
	if sent == nil || sent.Status == db.EmailFailed {
		if apiErr == nil {
			apiErr = apiError(http.StatusBadGateway, "the email could not be sent")
		}
		return nil, apiErr
	}
	at := s.now()
	if sent.SentAt != nil {
		at = *sent.SentAt
	}
	saved, err := s.repo.SetFollowUp(ctx, userID, a.ID, sent.ID, at)
	if err != nil {
		if errors.Is(err, repositories.ErrConflict) {
			return nil, apiError(http.StatusConflict, "a follow-up was already sent for this application")
		}
		return nil, repoError(err, "application not found")
	}
	return toResponse(saved, e), nil
}
