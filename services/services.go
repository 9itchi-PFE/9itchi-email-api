package services

import (
	"9itchi-email-api/core"
	"9itchi-email-api/pkg/ai"
	"9itchi-email-api/pkg/google"
	"9itchi-email-api/pkg/job"
	"9itchi-email-api/pkg/payments"
	"9itchi-email-api/pkg/resume"
	"9itchi-email-api/pkg/schedule"
	"9itchi-email-api/pkg/tracker"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"
)

// Services holds service implementations and the peer clients they call.
type Services struct {
	// Erase removes what this service keeps about a deleted account.
	Erase       EraseService
	Gmail       GmailService
	Email       EmailService
	Application ApplicationService
	Clients     *Clients
}

// Clients holds one HTTP client per peer service (singletons, 50s timeout).
type Clients struct {
	Ai       *ai.Client
	Resume   *resume.Client
	Job      *job.Client
	Tracker  *tracker.Client
	Schedule *schedule.Client
	Payments *payments.Client
	Google   *google.Client
}

func InitServices(config utils.AppConfig, repos *repositories.Repositories) *Services {
	clients := &Clients{
		Ai:       ai.New(config.AiAPIURL, config.InternalAPIKey),
		Resume:   resume.New(config.ResumeAPIURL, config.InternalAPIKey),
		Job:      job.New(config.JobAPIURL, config.InternalAPIKey),
		Tracker:  tracker.New(config.TrackerAPIURL, config.InternalAPIKey),
		Schedule: schedule.New(config.ScheduleAPIURL, config.InternalAPIKey),
		Payments: payments.New(config.PaymentsAPIURL, config.InternalAPIKey),
		Google:   google.New(config.GoogleClientID, config.GoogleClientSecret, config.GoogleRedirectURL),
	}
	gmail := NewGmailService(repos.Gmail, clients.Google, config)
	emails := NewEmailService(EmailDeps{
		Repo: repos.Email, Gmail: gmail, Mailer: clients.Google, Drafter: clients.Ai, Jobs: clients.Job,
		Resumes: clients.Resume, Tracker: clients.Tracker, Scheduler: clients.Schedule, Quotas: clients.Payments,
		Events: core.NewPublisher(config.RabbitMQURL), Config: config,
	})
	return &Services{
		Erase:       NewEraseService(repos.Erase),
		Gmail:       gmail,
		Email:       emails,
		Application: NewApplicationService(repos.Application, emails, clients.Job),
		Clients:     clients,
	}
}
