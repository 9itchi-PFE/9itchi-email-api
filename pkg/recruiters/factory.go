// Package recruiters holds the providers of recruiter contacts behind core.RecruiterFinder. A new provider is added
// by registering its constructor in Providers; callers only pick it by name (env RECRUITER_PROVIDER).
package recruiters

import (
	"fmt"

	"9itchi-email-api/core"
)

// Config is what a provider needs to reach its API. The fake provider needs nothing.
type Config struct {
	APIKey  string
	BaseURL string
}

// Providers maps a provider name to its constructor.
var Providers = map[string]func(Config) (core.RecruiterFinder, error){
	"fake": NewFake,
}

// New builds the provider registered under name.
func New(name string, cfg Config) (core.RecruiterFinder, error) {
	build, ok := Providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown recruiter provider %q", name)
	}
	return build(cfg)
}
