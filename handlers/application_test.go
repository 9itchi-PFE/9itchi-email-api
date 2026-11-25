package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/services"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

type mockApplications struct {
	apply   api.ApplyRequest
	update  api.UpdateApplicationRequest
	id      string
	userID  string
	failing *models.ApiError
}

func (m *mockApplications) Apply(_ context.Context, userID string, req api.ApplyRequest) (*api.ApplicationResponse, *models.ApiError) {
	m.apply, m.userID = req, userID
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.ApplicationResponse{ID: "a1", JobID: req.JobID, Status: "sent", Via: "gmail"}, nil
}
func (m *mockApplications) EmailContext(_ context.Context, emailID string) (*api.EmailContextResponse, *models.ApiError) {
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.EmailContextResponse{EmailID: emailID, Company: "Acme", JobTitle: "Portail RH"}, nil
}
func (m *mockApplications) ListApplications(context.Context, string) (*api.ApplicationsResponse, *models.ApiError) {
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.ApplicationsResponse{Items: []api.ApplicationResponse{}}, nil
}
func (m *mockApplications) UpdateApplication(_ context.Context, _ string, id string, req api.UpdateApplicationRequest) (*api.ApplicationResponse, *models.ApiError) {
	m.id, m.update = id, req
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.ApplicationResponse{ID: id, Status: req.Status}, nil
}
func (m *mockApplications) SendNow(_ context.Context, _ string, id string) (*api.ApplicationResponse, *models.ApiError) {
	m.id = id
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.ApplicationResponse{ID: id, Status: "sent"}, nil
}

func call(h gin.HandlerFunc, method, route, target, body string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, route, func(c *gin.Context) { c.Set("user_id", "u1") }, h)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

const job = "22222222-2222-2222-2222-222222222222"

