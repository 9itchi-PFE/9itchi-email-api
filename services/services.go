package services

import (
	"slices"

	"9itchi-email-api/core"
	"9itchi-email-api/pkg/ai"
	"9itchi-email-api/pkg/google"
	"9itchi-email-api/pkg/job"
	"9itchi-email-api/pkg/payments"
	"9itchi-email-api/pkg/recruiters"
	"9itchi-email-api/pkg/resume"
	"9itchi-email-api/pkg/schedule"
	"9itchi-email-api/pkg/statistics"
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
	Match       MatchService
	Score       ScoreService
	Recruiter   RecruiterService
	PoolStats   PoolStatsService
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

// InitServices builds the services. It fails on an unknown recruiter source.
func InitServices(config utils.AppConfig, repos *repositories.Repositories) (*Services, error) {
	// The admin credit tracker: providers report what they spent, and a token an admin set wins over the env key.
	credits := statistics.New(config.StatisticsAPIURL, config.InternalAPIKey)
	rcfg := recruiters.Config{HunterAPIKey: config.HunterAPIKey, ApolloAPIKey: config.ApolloAPIKey}
	if credits.Enabled() {
		rcfg.Token = credits.Token
	}
	finders, err := recruiters.NewChain(config.RecruiterChain, rcfg)
	if err != nil {
		return nil, err
	}
	discoverers, err := recruiters.NewDiscovery(config.CompanyDiscovery, rcfg)
	if err != nil {
		return nil, err
	}
	if config.Environment == "production" && (slices.Contains(config.RecruiterChain, "fake") || slices.Contains(config.CompanyDiscovery, "fake")) {
		utils.Logger.Logger.Warnw("recruiter search uses the fake source: its companies and people are invented (.example domains)")
	}
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
	// One publisher for the service: emails sent or failed, first applications, replies.
	events := core.NewPublisher(config.RabbitMQURL)
	emails := NewEmailService(EmailDeps{
		Repo: repos.Email, Gmail: gmail, Mailer: clients.Google, Drafter: clients.Ai, Jobs: clients.Job,
		Resumes: clients.Resume, Tracker: clients.Tracker, Scheduler: clients.Schedule, Quotas: clients.Payments,
		Events: events, Config: config, Drafts: repos.Draft, Offers: clients.Job, Recruiters: repos.Recruiter,
	})
	caps := map[string]int{"hunter": config.HunterMonthlyCap, "apollo": config.ApolloMonthlyCap}
	// The providers of the chain that spend credits (the capped ones), listed on every day of the admin stats.
	var creditProviders []string
	for _, name := range config.RecruiterChain {
		if _, ok := caps[name]; ok && !slices.Contains(creditProviders, name) {
			creditProviders = append(creditProviders, name)
		}
	}
	matchDeps := MatchDeps{
		Repo: repos.Match, Jobs: clients.Job, Resumes: clients.Resume, Matcher: clients.Ai, Quotas: clients.Payments, Config: config,
	}
	return &Services{
		Erase:       NewEraseService(repos.Erase),
		Gmail:       gmail,
		Email:       emails,
		Application: NewApplicationService(repos.Application, emails, clients.Job, events),
		Match:       NewMatchService(matchDeps),
		Score:       NewScoreService(matchDeps),
		Recruiter: NewRecruiterService(RecruiterDeps{
			Repo: repos.Recruiter, Pool: repos.Prospect, Offers: clients.Job, Finders: finders, Discoverers: discoverers,
			Caps:    caps,
			Credits: credits.Report, Skills: clients.Resume, Quotas: clients.Payments, Config: config,
		}),
		PoolStats: NewPoolStatsService(PoolStatsDeps{Repo: repos.PoolStats, CreditProviders: creditProviders}),
		Clients:   clients,
	}, nil
}
