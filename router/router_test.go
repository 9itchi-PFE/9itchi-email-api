package router

import (
	"net/http"
	"strings"
	"testing"

	"9itchi-email-api/services"
	"9itchi-email-api/utils"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// Every route registers without a conflict (gin panics on one), the recruiter routes among them.
func TestRoutesRegister(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var r *gin.Engine
	assert.NotPanics(t, func() { r = InitializeRouter(&services.Services{}) })
	got := map[string]bool{}
	for _, route := range r.Routes() {
		got[route.Method+" "+route.Path] = true
	}
	for _, want := range []string{
		"POST /recruiters/search", "GET /recruiters", "GET /recruiters/allowance", "POST /recruiters/:id/draft",
		"POST /emails/draft-from-link", "PATCH /emails/:id", "GET /admin/prospects/stats",
	} {
		assert.True(t, got[want], want)
	}
}

// The pool stats are for admins only: a student's token gets 403 before any service is called.
func TestProspectStatsAdminOnly(t *testing.T) {
	t.Setenv("ITCHI_SECRET_KEY", strings.Repeat("s", 32))
	t.Setenv("ITCHI_ENCRYPTION_KEY", strings.Repeat("e", 32))
	gin.SetMode(gin.TestMode)
	r := InitializeRouter(&services.Services{})
	assert.Equal(t, http.StatusUnauthorized, do(r, "/admin/prospects/stats", ""))
	assert.Equal(t, http.StatusForbidden, do(r, "/admin/prospects/stats", "Bearer "+tokenFor(t, "candidate", utils.TokenAccess)))
}
