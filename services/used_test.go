package services

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
)

func TestUserBuiltSendsForTheTeam(t *testing.T) {
	const student = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	h := newHarness(nil, unlimited)
	first, last := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 24, 18, 0, 0, 0, time.UTC)
	add := func(userID, status string, original bool, at *time.Time) {
		e, err := h.repo.Create(context.Background(), &db.Email{UserID: userID, Status: status, CVOriginal: original})
		require.NoError(t, err)
		h.repo.rows[e.ID].SentAt = at
	}
	res, apiErr := h.svc.UserBuiltSends(context.Background(), student)
	require.Nil(t, apiErr, "no application is an answer, not an error")
	assert.Equal(t, &api.UserBuiltSendsResponse{}, res)

	add(student, db.EmailSent, false, &last)
	add(student, db.EmailSent, false, &first)
	add(student, db.EmailSent, true, &last)     // the imported file: no subscription was needed
	add(student, db.EmailScheduled, false, nil) // has not left
	add(student, db.EmailFailed, false, nil)
	add("11111111-2222-4333-8444-555555555555", db.EmailSent, false, &last)

	res, apiErr = h.svc.UserBuiltSends(context.Background(), strings.ToUpper(student))
	require.Nil(t, apiErr)
	assert.Equal(t, &api.UserBuiltSendsResponse{Count: 2, LastAt: &last}, res)

	_, apiErr = h.svc.UserBuiltSends(context.Background(), "nope")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.Code)

	h.repo.builtErr = errors.New("db down")
	_, apiErr = h.svc.UserBuiltSends(context.Background(), student)
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusInternalServerError, apiErr.Code)
}
