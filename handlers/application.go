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
