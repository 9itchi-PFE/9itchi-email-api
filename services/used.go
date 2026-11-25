package services

import (
	"context"
	"net/http"
	"strings"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
)

// UserBuiltSends tells the team whether a student sent applications with the CV built in 9itchi and when they last
// did: one sent after a payment means the payment was used. Applications sent with the imported file (no
// subscription needed) and those still scheduled do not count. A student with none gets zero, not an error.
func (s *emailService) UserBuiltSends(ctx context.Context, id string) (*api.UserBuiltSendsResponse, *models.ApiError) {
	if !uuidRe.MatchString(id) {
		return nil, apiError(http.StatusBadRequest, "invalid user id")
	}
	n, last, err := s.Repo.LastBuiltSend(ctx, strings.ToLower(id))
	if err != nil {
		return nil, repoError(err, "not found")
	}
	return &api.UserBuiltSendsResponse{Count: n, LastAt: last}, nil
}
