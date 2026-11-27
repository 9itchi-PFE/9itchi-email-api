package utils

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

type AppConfig struct {
	Port        int
	Environment string
	// DatabaseURL is the PostgreSQL connection string (env DATABASE_URL). Never log it.
	DatabaseURL string
	// SecretKey signs JWTs (HS256), EncryptionKey wraps them with AES-GCM. Shared by every service.
	SecretKey     string
	EncryptionKey string
	// InternalAPIKey is the shared secret for service-to-service calls (env INTERNAL_API_KEY).
	InternalAPIKey string
	// InternalAPIKeyRequired (env INTERNAL_API_KEY_REQUIRED, default true); false = log and allow.
	InternalAPIKeyRequired bool
	RabbitMQURL            string
	// Peer service base URLs.
	AiAPIURL       string
	ResumeAPIURL   string
	JobAPIURL      string
	TrackerAPIURL  string
	ScheduleAPIURL string
	PaymentsAPIURL string
	// Gmail OAuth2 (Google Cloud client). GoogleRedirectURL must be registered in the Google console.
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	// ClientURL is where the browser lands after the Gmail callback.
	ClientURL string
	// TrackerPublicURL is the public base URL of the tracking pixel (opened by the recipient's mail client).
	TrackerPublicURL string
	// RecruiterChain is the contact finders of Recruiter Radar, in order (env RECRUITER_CHAIN, default
	// "website,hunter,apollo"; names registered in pkg/recruiters, an unknown one fails startup). CompanyDiscovery is the
	// company discoverers (env COMPANY_DISCOVERY, default "hunter"; empty = none). A source without its key is skipped.
	RecruiterChain   []string
	CompanyDiscovery []string
	// HunterAPIKey and ApolloAPIKey are the providers' keys (a token an admin set in statistics-api wins). Never log them.
	HunterAPIKey string
	ApolloAPIKey string
	// HunterMonthlyCap (searches) and ApolloMonthlyCap (reveals) cap the credits spent per UTC month (default 50 each).
	HunterMonthlyCap int
	ApolloMonthlyCap int
	// StatisticsAPIURL is the admin credit tracker (empty = off).
	StatisticsAPIURL string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() AppConfig {
	defer lockConfig()()
	viper.AutomaticEnv()
	return AppConfig{
		Port:                   getIntWithDefault("PORT", 8085),
		Environment:            getStringWithDefault("ENVIRONMENT", "development"),
		DatabaseURL:            getStringWithDefault("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/emails?sslmode=disable"),
		SecretKey:              getStringWithDefault("ITCHI_SECRET_KEY", ""),
		EncryptionKey:          getStringWithDefault("ITCHI_ENCRYPTION_KEY", ""),
		InternalAPIKey:         getStringWithDefault("INTERNAL_API_KEY", ""),
		InternalAPIKeyRequired: getBoolWithDefault("INTERNAL_API_KEY_REQUIRED", true),
		RabbitMQURL:            getStringWithDefault("RABBITMQ_URL", ""),
		AiAPIURL:               getStringWithDefault("AI_API_URL", "http://9itchi-ai-api:8083"),
		ResumeAPIURL:           getStringWithDefault("RESUME_API_URL", "http://9itchi-resume-api:8082"),
		JobAPIURL:              getStringWithDefault("JOB_API_URL", "http://9itchi-job-api:8084"),
		TrackerAPIURL:          getStringWithDefault("TRACKER_API_URL", "http://9itchi-tracker-api:8086"),
		ScheduleAPIURL:         getStringWithDefault("SCHEDULE_API_URL", "http://9itchi-schedule-api:8091"),
		PaymentsAPIURL:         getStringWithDefault("PAYMENTS_API_URL", "http://9itchi-payments-api:8089"),
		GoogleClientID:         getStringWithDefault("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:     getStringWithDefault("GOOGLE_CLIENT_SECRET", ""),
		GoogleRedirectURL:      getStringWithDefault("GMAIL_REDIRECT_URL", "http://localhost:8085/gmail/callback"),
		ClientURL:              getStringWithDefault("CLIENT_URL", "http://localhost:3000"),
		TrackerPublicURL:       getStringWithDefault("TRACKER_PUBLIC_URL", "http://localhost:8086"),
		RecruiterChain:         getListWithDefault("RECRUITER_CHAIN", "website,hunter,apollo"),
		CompanyDiscovery:       getListWithDefault("COMPANY_DISCOVERY", "hunter"),
		HunterAPIKey:           getStringWithDefault("HUNTER_API_KEY", ""),
		ApolloAPIKey:           getStringWithDefault("APOLLO_API_KEY", ""),
		HunterMonthlyCap:       getIntWithDefault("HUNTER_MONTHLY_CAP", 50),
		ApolloMonthlyCap:       getIntWithDefault("APOLLO_MONTHLY_CAP", 50),
		StatisticsAPIURL:       getStringWithDefault("STATISTICS_API_URL", "http://9itchi-statistics-api:8090"),
	}
}

// Validate fails fast on an unusable config. Only variable names are reported, never values.
func (c AppConfig) Validate() error {
	if c.Port < 1 {
		return fmt.Errorf("invalid PORT: must be a positive integer")
	}
	var bad []string
	if len(c.SecretKey) < 32 {
		bad = append(bad, "ITCHI_SECRET_KEY (min 32 chars)")
	}
	if len(c.EncryptionKey) < 32 {
		bad = append(bad, "ITCHI_ENCRYPTION_KEY (min 32 chars)")
	}
	if c.DatabaseURL == "" {
		bad = append(bad, "DATABASE_URL")
	}
	if c.InternalAPIKeyRequired && c.InternalAPIKey == "" {
		bad = append(bad, "INTERNAL_API_KEY")
	}
	if len(c.RecruiterChain) == 0 {
		bad = append(bad, "RECRUITER_CHAIN")
	}
	if c.HunterMonthlyCap < 0 {
		bad = append(bad, "HUNTER_MONTHLY_CAP (min 0)")
	}
	if c.ApolloMonthlyCap < 0 {
		bad = append(bad, "APOLLO_MONTHLY_CAP (min 0)")
	}
	if len(bad) > 0 {
		return fmt.Errorf("required environment variables are missing or invalid: %s", strings.Join(bad, ", "))
	}
	return nil
}

func getStringWithDefault(key, defaultValue string) string {
	viper.SetDefault(key, defaultValue)
	return viper.GetString(key)
}

func getIntWithDefault(key string, defaultValue int) int {
	viper.SetDefault(key, defaultValue)
	return viper.GetInt(key)
}

func getBoolWithDefault(key string, defaultValue bool) bool {
	viper.SetDefault(key, defaultValue)
	return viper.GetBool(key)
}

// getListWithDefault reads a comma-separated list, trimmed, without empty items; "none" is an empty list (viper
// reads an empty variable as unset).
func getListWithDefault(key, defaultValue string) []string {
	viper.SetDefault(key, defaultValue)
	var out []string
	for _, s := range strings.Split(viper.GetString(key), ",") {
		if s = strings.TrimSpace(s); s != "" && s != "none" {
			out = append(out, s)
		}
	}
	return out
}
