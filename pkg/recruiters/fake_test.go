package recruiters

import (
	"context"
	"strings"
	"testing"

	"9itchi-email-api/core"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChainPicksTheSourcesByName(t *testing.T) {
	chain, err := NewChain([]string{"website", "hunter", "apollo", "fake"}, Config{})
	require.NoError(t, err)
	require.Len(t, chain, 4)
	assert.Equal(t, "website", chain[0].Name())
	assert.True(t, chain[0].Ready(), "the website needs no key")
	assert.False(t, chain[1].Ready(), "hunter without its key is skipped")
	assert.False(t, chain[2].Ready(), "apollo without its key is skipped")

	_, err = NewChain([]string{"nobody"}, Config{})
	assert.Error(t, err)

	disc, err := NewDiscovery([]string{"hunter", "fake"}, Config{})
	require.NoError(t, err)
	require.Len(t, disc, 2)
	_, err = NewDiscovery([]string{"apollo"}, Config{})
	assert.Error(t, err, "apollo company search is not a discoverer")
}

func TestTokenWinsOverTheEnvKey(t *testing.T) {
	h := NewHunter(Config{HunterAPIKey: "env", Token: func(p string) string {
		if p == "hunter" {
			return "admin"
		}
		return ""
	}})
	assert.Equal(t, "admin", h.key())
	a := NewApollo(Config{ApolloAPIKey: "env", Token: func(string) string { return "" }})
	assert.Equal(t, "env", a.key(), "no admin token: the env key")
	assert.True(t, NewHunter(Config{Token: func(string) string { return "admin" }}).Ready())
}

func TestFakeHonorsTheContracts(t *testing.T) {
	f := Fake{}
	got, err := f.Find(context.Background(), core.ContactQuery{Domain: "www.Vermeg.com"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(got.Contacts), 2)
	assert.Equal(t, "vermeg.example", got.Domain, "the company's real domain is never used")
	for _, c := range got.Contacts {
		assert.True(t, strings.HasSuffix(c.Email, "@vermeg.example"), c.Email)
		assert.NotEmpty(t, c.FirstName)
		assert.GreaterOrEqual(t, c.Confidence, 0)
		assert.LessOrEqual(t, c.Confidence, 100)
	}
	again, _ := f.Find(context.Background(), core.ContactQuery{Domain: "vermeg.com"})
	assert.Equal(t, got, again, "the same company gives the same people")

	byName, _ := f.Find(context.Background(), core.ContactQuery{Company: "Carthage Labs"})
	assert.Equal(t, "carthage-labs.example", byName.Domain)
	none, _ := f.Find(context.Background(), core.ContactQuery{})
	assert.Empty(t, none.Contacts)

	companies, err := f.Discover(context.Background(), core.CompanyQuery{Terms: []string{"data", "python"}, Country: "TN", Limit: 4})
	require.NoError(t, err)
	require.NotEmpty(t, companies)
	assert.LessOrEqual(t, len(companies), 4)
	for _, c := range companies {
		assert.True(t, strings.HasSuffix(c.Domain, ".example"), c.Domain)
		assert.Equal(t, "TN", c.Country)
		assert.Contains(t, c.Description, "data")
	}
	same, _ := f.Discover(context.Background(), core.CompanyQuery{Terms: []string{"data", "python"}, Country: "TN", Limit: 4})
	assert.Equal(t, companies, same)
}
