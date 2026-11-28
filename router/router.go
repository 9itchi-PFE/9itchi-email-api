package router

import (
	"9itchi-email-api/handlers"
	"9itchi-email-api/services"

	"github.com/gin-gonic/gin"
)

func InitializeRouter(services *services.Services) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())

	initializeMiddlewares(r)
	initializeRoutes(r, services)

	return r
}

func initializeRoutes(r *gin.Engine, services *services.Services) {
	// Health endpoints
	r.GET("/", handlers.GetHealth)
	r.GET("/health", handlers.GetHealth)

	// public endpoints
	{
		r.GET("/gmail/callback", handlers.GmailCallback(services.Gmail))
	}

	// user endpoints
	userGroup := r.Group("/")
	userGroup.Use(AuthMiddleware())
	{
		userGroup.GET("/gmail/connect", handlers.ConnectGmail(services.Gmail))
		userGroup.GET("/gmail/status", handlers.GmailStatus(services.Gmail))
		userGroup.DELETE("/gmail/connection", handlers.RevokeGmail(services.Gmail))
		userGroup.POST("/emails/draft", handlers.DraftEmail(services.Email))
		userGroup.POST("/emails/draft-from-link", handlers.DraftFromLink(services.Email))
		userGroup.POST("/emails", handlers.SendEmail(services.Email))
		userGroup.POST("/emails/schedule", handlers.ScheduleEmail(services.Email))
		userGroup.GET("/emails", handlers.ListEmails(services.Email))
		userGroup.GET("/emails/attachment-names", handlers.AttachmentNames(services.Email))
		userGroup.GET("/emails/:id", handlers.GetEmail(services.Email))
		userGroup.PATCH("/emails/:id", handlers.UpdateEmail(services.Email))
		userGroup.DELETE("/emails/:id", handlers.CancelEmail(services.Email))
		userGroup.POST("/applications", handlers.Apply(services.Application))
		userGroup.GET("/applications", handlers.ListApplications(services.Application))
		userGroup.GET("/applications/allowance", handlers.Allowance(services.Email))
		userGroup.POST("/applications/match", handlers.Match(services.Match))
		userGroup.GET("/applications/match", handlers.FindMatch(services.Match))
		userGroup.POST("/applications/scores", handlers.Score(services.Score))
		userGroup.GET("/applications/scores", handlers.FindScores(services.Score))
		userGroup.PATCH("/applications/:id", handlers.UpdateApplication(services.Application))
		userGroup.POST("/applications/:id/send", handlers.SendApplicationNow(services.Application))
		userGroup.POST("/applications/:id/follow-up/draft", handlers.DraftFollowUp(services.Application))
		userGroup.POST("/applications/:id/follow-up", handlers.FollowUp(services.Application))
		userGroup.POST("/recruiters/search", handlers.SearchRecruiters(services.Recruiter))
		userGroup.GET("/recruiters", handlers.ListRecruiters(services.Recruiter))
		userGroup.GET("/recruiters/allowance", handlers.RecruiterAllowance(services.Recruiter))
		userGroup.POST("/recruiters/:id/draft", handlers.DraftForRecruiter(services.Email))
	}

	// admin endpoints
	adminGroup := r.Group("/")
	adminGroup.Use(AuthMiddleware(), RequireRole("admin"))
	{
		adminGroup.GET("/admin/users/:id/applications", handlers.UserApplications(services.Application))
		adminGroup.GET("/admin/users/:id/gmail", handlers.UserGmailStatus(services.Gmail))
		adminGroup.GET("/admin/users/:id/sent-with-cv", handlers.UserBuiltSends(services.Email))
		adminGroup.GET("/admin/prospects/stats", handlers.ProspectStats(services.PoolStats))
	}

	// internal endpoints
	internalGroup := r.Group("/")
	internalGroup.Use(InternalAPIKeyMiddleware())
	{
		internalGroup.DELETE("/internal/users/:id", handlers.EraseUser(services.Erase))
		internalGroup.POST("/internal/emails/:id/dispatch", handlers.DispatchScheduled(services.Email))
		internalGroup.GET("/internal/emails/:id/context", handlers.EmailContext(services.Application))
	}

	// Fallback
	r.NoRoute(handlers.NoRoute)
}
