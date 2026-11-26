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

// Keys of the payments plan quotas read here.
const (
	// QuotaEmailsPerMonth is the monthly limit of applications sent with the CV built in 9itchi.
	QuotaEmailsPerMonth = "emails_per_month"
	// QuotaFreeApplications applies when the monthly limit is 0 (no subscription): how many applications still
	// leave, over the whole life of the account, with the file the student imported.
	QuotaFreeApplications = "free_applications"
	// QuotaAIMatches is the monthly limit of AI analyses of how well the CV fits an offer (absent: no limit;
	// 0: none). Reading an analysis already made again is not counted.
	QuotaAIMatches = "ai_matches_per_month"
	// QuotaAIDrafts is the monthly limit of letters written by the AI (absent: no limit; 0: none). A letter
	// already written for the same offer and CV, given back again, is not counted.
	QuotaAIDrafts = "ai_drafts_per_month"
)

// GoogleToken is an OAuth2 token pair stored (encrypted) per user.
type GoogleToken struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	Expiry       time.Time `json:"expiry"`
}

// Attachment is a file sent with an email.
type Attachment struct {
	Filename string
	// ContentType of the file; empty means a PDF.
	ContentType string
	Data        []byte
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
	Goal             string
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
	// ApplyEmail is where the offer takes applications; empty when it only takes them elsewhere.
	ApplyEmail string
	// Skills are the skills the offer names (empty from the internal view).
	Skills []string
	// Open is false once the deadline of the offer has passed.
	Open bool
}

// JobSource calls 9itchi-job-api.
type JobSource interface {
	Job(ctx context.Context, bearer, id string) (*JobInfo, error)
	// ApplyInfo is the internal view of a published offer, with the recruiter's address.
	ApplyInfo(ctx context.Context, id string) (*JobInfo, error)
}

// ResumeSource calls 9itchi-resume-api.
type ResumeSource interface {
	// Summary is a short plain-text version of the CV.
	Summary(ctx context.Context, bearer, id string) (string, error)
	// PDF is the latest exported PDF of the CV, nil when none was exported.
	PDF(ctx context.Context, id string) ([]byte, error)
	// Original is the file the student imported for the CV, as it came; nil when the CV has none.
	Original(ctx context.Context, id string) (*Attachment, error)
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
	// Quotas returns the limits of the user's plan (the active one, else the plan without subscription). A key
	// that is absent sets no limit.
	Quotas(ctx context.Context, userID string) (map[string]int, error)
}

// MatchInput is what the AI reads to say how well a CV fits an offer.
type MatchInput struct {
	JobTitle       string
	Company        string
	JobDescription string
	JobSkills      []string
	Candidate      string
	Language       string
}

// MatchResult is the AI's reading: a score out of 100, its level, strengths, gaps and one piece of advice, plus the
// offer's keywords found or not in the CV and the share found (0 to 100).
type MatchResult struct {
	Score           int
	Level           string
	Strengths       []string
	Gaps            []string
	Advice          string
	KeywordsMatched []string
	KeywordsMissing []string
	KeywordCoverage int
}

// Matcher calls 9itchi-ai-api (internal route).
type Matcher interface {
	Match(ctx context.Context, in MatchInput) (*MatchResult, error)
}
