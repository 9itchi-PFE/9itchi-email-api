// Package recruiters holds the sources of the prospect pool: contact finders behind core.ContactFinder and company
// discoverers behind core.CompanyDiscoverer. A new source is added by registering its constructor in Finders or
// Discoverers; callers only pick it by name (env RECRUITER_CHAIN, COMPANY_DISCOVERY).
package recruiters

import (
	"fmt"
	"net/http"
	"strings"

	"9itchi-email-api/core"
)

// Config is what the sources need. Keys come from env (comma separated: several accounts); Token, when set and non-empty for a provider, wins over the
// env key (the token an admin set in statistics-api). Base URLs and HTTP are for tests; empty means the real API.
type Config struct {
	HunterAPIKey string
	ApolloAPIKey string
	Token        func(provider string) string
	HunterURL    string
	ApolloURL    string
	HTTP         *http.Client
}

// Finders maps a contact finder name to its constructor.
var Finders = map[string]func(Config) core.ContactFinder{
	"website": func(Config) core.ContactFinder { return NewWebsite() },
	"hunter":  func(c Config) core.ContactFinder { return NewHunter(c) },
	"apollo":  func(c Config) core.ContactFinder { return NewApollo(c) },
	"fake":    func(Config) core.ContactFinder { return Fake{} },
}

// Discoverers maps a company discoverer name to its constructor.
var Discoverers = map[string]func(Config) core.CompanyDiscoverer{
	"hunter": func(c Config) core.CompanyDiscoverer { return NewHunter(c) },
	"fake":   func(Config) core.CompanyDiscoverer { return Fake{} },
}

// NewChain builds the contact finders in the order named. An unknown name is an error (startup fails).
func NewChain(names []string, cfg Config) ([]core.ContactFinder, error) {
	out := make([]core.ContactFinder, 0, len(names))
	for _, name := range names {
		build, ok := Finders[strings.TrimSpace(name)]
		if !ok {
			return nil, fmt.Errorf("unknown contact finder %q in RECRUITER_CHAIN", name)
		}
		out = append(out, build(cfg))
	}
	return out, nil
}

// NewDiscovery builds the company discoverers in the order named. An unknown name is an error (startup fails).
func NewDiscovery(names []string, cfg Config) ([]core.CompanyDiscoverer, error) {
	out := make([]core.CompanyDiscoverer, 0, len(names))
	for _, name := range names {
		build, ok := Discoverers[strings.TrimSpace(name)]
		if !ok {
			return nil, fmt.Errorf("unknown company discoverer %q in COMPANY_DISCOVERY", name)
		}
		out = append(out, build(cfg))
	}
	return out, nil
}

func (c Config) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return defaultHTTP
}
