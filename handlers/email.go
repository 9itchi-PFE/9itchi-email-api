package handlers

import (
	"net/http"

	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/services"

	"github.com/gin-gonic/gin"
)

// DraftEmail: POST /emails/draft (user) AI draft for a job + resume.
func DraftEmail(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.DraftEmailRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.DraftEmail(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// SendEmail: POST /emails (user) send immediately via Gmail.
func SendEmail(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.SendEmailRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.SendEmail(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// ScheduleEmail: POST /emails/schedule (user) schedule for date/time.
func ScheduleEmail(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req api.ScheduleEmailRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			models.FormatError(c, &models.ApiError{Code: http.StatusBadRequest, Error: err})
			return
		}
		result, apiErr := svc.ScheduleEmail(c.Request.Context(), c.GetString("user_id"), req)
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// ListEmails: GET /emails (user) history: draft, scheduled, sent, opened, failed.
func ListEmails(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.ListEmails(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// GetEmail: GET /emails/:id (user) email detail.
func GetEmail(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.GetEmail(c.Request.Context(), c.GetString("user_id"), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// CancelEmail: DELETE /emails/:id (user) cancel a scheduled email.
func CancelEmail(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.CancelEmail(c.Request.Context(), c.GetString("user_id"), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// DispatchScheduled: POST /internal/emails/:id/dispatch (internal) called by schedule-api when due.
func DispatchScheduled(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.DispatchScheduled(c.Request.Context(), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// UserBuiltSends: GET /admin/users/:id/sent-with-cv (admin) how many applications a student sent with the CV built
// in 9itchi and when the last one left.
func UserBuiltSends(svc services.EmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.UserBuiltSends(c.Request.Context(), c.Param("id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
