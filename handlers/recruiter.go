package handlers

import (
	"net/http"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/services"

	"github.com/gin-gonic/gin"
)

// SearchRecruiters: POST /recruiters/search (user) recruiters for a CV, keywords, a company or a domain.
func SearchRecruiters(svc services.RecruiterService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.RecruiterSearchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.Search(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// ListRecruiters: GET /recruiters (user) every recruiter found for the student.
func ListRecruiters(svc services.RecruiterService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.List(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// RecruiterAllowance: GET /recruiters/allowance (user) the plan's daily limit of searches and today's use.
func RecruiterAllowance(svc services.RecruiterService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.Allowance(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// ProspectStats: GET /admin/prospects/stats (admin) the shared pool, its hit rates and the daily credits.
func ProspectStats(svc services.PoolStatsService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.Stats(c.Request.Context())
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// DraftForRecruiter: POST /recruiters/:id/draft (user) AI draft of a spontaneous application to a recruiter.
func DraftForRecruiter(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.RecruiterDraftRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.DraftForRecruiter(c.Request.Context(), c.GetString("user_id"), c.Param("id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
