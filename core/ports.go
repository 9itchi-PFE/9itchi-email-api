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
	// ErrPeerUnprocessable means a peer service answered 422: it understood the request and cannot do it (an offer
	// link it cannot read).
	ErrPeerUnprocessable = errors.New("peer could not process the request")
	// ErrMailTooLarge means the message, attachments included, is larger than Gmail accepts.
	ErrMailTooLarge = errors.New("message larger than gmail allows")
)

// MaxMailBytes is the largest message Gmail accepts (25 MB), attachments included.
const MaxMailBytes = 25 << 20

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
	// QuotaAIScores is the daily limit (Tunis day) of quick AI scores of offers (absent or negative: no limit;
	// 0: none). A score or analysis already made for the same offer and CV is given back, not counted.
	QuotaAIScores = "ai_scores_per_day"
	// QuotaAIDrafts is the monthly limit of letters written by the AI (absent: no limit; 0: none). A letter
	// already written for the same offer and CV, given back again, is not counted.
	QuotaAIDrafts = "ai_drafts_per_month"
	// QuotaRecruiterSearches is the daily limit (Tunis day) of recruiter searches (absent or negative: no limit;
	// 0: none). The same search again the same day is not counted.
	QuotaRecruiterSearches = "recruiter_searches_per_day"
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

// MailMessage is one outgoing email: an HTML body and its files (the CV, the cover letter), in that order.
type MailMessage struct {
	From        string
	To          string
	Subject     string
	HTML        string
	Attachments []Attachment
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
	// Goal is what the student looks for (Recruiter Radar); ai-api writes the email around it.
	Goal string
	// Keywords are the terms the student chose for the search (Recruiter Radar); ai-api works in those the CV backs.
	Keywords []string
	// Kind is "follow_up" for a follow-up of an application (empty: a first email). Previous is the first email
	// (subject and body) and DaysSince how many days ago it left.
	Kind      string
	Previous  string
	DaysSince int
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
	// PDF is the CV as a PDF, named by resume-api after the CV's title (CV.pdf when it gives no name); nil when
	// the CV does not exist.
	PDF(ctx context.Context, id string) (*Attachment, error)
	// Original is the file the student imported for the CV, as it came; nil when the CV has none.
	Original(ctx context.Context, id string) (*Attachment, error)
	// Names reads the title of a CV of the user and the name on it.
	Names(ctx context.Context, bearer, id string) (*ResumeNames, error)
	// LetterPDF renders a cover letter as a PDF.
	LetterPDF(ctx context.Context, in LetterRequest) ([]byte, error)
}

// ResumeNames are the title the student gave a CV and the name in its contact block.
type ResumeNames struct {
	Title       string
	ContactName string
}

// LetterRequest is the cover letter resume-api renders: the body and subject of the email, in Language (fr, en,
// ar), signed with the contact block of ResumeID (none when empty) and SenderEmail.
type LetterRequest struct {
	ResumeID    string
	Language    string
	Subject     string
	Body        string
	SenderEmail string
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

// Company is a company that may hire the student, as a provider or the platform's offers know it. Domain is its
// web host, lower case without "www." (empty when unknown). Country is ISO 3166 alpha-2 or empty.
type Company struct {
	Domain      string
	Name        string
	Description string
	Industry    string
	SizeRange   string
	Country     string
	City        string
	Keywords    []string
}

// CompanyQuery is what a student looks for, as search terms (lower case, no accents), in a country (and city).
type CompanyQuery struct {
	Terms   []string
	Country string
	City    string
	Limit   int
}

// CompanyDiscoverer finds companies that match a query, chosen by name in pkg/recruiters (env COMPANY_DISCOVERY).
// Contract: Ready is false when its key is missing (then it is skipped, never an error); Discover returns at most
// q.Limit companies, each with a Domain; a failure is an error (the search goes on without it).
type CompanyDiscoverer interface {
	Name() string
	Ready() bool
	Discover(ctx context.Context, q CompanyQuery) ([]Company, error)
}

// ContactQuery asks for the people who hire at a company: by Domain, else by Company name (only finders that can
// resolve a name answer then). MaxCredits is how many paid credits the finder may still spend this month.
type ContactQuery struct {
	Domain     string
	Company    string
	MaxCredits int
}

// Contact is one person (or recruiting mailbox) at a company. Department is hr, management, executive,
// engineering or other; Verification valid, accept_all, unknown, invalid or empty (not checked).
type Contact struct {
	Email        string
	FirstName    string
	LastName     string
	Title        string
	Department   string
	LinkedInURL  string
	Confidence   int
	Verification string
}

// ContactLookup is what one finder returned: the people, the credits it spent and, when asked by name, the
// domain and name of the company it resolved.
type ContactLookup struct {
	Contacts []Contact
	Credits  int
	Domain   string
	Company  string
}

// ContactFinder is one source of company contacts, chosen by name in pkg/recruiters (env RECRUITER_CHAIN).
// Contract: Ready is false when its key is missing (skipped, never an error); Find never spends more than
// q.MaxCredits; a query it cannot answer (no domain) gives an empty lookup, not an error; Confidence is 0 to 100.
type ContactFinder interface {
	Name() string
	Ready() bool
	Find(ctx context.Context, q ContactQuery) (*ContactLookup, error)
}

// OfferCompany is a company that published still-open offers matching the search terms on the platform.
type OfferCompany struct {
	Company      string
	Domain       string
	Description  string
	City         string
	Country      string
	Titles       []string
	MatchedTerms []string
}

// OfferCompanySource calls 9itchi-job-api for the companies of the open offers matching search terms.
type OfferCompanySource interface {
	OfferCompanies(ctx context.Context, terms []string, country string, limit int) ([]OfferCompany, error)
}

// SkillSource calls 9itchi-resume-api for the skills listed on a CV of the user.
type SkillSource interface {
	Skills(ctx context.Context, bearer, resumeID string) ([]string, error)
}

// OfferText is an offer read from a link (or from its pasted text) by 9itchi-job-api. Source is "linkedin" or the
// host of the link.
type OfferText struct {
	Source      string
	Title       string
	Company     string
	Description string
	ApplyEmail  string
}

// OfferReader calls 9itchi-job-api to read one offer link. text, when given, is the offer as the student pasted
// it and nothing is fetched. An offer it cannot read wraps ErrPeerUnprocessable.
type OfferReader interface {
	ReadOffer(ctx context.Context, rawURL, text string) (*OfferText, error)
}
