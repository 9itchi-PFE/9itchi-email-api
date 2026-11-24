package handlers

import (
	"net/http"

	"9itchi-email-api/models"
	"9itchi-email-api/services"

	"github.com/gin-gonic/gin"
)

// ConnectGmail: GET /gmail/connect (user) start Gmail OAuth2 (send-only scope).
func ConnectGmail(svc services.GmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.ConnectGmail(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// GmailCallback: GET /gmail/callback (public) finish OAuth2, store AES-GCM encrypted token.
func GmailCallback(svc services.GmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		redirect, apiErr := svc.GmailCallback(c.Request.Context(), c.Query("code"), c.Query("state"), c.Query("error"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.Redirect(http.StatusFound, redirect)
	}
}

// GmailStatus: GET /gmail/status (user) connection state.
func GmailStatus(svc services.GmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.GmailStatus(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

// RevokeGmail: DELETE /gmail/connection (user) revoke access and delete token.
func RevokeGmail(svc services.GmailService) gin.HandlerFunc {
	return func(c *gin.Context) {
		result, apiErr := svc.RevokeGmail(c.Request.Context(), c.GetString("user_id"))
		if apiErr != nil {
			models.FormatError(c, apiErr)
			return
		}
		c.JSON(http.StatusOK, result)
	}
}
