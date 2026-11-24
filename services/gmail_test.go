package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"9itchi-email-api/core"
	"9itchi-email-api/models/db"
	"9itchi-email-api/repositories"
	"9itchi-email-api/utils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memGmail struct{ conn *db.GmailConnection }

func (m *memGmail) Upsert(_ context.Context, c *db.GmailConnection) error {
	cp := *c
	cp.ConnectedAt = time.Now()
	m.conn = &cp
	return nil
}
func (m *memGmail) Get(context.Context, string) (*db.GmailConnection, error) {
	if m.conn == nil {
		return nil, repositories.ErrNotFound
	}
	return m.conn, nil
}
func (m *memGmail) Delete(context.Context, string) error { m.conn = nil; return nil }

type stubMailer struct {
	core.Mailer
	configured bool
	exchange   *core.GoogleToken
	exchErr    error
	refreshed  *core.GoogleToken
	refreshErr error
	revoked    []string
}

func (s *stubMailer) Configured() bool       { return s.configured }
func (s *stubMailer) AuthURL(st string) string { return "https://google/auth?state=" + st }
func (s *stubMailer) Exchange(context.Context, string) (*core.GoogleToken, error) {
	return s.exchange, s.exchErr
}
func (s *stubMailer) Refresh(context.Context, string) (*core.GoogleToken, error) {
	return s.refreshed, s.refreshErr
}
func (s *stubMailer) UserEmail(context.Context, string) (string, error) { return "me@gmail.com", nil }
func (s *stubMailer) Revoke(_ context.Context, t string) error         { s.revoked = append(s.revoked, t); return nil }

func newGmail(m *stubMailer) (GmailService, *memGmail) {
	repo := &memGmail{}
	return NewGmailService(repo, m, testCfg), repo
}

func stateOf(t *testing.T, svc GmailService) string {
	res, apiErr := svc.ConnectGmail(context.Background(), uid)
	require.Nil(t, apiErr)
	const prefix = "https://google/auth?state="
	require.Contains(t, res.URL, prefix)
	return res.URL[len(prefix):]
}

func TestConnectNeedsConfiguration(t *testing.T) {
	svc, _ := newGmail(&stubMailer{})
	_, apiErr := svc.ConnectGmail(context.Background(), uid)
	require.NotNil(t, apiErr)
	assert.Equal(t, 503, apiErr.Code)
}

func TestCallbackStoresEncryptedToken(t *testing.T) {
	m := &stubMailer{configured: true, exchange: &core.GoogleToken{AccessToken: "acc", RefreshToken: "ref", Expiry: time.Now().Add(time.Hour)}}
	svc, repo := newGmail(m)
	state := stateOf(t, svc)

	redirect, apiErr := svc.GmailCallback(context.Background(), "code", state, "")
	require.Nil(t, apiErr)
	assert.Equal(t, "http://client/settings/gmail?gmail=connected", redirect)
	require.NotNil(t, repo.conn)
	assert.Equal(t, uid, repo.conn.UserID)
	assert.NotContains(t, repo.conn.EncryptedToken, "acc")
	assert.NotContains(t, repo.conn.EncryptedToken, "ref")

	st, apiErr := svc.GmailStatus(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.True(t, st.Connected)
	assert.Equal(t, "me@gmail.com", st.Email)

	access, addr, apiErr := svc.Credentials(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Equal(t, "acc", access)
	assert.Equal(t, "me@gmail.com", addr)
}

func TestCallbackRejectsBadState(t *testing.T) {
	svc, _ := newGmail(&stubMailer{configured: true})
	_, apiErr := svc.GmailCallback(context.Background(), "code", "garbage", "")
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Code)

	// An access token must not be usable as an OAuth state.
	tok, err := utils.IssueToken(testCfg, utils.Claims{ID: uid, Type: utils.TokenAccess}, time.Minute)
	require.NoError(t, err)
	_, apiErr = svc.GmailCallback(context.Background(), "code", tok, "")
	require.NotNil(t, apiErr)
	assert.Equal(t, 400, apiErr.Code)
}

func TestCallbackOutcomes(t *testing.T) {
	m := &stubMailer{configured: true, exchange: &core.GoogleToken{AccessToken: "acc"}}
	svc, repo := newGmail(m)
	state := stateOf(t, svc)

	redirect, _ := svc.GmailCallback(context.Background(), "", state, "access_denied")
	assert.Equal(t, "http://client/settings/gmail?gmail=denied", redirect)

	// Google returned no refresh token and none is stored: cannot send later.
	redirect, _ = svc.GmailCallback(context.Background(), "code", state, "")
	assert.Equal(t, "http://client/settings/gmail?gmail=error", redirect)
	assert.Nil(t, repo.conn)

	m.exchErr = errors.New("bad code")
	redirect, _ = svc.GmailCallback(context.Background(), "code", state, "")
	assert.Equal(t, "http://client/settings/gmail?gmail=error", redirect)
}

func TestCredentialsRefreshAndRevoke(t *testing.T) {
	m := &stubMailer{
		configured: true,
		exchange:   &core.GoogleToken{AccessToken: "old", RefreshToken: "ref", Expiry: time.Now().Add(-time.Hour)},
		refreshed:  &core.GoogleToken{AccessToken: "new", RefreshToken: "ref", Expiry: time.Now().Add(time.Hour)},
	}
	svc, _ := newGmail(m)
	_, _ = svc.GmailCallback(context.Background(), "code", stateOf(t, svc), "")

	access, _, apiErr := svc.Credentials(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.Equal(t, "new", access)

	m.exchange.Expiry = time.Now().Add(-time.Hour)
	m.refreshed = nil
	m.refreshErr = core.ErrGmailAuth
	_, _ = svc.GmailCallback(context.Background(), "code", stateOf(t, svc), "")
	_, _, apiErr = svc.Credentials(context.Background(), uid)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)

	st, apiErr := svc.RevokeGmail(context.Background(), uid)
	require.Nil(t, apiErr)
	assert.False(t, st.Connected)
	assert.Equal(t, []string{"ref"}, m.revoked)
	_, _, apiErr = svc.Credentials(context.Background(), uid)
	require.NotNil(t, apiErr)
	assert.Equal(t, 409, apiErr.Code)
}
