package router

import (
	"testing"

	"9itchi-email-api/services"

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
		"POST /emails/draft-from-link", "PATCH /emails/:id",
	} {
		assert.True(t, got[want], want)
	}
}
