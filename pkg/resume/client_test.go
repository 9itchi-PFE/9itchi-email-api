package resume

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummaryTextKeepsFiguresAndCredentials(t *testing.T) {
	var v resumeView
	require.NoError(t, json.Unmarshal([]byte(`{"content":{
		"contact":{"name":"Ibrahim Benlakhal","headline":"Élève ingénieur"},
		"education":[{"school":"ENIS","degree":"Génie informatique","start":"2022","end":"2028"},{"school":"Old","hidden":true}],
		"experience":[{"company":"Telnet","title":"Stagiaire","kind":"stage d'été","start":"2025-07","end":"2025-08",
			"description":["Réduit la latence de 2 s à moins de 100 ms", " "]}],
		"projects":[{"name":"9itchi","tech":"Go, Next.js","description":["Conçu 11 microservices"]}],
		"certifications":[{"name":"CTI / EUR-ACE","issuer":"ENIS"}],
		"languages":["Anglais: B2"],
		"extra":[{"title":"Situation","items":["Carte de séjour Talent"]},{"title":"Vide","items":[]}],
		"skills":["C++","Go"]}}`), &v))

	got := summaryText(&v)
	for _, want := range []string{
		"Name: Ibrahim Benlakhal\n",
		"Education: Génie informatique, ENIS (2022 - 2028)\n",
		"Experience: Stagiaire (stage d'été) at Telnet (2025-07 - 2025-08)\n  - Réduit la latence de 2 s à moins de 100 ms\n",
		"Project: 9itchi (Go, Next.js)\n  - Conçu 11 microservices\n",
		"Certification: CTI / EUR-ACE, ENIS\n",
		"Languages: Anglais: B2\n",
		"Situation: Carte de séjour Talent\n",
		"Skills: C++, Go\n",
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, "Old", "hidden entries stay out")
	assert.NotContains(t, got, "Vide")
	assert.NotContains(t, got, "  - \n", "blank bullets dropped")
}

func TestSummaryTextCutsOnALineEnd(t *testing.T) {
	var v resumeView
	v.Content.Skills = []string{strings.Repeat("x", 100)}
	for i := 0; i < 200; i++ {
		v.Content.Languages = append(v.Content.Languages, "Français")
	}
	v.Content.Summary = strings.Repeat("a", maxSummaryBytes)
	got := summaryText(&v)
	assert.LessOrEqual(t, len(got), maxSummaryBytes)
	assert.True(t, strings.HasSuffix(got, "\n"))
}
