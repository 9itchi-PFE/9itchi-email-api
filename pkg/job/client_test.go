package job

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/internal/jobs/j1":
			_, _ = w.Write([]byte(`{"id":"j1","title":"Stage","company":"Acme","description":"d","apply_email":"rh@acme.tn","closes_on":"2027-01-10","open":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	info, err := New(srv.URL, "key").ApplyInfo(context.Background(), "j1")
	require.NoError(t, err)
	assert.Equal(t, &core.JobInfo{Title: "Stage", Company: "Acme", Description: "d", ApplyEmail: "rh@acme.tn", Open: true}, info)

	_, err = New(srv.URL, "key").ApplyInfo(context.Background(), "gone")
	assert.ErrorIs(t, err, core.ErrPeerNotFound)

	_, err = New(srv.URL, "wrong").ApplyInfo(context.Background(), "j1")
	assert.ErrorIs(t, err, core.ErrPeerUnavailable)
}