func TestApplyHandler(t *testing.T) {
	m := &mockApplications{}
	w := call(Apply(m), http.MethodPost, "/applications", "/applications", `{"job_id":"`+job+`","subject":"s","body":"b"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "u1", m.userID)
	assert.Equal(t, job, m.apply.JobID)

	m = &mockApplications{}
	w = call(Apply(m), http.MethodPost, "/applications", "/applications", `{"job_id":"`+job+`","via":"self"}`)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "self", m.apply.Via)

	for _, body := range []string{``, `{`, `{"subject":"s"}`, `{"job_id":"x"}`, `{"job_id":"` + job + `","via":"pigeon"}`, `{"job_id":"` + job + `","to":"not-an-email"}`} {
		w = call(Apply(&mockApplications{}), http.MethodPost, "/applications", "/applications", body)
		assert.Equal(t, http.StatusBadRequest, w.Code, body)
	}

	w = call(Apply(&mockApplications{failing: &models.ApiError{Code: 402, Error: assert.AnError}}), http.MethodPost, "/applications", "/applications",
		`{"job_id":"`+job+`","subject":"s","body":"b"}`)
	assert.Equal(t, http.StatusPaymentRequired, w.Code)
}

func TestListApplicationsHandler(t *testing.T) {
	w := call(ListApplications(&mockApplications{}), http.MethodGet, "/applications", "/applications", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"items":[]}`, w.Body.String())

	w = call(ListApplications(&mockApplications{failing: &models.ApiError{Code: 500, Error: assert.AnError}}), http.MethodGet, "/applications", "/applications", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func (m *mockApplications) UserApplications(_ context.Context, id string) (*api.ApplicationsResponse, *models.ApiError) {
	m.id = id
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.ApplicationsResponse{Items: []api.ApplicationResponse{{ID: "a1", Company: "Acme", Status: "sent"}}}, nil
}

func TestUserApplicationsHandler(t *testing.T) {
	m := &mockApplications{}
	w := call(UserApplications(m), http.MethodGet, "/admin/users/:id/applications", "/admin/users/"+job+"/applications", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, job, m.id, "the student is the one of the path, not the admin who asks")
	assert.Contains(t, w.Body.String(), `"company":"Acme"`)

	m.failing = &models.ApiError{Code: http.StatusBadRequest, Error: assert.AnError}
	w = call(UserApplications(m), http.MethodGet, "/admin/users/:id/applications", "/admin/users/nope/applications", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

type mockGmailStatus struct {
	services.GmailService
	id      string
	failing *models.ApiError
}

func (m *mockGmailStatus) UserGmailStatus(_ context.Context, id string) (*api.GmailStatusResponse, *models.ApiError) {
	m.id = id
	if m.failing != nil {
		return nil, m.failing
	}
	return &api.GmailStatusResponse{Connected: true}, nil
}

func TestUserGmailStatusHandler(t *testing.T) {
	m := &mockGmailStatus{}
	w := call(UserGmailStatus(m), http.MethodGet, "/admin/users/:id/gmail", "/admin/users/"+job+"/gmail", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, job, m.id)
	assert.JSONEq(t, `{"connected":true}`, w.Body.String())

	m.failing = &models.ApiError{Code: http.StatusInternalServerError, Error: assert.AnError}
	w = call(UserGmailStatus(m), http.MethodGet, "/admin/users/:id/gmail", "/admin/users/"+job+"/gmail", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestUpdateApplicationHandler(t *testing.T) {
	m := &mockApplications{}
	w := call(UpdateApplication(m), http.MethodPatch, "/applications/:id", "/applications/a1", `{"status":"replied"}`)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "a1", m.id)
	assert.Equal(t, "replied", m.update.Status)

	for _, body := range []string{``, `{}`, `{"status":"scheduled"}`, `{"status":"opened"}`} {
		w = call(UpdateApplication(&mockApplications{}), http.MethodPatch, "/applications/:id", "/applications/a1", body)
		assert.Equal(t, http.StatusBadRequest, w.Code, body)
	}

	w = call(UpdateApplication(&mockApplications{failing: &models.ApiError{Code: 409, Error: assert.AnError}}), http.MethodPatch,
		"/applications/:id", "/applications/a1", `{"status":"closed"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestSendApplicationNowHandler(t *testing.T) {
	m := &mockApplications{}
	w := call(SendApplicationNow(m), http.MethodPost, "/applications/:id/send", "/applications/a1/send", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "a1", m.id)

	w = call(SendApplicationNow(&mockApplications{failing: &models.ApiError{Code: 409, Error: assert.AnError}}), http.MethodPost,
		"/applications/:id/send", "/applications/a1/send", "")
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestEmailContextHandler(t *testing.T) {
	w := call(EmailContext(&mockApplications{}), http.MethodGet, "/internal/emails/:id/context", "/internal/emails/e1/context", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"email_id":"e1"`)
	assert.Contains(t, w.Body.String(), `"company":"Acme"`)

	w = call(EmailContext(&mockApplications{failing: &models.ApiError{Code: 404, Error: assert.AnError}}), http.MethodGet,
		"/internal/emails/:id/context", "/internal/emails/e1/context", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), `"error"`)
}

type mockAllowance struct {
	services.EmailService
	failing *models.ApiError
}

func (m mockAllowance) Allowance(context.Context, string) (*api.AllowanceResponse, *models.ApiError) {
	if m.failing != nil {
		return nil, m.failing
	}
	five := 5
	return &api.AllowanceResponse{Mode: services.AllowOriginal, Limit: &five, Used: 2}, nil
}

func TestAllowanceHandler(t *testing.T) {
	w := call(Allowance(mockAllowance{}), http.MethodGet, "/applications/allowance", "/applications/allowance", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"mode":"original","limit":5,"used":2}`, w.Body.String())

	w = call(Allowance(mockAllowance{failing: &models.ApiError{Code: http.StatusBadGateway, Error: assert.AnError}}), http.MethodGet, "/applications/allowance", "/applications/allowance", "")
	assert.Equal(t, http.StatusBadGateway, w.Code)
}
