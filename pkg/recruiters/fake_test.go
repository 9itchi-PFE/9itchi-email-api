package recruiters

import (
	"context"
	"strings"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPicksTheProviderByName(t *testing.T) {
	f, err := New("fake", Config{})
	require.NoError(t, err)
	assert.Equal(t, "fake", f.Name())

	_, err = New("nobody", Config{APIKey: "k"})
	assert.Error(t, err)
}

func TestFakeHonorsTheFinderContract(t *testing.T) {
	f, _ := NewFake(Config{})
	q := core.RecruiterQuery{Keywords: []string{"React", "Node"}, Location: "Tunis", Limit: 20}
	got, err := f.Find(context.Background(), q)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got), 8)
	require.LessOrEqual(t, len(got), 12)

	withoutEmail := 0
	names := map[string]bool{}
	for _, r := range got {
		assert.NotEmpty(t, r.Name)
		assert.False(t, names[r.Name], "no one twice")
		names[r.Name] = true
		assert.NotEmpty(t, r.Title)
		assert.True(t, strings.HasSuffix(r.Domain, ".example"), "never a real domain: %s", r.Domain)
		assert.True(t, strings.HasPrefix(r.LinkedInURL, "https://www.linkedin.com/in/"))
		assert.GreaterOrEqual(t, r.Confidence, 0)
		assert.LessOrEqual(t, r.Confidence, 100)
		if r.Email == "" {
			withoutEmail++
			continue
		}
		assert.True(t, strings.HasSuffix(r.Email, "@"+r.Domain), r.Email)
	}
	assert.Less(t, withoutEmail, len(got), "most have an address")

	again, _ := f.Find(context.Background(), core.RecruiterQuery{Keywords: []string{" node", "react "}, Location: "TUNIS", Limit: 20})
	assert.Equal(t, got, again, "the same search, however typed, gives the same people")

	other, _ := f.Find(context.Background(), core.RecruiterQuery{Keywords: []string{"Java"}, Limit: 20})
	assert.NotEqual(t, got, other)
}

func TestFakeCompanyAndLimit(t *testing.T) {
	f, _ := NewFake(Config{})
	got, _ := f.Find(context.Background(), core.RecruiterQuery{Company: "Vermeg", Domain: "vermeg.com", Limit: 3})
	require.Len(t, got, 3)
	for _, r := range got {
		assert.Equal(t, "Vermeg", r.Company)
		assert.Equal(t, "vermeg.example", r.Domain, "the company's real domain is never used")
	}

	got, _ = f.Find(context.Background(), core.RecruiterQuery{Domain: "www.acme.tn", Limit: 20})
	require.NotEmpty(t, got)
	assert.Equal(t, "Acme", got[0].Company)
	assert.Equal(t, "acme.example", got[0].Domain)

	got, _ = f.Find(context.Background(), core.RecruiterQuery{Limit: 20})
	assert.NotEmpty(t, got, "a search with nothing to go on still answers")
}
