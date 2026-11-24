package core

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrPeerUnavailable means a peer service could not be reached or answered with an error.
	ErrPeerUnavailable = errors.New("peer service unavailable")
	// ErrPeerNotFound means a peer service answered 404.
	ErrPeerNotFound = errors.New("peer resource not found")
	// ErrGmailAuth means Google refused the stored credentials (revoked or expired): the user must reconnect.
	ErrGmailAuth = errors.New("gmail authorization failed")
)

// QuotaEmailsPerMonth is the key of the monthly email limit in the payments plan quotas.
const QuotaEmailsPerMonth = "emails_per_month"

// GoogleToken is an OAuth2 token pair stored (encrypted) per user.
type GoogleToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// Attachment is a file sent with an email.
type Attachment struct {
	Filename string
	Data     []byte
}

// MailMessage is one outgoing email: an HTML body and an optional PDF.
type MailMessage struct {
	From       string
	To         string
	Subject    string
	HTML       string
	Attachment *Attachment
}

// Mailer is the Google side: OAuth2 and the Gmail send API.
type Mailer interface {
	Configured() bool
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (*GoogleToken, error)
	Refresh(ctx context.Context, refreshToken string) (*GoogleToken, error)
	UserEmail(ctx context.Context, accessToken string) (string, error)
	Revoke(ctx context.Context, token string) error
	Send(ctx context.Context, accessToken string, msg MailMessage) error
}

// DraftInput is what the AI needs to write an application email.
type DraftInput struct {
	JobTitle         string
	Company          string
	JobDescription   string
	CandidateSummary string
	Language         string
	Tone             string
}

// Drafter calls 9itchi-ai-api. bearer is a short-lived token of the user.
type Drafter interface {
	Draft(ctx context.Context, bearer string, in DraftInput) (subject, body string, err error)
}

// JobInfo is the part of an offer the email flow uses.
type JobInfo struct {
	Title       string
	Company     string
	Description string
}

// JobSource calls 9itchi-job-api.
type JobSource interface {
	Job(ctx context.Context, bearer, id string) (*JobInfo, error)
}

// ResumeSource calls 9itchi-resume-api.
type ResumeSource interface {
	// Summary is a short plain-text version of the CV.
	Summary(ctx context.Context, bearer, id string) (string, error)
	// PDF is the latest exported PDF of the CV, nil when none was exported.
	PDF(ctx context.Context, id string) ([]byte, error)
}

// Tracker calls 9itchi-tracker-api.
type Tracker interface {
	Register(ctx context.Context, emailID, userID string) (trackingID, pixelPath string, err error)
}

// Scheduler calls 9itchi-schedule-api.
type Scheduler interface {
	ScheduleEmail(ctx context.Context, emailID string, runAt time.Time) (taskID string, err error)
	Cancel(ctx context.Context, taskID string) error
}

// QuotaSource calls 9itchi-payments-api.
type QuotaSource interface {
	// Limit returns the plan limit for key; ok is false when the plan has none.
	Limit(ctx context.Context, userID, key string) (limit int, ok bool, err error)
}
