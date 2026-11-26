package services

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/repositories"
)

// spontaneousTitle names a spontaneous application when the student gave no goal.
const spontaneousTitle = "Candidature spontanée"

// DraftForRecruiter writes a spontaneous application to one recruiter of the student. The letter is kept and
// counted like a letter for a link, under the source "recruiter:<id>". Only the recruiter's title and company reach
// the AI, never their name nor address (the student adds the greeting).
func (s *emailService) DraftForRecruiter(ctx context.Context, userID, id string, req api.RecruiterDraftRequest) (*api.DraftEmailResponse, *models.ApiError) {
	if !uuidRe.MatchString(id) {
		return nil, apiError(http.StatusNotFound, "recruiter not found")
	}
	r, err := s.Recruiters.Get(ctx, userID, strings.ToLower(id))
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, apiError(http.StatusNotFound, "recruiter not found")
		}
		return nil, repoError(err, "recruiter not found")
	}
	language, tone := req.Language, req.Tone
	if language == "" {
		language = "fr"
	}
	if tone == "" {
		tone = "formal"
	}
	source := "recruiter:" + r.ID
	if cached, apiErr := s.sourceQuota(ctx, userID, source, language, tone, req.Fresh); apiErr != nil || cached != nil {
		if cached == nil {
			return nil, apiErr
		}
		return cached, nil
	}
	goal := strings.Join(strings.Fields(req.Goal), " ")
	title := goal
	if title == "" {
		title = spontaneousTitle
	}
	role := r.Title
	if role == "" {
		role = "recruiter"
	}
	brief := "Spontaneous application, not an answer to a published offer. Addressed to the " + role + " of " + r.Company + "."
	if goal != "" {
		brief += " The candidate is looking for: " + goal + "."
	}
	return s.draftForSource(ctx, userID, source, req.ResumeID, req.Fresh, core.DraftInput{
		JobTitle: title, Company: r.Company, JobDescription: brief, Language: language, Tone: tone, Goal: goal,
	})
}
