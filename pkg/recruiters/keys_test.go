package recruiters

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitKeys(t *testing.T) {
	assert.Equal(t, []string{"a", "b"}, splitKeys(" a, ,b,a "))
	assert.Empty(t, splitKeys(""))
	assert.Equal(t, 2, KeyCount("a,b"))
}

func TestKeyRingTakesTurnsAndRestsRefusedKeys(t *testing.T) {
	r := &keyRing{resting: map[string]time.Time{}}
	now := time.Now()
	keys := []string{"a", "b", "c"}
	assert.Equal(t, []string{"a", "b", "c"}, r.order(keys, now))
	assert.Equal(t, []string{"b", "c", "a"}, r.order(keys, now))
	r.rest("c", now.Add(time.Hour))
	assert.Equal(t, []string{"a", "b", "c"}, r.order(keys, now), "a resting key comes last")
	assert.Equal(t, []string{"a", "b", "c"}, r.order(keys, now.Add(2*time.Hour)), "rested: back in turn")
}

func TestSendFallsBackToTheNextKey(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := r.Header.Get("x-api-key")
		seen = append(seen, k)
		if k == "spent-key" {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := Config{}
	keys := []string{"spent-key", "good-key"}
	for range 2 {
		resp, err := c.send("test-provider", keys, func(key string) (*http.Request, error) {
			req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
			if err == nil {
				req.Header.Set("x-api-key", key)
			}
			return req, err
		})
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	}
	assert.Equal(t, []string{"spent-key", "good-key", "good-key"}, seen, "the refused key rests, the next call skips it")
}
