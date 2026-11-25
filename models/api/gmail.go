package api

import "time"

// GmailConnectResponse carries the Google consent URL the browser must open.
type GmailConnectResponse struct {
	URL string `json:"url"`
}

// UserBuiltSendsResponse is what the team sees of the applications a student sent with the CV built in 9itchi: how
// many, and when the last one left. It tells whether a payment was used (the refund rule); no recipient, no subject.
type UserBuiltSendsResponse struct {
	Count  int        `json:"count"`
	LastAt *time.Time `json:"last_at"`
}

// GmailStatusResponse is the Gmail connection state of the user.
type GmailStatusResponse struct {
	Connected bool    `json:"connected"`
	Email     string  `json:"email,omitempty"`
	Since     *string `json:"connected_at,omitempty"`
}
