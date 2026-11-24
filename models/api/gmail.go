package api

// GmailConnectResponse carries the Google consent URL the browser must open.
type GmailConnectResponse struct {
	URL string `json:"url"`
}

// GmailStatusResponse is the Gmail connection state of the user.
type GmailStatusResponse struct {
	Connected bool    `json:"connected"`
	Email     string  `json:"email,omitempty"`
	Since     *string `json:"connected_at,omitempty"`
}
