package handlers

import (
	"context"
	"net/http"
	"testing"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/services"

	"github.com/stretchr/testify/assert"
)

type mockAttachmentNames struct {
	services.EmailService
	q       api.AttachmentNamesQuery
	failing *models.ApiError
}

func (m *mockAttachmentNames) AttachmentNames(_ context.Context, _ string, q api.AttachmentNamesQuery) (*api.AttachmentNamesResponse, *models.ApiError) {
	m.q = q
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.AttachmentNamesResponse{CVFilename: "Développeuse React.pdf", LetterFilename: "Lettre_de_motivation_Ines"}, nil
}

func TestAttachmentNamesHandler(t *testing.T) {
	const resume = "33333333-3333-3333-3333-333333333333"
	m := &mockAttachmentNames{}
	w := call(AttachmentNames(m), http.MethodGet, "/emails/attachment-names", "/emails/attachment-names?resume_id="+resume+"&language=ar", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"cv_filename":"Développeuse React.pdf","letter_filename":"Lettre_de_motivation_Ines"}`, w.Body.String())
	assert.Equal(t, api.AttachmentNamesQuery{ResumeID: resume, Language: "ar"}, m.q)

	for _, q := range []string{"?resume_id=x", "?language=de"} {
		w = call(AttachmentNames(&mockAttachmentNames{}), http.MethodGet, "/emails/attachment-names", "/emails/attachment-names"+q, "")
		assert.Equal(t, http.StatusBadRequest, w.Code, q)
	}

	w = call(AttachmentNames(&mockAttachmentNames{failing: &models.ApiError{Code: http.StatusNotFound, Error: assert.AnError}}),
		http.MethodGet, "/emails/attachment-names", "/emails/attachment-names?resume_id="+resume, "")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"error"`)
}

// The letter fields of an application reach the service; a letter name too long is refused.
func TestApplyHandlerLetterFields(t *testing.T) {
	m := &mockApplications{}
	w := call(Apply(m), http.MethodPost, "/applications", "/applications",
		`{"job_id":"`+job+`","subject":"s","body":"b","letter_filename":"Ma lettre","attach_letter":false,"language":"en"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "Ma lettre", m.apply.LetterFilename)
	if assert.NotNil(t, m.apply.AttachLetter) {
		assert.False(t, *m.apply.AttachLetter)
	}
	assert.Equal(t, "en", m.apply.Language)

	long := make([]byte, 121)
	for i := range long {
		long[i] = 'a'
	}
	for _, extra := range []string{`"letter_filename":"` + string(long) + `"`, `"language":"de"`} {
		w = call(Apply(&mockApplications{}), http.MethodPost, "/applications", "/applications", `{"job_id":"`+job+`","subject":"s","body":"b",`+extra+`}`)
		assert.Equal(t, http.StatusBadRequest, w.Code, extra)
	}
}
