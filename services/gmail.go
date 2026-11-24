package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models"
	"9itchi-email-api/models/api"
	"9itchi-email-api/models/db"
	"9itchi-email-api/pkg/google"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"
)

const (
	tokenGmailState = "gmail_state"
	stateTTL        = 10 * time.Minute
	refreshMargin   = time.Minute
)

// GmailService holds all business logic for gmail. Handlers stay thin.
type GmailService interface {
	// GET /gmail/connect: start Gmail OAuth2 (send-only scope)
	ConnectGmail(ctx context.Context, userID string) (*api.GmailConnectResponse, *models.ApiError)
	// GET /gmail/callback: finish OAuth2, store AES-GCM encrypted token. Returns the client URL to redirect to.
	GmailCallback(ctx context.Context, code, state, oauthErr string) (string, *models.ApiError)
	// GET /gmail/status: connection state
	GmailStatus(ctx context.Context, userID string) (*api.GmailStatusResponse, *models.ApiError)
	// DELETE /gmail/connection: revoke access and delete token
	RevokeGmail(ctx context.Context, userID string) (*api.GmailStatusResponse, *models.ApiError)
	// Credentials returns a valid access token and the address of the connected account.
	Credentials(ctx context.Context, userID string) (accessToken, address string, apiErr *models.ApiError)
}

type gmailService struct {
	repo   repositories.GmailRepository
	mailer core.Mailer
	cfg    utils.AppConfig
}

func NewGmailService(repo repositories.GmailRepository, mailer core.Mailer, cfg utils.AppConfig) GmailService {
	return &gmailService{repo: repo, mailer: mailer, cfg: cfg}
}

func apiError(code int, msg string) *models.ApiError {
	return &models.ApiError{Code: code, Error: errors.New(msg)}
}

func (s *gmailService) ConnectGmail(_ context.Context, userID string) (*api.GmailConnectResponse, *models.ApiError) {
	if !s.mailer.Configured() {
		return nil, apiError(http.StatusServiceUnavailable, "gmail is not configured")
	}
	state, err := utils.IssueToken(s.cfg, utils.Claims{ID: userID, Type: tokenGmailState}, stateTTL)
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "could not start gmail connection")
	}
	return &api.GmailConnectResponse{URL: s.mailer.AuthURL(state)}, nil
}

// clientRedirect is the settings screen with the outcome in the query string.
func (s *gmailService) clientRedirect(result string) string {
	return s.cfg.ClientURL + "/settings/gmail?gmail=" + url.QueryEscape(result)
}

func (s *gmailService) GmailCallback(ctx context.Context, code, state, oauthErr string) (string, *models.ApiError) {
	claims, err := utils.ParseToken(s.cfg, state)
	if err != nil || claims.Type != tokenGmailState || claims.ID == "" {
		return "", apiError(http.StatusBadRequest, "invalid state")
	}
	if oauthErr != "" || code == "" {
		return s.clientRedirect("denied"), nil
	}
	tok, err := s.mailer.Exchange(ctx, code)
	if err != nil {
		utils.Logger.Logger.Warnw("gmail: code exchange failed", "error", err)
		return s.clientRedirect("error"), nil
	}
	if tok.RefreshToken == "" {
		if old, getErr := s.load(ctx, claims.ID); getErr == nil {
			tok.RefreshToken = old.RefreshToken
		}
	}
	if tok.RefreshToken == "" {
		return s.clientRedirect("error"), nil
	}
	address, err := s.mailer.UserEmail(ctx, tok.AccessToken)
	if err != nil {
		utils.Logger.Logger.Warnw("gmail: userinfo failed", "error", err)
		return s.clientRedirect("error"), nil
	}
	if err := s.save(ctx, claims.ID, address, tok); err != nil {
		utils.Logger.Logger.Warnw("gmail: save failed", "error", err)
		return s.clientRedirect("error"), nil
	}
	return s.clientRedirect("connected"), nil
}

func (s *gmailService) save(ctx context.Context, userID, address string, tok *core.GoogleToken) error {
	raw, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	enc, err := utils.EncryptSecret(s.cfg, raw)
	if err != nil {
		return err
	}
	return s.repo.Upsert(ctx, &db.GmailConnection{UserID: userID, Email: address, EncryptedToken: enc, Scopes: google.Scopes})
}

func (s *gmailService) load(ctx context.Context, userID string) (*core.GoogleToken, error) {
	conn, err := s.repo.Get(ctx, userID)
	if err != nil {
		return nil, err
	}
	raw, err := utils.DecryptSecret(s.cfg, conn.EncryptedToken)
	if err != nil {
		return nil, err
	}
	var tok core.GoogleToken
	if err := json.Unmarshal(raw, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

func (s *gmailService) GmailStatus(ctx context.Context, userID string) (*api.GmailStatusResponse, *models.ApiError) {
	conn, err := s.repo.Get(ctx, userID)
	if errors.Is(err, repositories.ErrNotFound) {
		return &api.GmailStatusResponse{Connected: false}, nil
	}
	if err != nil {
		return nil, apiError(http.StatusInternalServerError, "could not read gmail connection")
	}
	since := conn.ConnectedAt.UTC().Format(time.RFC3339)
	return &api.GmailStatusResponse{Connected: true, Email: conn.Email, Since: &since}, nil
}

func (s *gmailService) RevokeGmail(ctx context.Context, userID string) (*api.GmailStatusResponse, *models.ApiError) {
	if tok, err := s.load(ctx, userID); err == nil {
		// Best effort: the local token is deleted even when Google cannot be reached.
		if revErr := s.mailer.Revoke(ctx, tok.RefreshToken); revErr != nil {
			utils.Logger.Logger.Warnw("gmail: revoke failed", "error", revErr)
		}
	}
	if err := s.repo.Delete(ctx, userID); err != nil {
		return nil, apiError(http.StatusInternalServerError, "could not delete gmail connection")
	}
	return &api.GmailStatusResponse{Connected: false}, nil
}

func (s *gmailService) Credentials(ctx context.Context, userID string) (string, string, *models.ApiError) {
	conn, err := s.repo.Get(ctx, userID)
	if errors.Is(err, repositories.ErrNotFound) {
		return "", "", apiError(http.StatusConflict, "gmail is not connected")
	}
	if err != nil {
		return "", "", apiError(http.StatusInternalServerError, "could not read gmail connection")
	}
	tok, err := s.load(ctx, userID)
	if err != nil {
		return "", "", apiError(http.StatusConflict, "gmail connection is unreadable, reconnect gmail")
	}
	if time.Until(tok.Expiry) > refreshMargin {
		return tok.AccessToken, conn.Email, nil
	}
	fresh, err := s.mailer.Refresh(ctx, tok.RefreshToken)
	if errors.Is(err, core.ErrGmailAuth) {
		return "", "", apiError(http.StatusConflict, "gmail access was revoked, reconnect gmail")
	}
	if err != nil {
		return "", "", apiError(http.StatusBadGateway, "gmail is unavailable")
	}
	if err := s.save(ctx, userID, conn.Email, fresh); err != nil {
		utils.Logger.Logger.Warnw("gmail: could not store refreshed token", "error", err)
	}
	return fresh.AccessToken, conn.Email, nil
}
