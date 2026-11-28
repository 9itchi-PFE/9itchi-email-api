package recruiters

import (
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// keyRest is how long a key the provider refused (401, 402, 403, 429) is left aside.
const keyRest = time.Hour

// keyRing spreads a provider's calls over its keys: each call starts at the next key (round robin) and a refused
// key rests for keyRest while the others are tried. Shared by every source of the same provider.
type keyRing struct {
	mu      sync.Mutex
	next    int
	resting map[string]time.Time
}

var (
	ringsMu sync.Mutex
	rings   = map[string]*keyRing{}
)

func ringOf(provider string) *keyRing {
	ringsMu.Lock()
	defer ringsMu.Unlock()
	r, ok := rings[provider]
	if !ok {
		r = &keyRing{resting: map[string]time.Time{}}
		rings[provider] = r
	}
	return r
}

// splitKeys reads a comma separated list of keys (spaces and empty entries out, duplicates once).
func splitKeys(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" && !seen[k] {
			seen[k] = true
			out = append(out, k)
		}
	}
	return out
}

// order is the keys to try for one call: from the next key round, resting keys last.
func (r *keyRing) order(keys []string, now time.Time) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.next % len(keys)
	r.next++
	var ready, rest []string
	for i := range keys {
		k := keys[(start+i)%len(keys)]
		if until, ok := r.resting[k]; ok && now.Before(until) {
			rest = append(rest, k)
		} else {
			ready = append(ready, k)
		}
	}
	return append(ready, rest...)
}

func (r *keyRing) rest(key string, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resting[key] = until
}

// keys are the provider's keys: the token an admin set (a comma separated list too), else the env keys.
func (c Config) keys(provider, env string) []string {
	if c.Token != nil {
		if k := splitKeys(c.Token(provider)); len(k) > 0 {
			return k
		}
	}
	return splitKeys(env)
}

// refused tells a key the provider will not serve now (wrong key, plan, credits or rate).
func refused(status int) bool {
	return status == http.StatusUnauthorized || status == http.StatusPaymentRequired ||
		status == http.StatusForbidden || status == http.StatusTooManyRequests
}

// send does one provider call with its keys: build makes the request for a key; a refused answer rests that key
// and the next is tried. The last answer (or error) is returned; with no key, build gets "".
func (c Config) send(provider string, keys []string, build func(key string) (*http.Request, error)) (*http.Response, error) {
	if len(keys) == 0 {
		keys = []string{""}
	}
	ring := ringOf(provider)
	order := ring.order(keys, time.Now())
	for i, k := range order {
		req, err := build(k)
		if err != nil {
			return nil, err
		}
		resp, err := c.client().Do(req)
		if err != nil || !refused(resp.StatusCode) || i == len(order)-1 {
			return resp, err
		}
		ring.rest(k, time.Now().Add(keyRest))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
	}
	return nil, nil // unreachable: order is never empty
}

// KeyCount is how many keys a provider has in the env value (each key brings its own monthly cap).
func KeyCount(env string) int { return len(splitKeys(env)) }
