package handlers

import (
	"net/http"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/services"

	"github.com/gin-gonic/gin"
)

// Apply: POST /applications (user) one-click apply: prefilled email + chosen resume.
func Apply(svc services.ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.ApplyRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.Apply(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusCreated, result)
	}
}

// ListApplications: GET /applications (user) tracked applications.
func ListApplications(svc services.ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.ListApplications(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// Allowance: GET /applications/allowance (user) how the user's applications leave and how many are left.
func Allowance(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.Allowance(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// UpdateApplication: PATCH /applications/:id (user) the student says what happened: replied, withdrawn, closed, sent.
func UpdateApplication(svc services.ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.UpdateApplicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.UpdateApplication(c.Request.Context(), c.GetString("user_id"), c.Param("id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// SendApplicationNow: POST /applications/:id/send (user) send a scheduled application right away.
func SendApplicationNow(svc services.ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.SendNow(c.Request.Context(), c.GetString("user_id"), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// EmailContext: GET /internal/emails/:id/context (internal) the offer an email was sent for.
func EmailContext(svc services.ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.EmailContext(c.Request.Context(), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// UserApplications: GET /admin/users/:id/applications (admin) the applications of one student.
func UserApplications(svc services.ApplicationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.UserApplications(c.Request.Context(), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// Match: POST /applications/match (user) how well the student's CV fits an offer, read by the AI.
func Match(svc services.MatchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.MatchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.Match(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// FindMatch: GET /applications/match?job_id=&resume_id=&language= (user) the analysis already made, 404 when none.
func FindMatch(svc services.MatchService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.MatchRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.Find(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
