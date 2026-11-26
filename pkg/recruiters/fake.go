package recruiters

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"9itchi-email-api/core"
)

// Fake invents plausible recruiters, the same ones for the same query, without calling anyone. Its domains all end
// in .example (reserved, never a real company), so a letter written to one of its addresses can never reach anybody.
type Fake struct{}

// NewFake builds the fake provider (it needs no key).
func NewFake(Config) (core.RecruiterFinder, error) { return Fake{}, nil }

func (Fake) Name() string { return "fake" }

var (
	fakeFirstNames = []string{"Amine", "Ines", "Mehdi", "Sarra", "Youssef", "Rania", "Karim", "Mariem", "Skander", "Nour",
		"Hamza", "Emna", "Aymen", "Salma", "Wassim", "Asma", "Oussema", "Chaima", "Bilel", "Yasmine"}
	fakeLastNames = []string{"Ben Ali", "Trabelsi", "Gharbi", "Jaziri", "Mejri", "Chaabane", "Ayari", "Hammami", "Bouazizi",
		"Ben Salah", "Khelifi", "Sassi", "Dridi", "Mansouri", "Zouari", "Ferchichi", "Jlassi", "Masmoudi", "Kammoun", "Baccouche"}
	fakeTitles = []string{"Responsable RH", "Chargée de recrutement", "Talent Acquisition Specialist", "Talent Acquisition Manager",
		"HR Business Partner", "Directrice des ressources humaines", "Campus Recruiter", "Hiring Manager",
		"Responsable recrutement", "HR Manager"}
	fakeSuffixes  = []string{"Labs", "Solutions", "Digital", "Systems", "Consulting", "Technologies"}
	fakeCompanies = []string{"Carthage Labs", "Medina Digital", "Jasmin Systems", "Sahel Solutions", "Atlas Consulting"}
)

func (Fake) Find(_ context.Context, q core.RecruiterQuery) ([]core.Recruiter, error) {
	norm, sum := fakeSeed(q)
	rng := rand.New(rand.NewPCG(binary.BigEndian.Uint64(sum[:8]), binary.BigEndian.Uint64(sum[8:16])))
	companies := fakeCompaniesFor(q.Company, norm, rng)
	n := 8 + rng.IntN(5)
	if q.Limit > 0 && n > q.Limit {
		n = q.Limit
	}
	out := make([]core.Recruiter, 0, n)
	used := map[string]bool{}
	for len(out) < n {
		first, last := fakeFirstNames[rng.IntN(len(fakeFirstNames))], fakeLastNames[rng.IntN(len(fakeLastNames))]
		if used[first+last] {
			continue
		}
		used[first+last] = true
		company := companies[rng.IntN(len(companies))]
		domain := slug(company) + ".example"
		r := core.Recruiter{
			Name:        first + " " + last,
			Title:       fakeTitles[rng.IntN(len(fakeTitles))],
			Company:     company,
			Domain:      domain,
			LinkedInURL: fmt.Sprintf("https://www.linkedin.com/in/%s-%s-%06x", slug(first), slug(last), rng.Uint32()&0xffffff),
		}
		if rng.IntN(3) == 0 {
			r.Confidence = 15 + rng.IntN(40)
		} else {
			r.Email = strings.ReplaceAll(slug(first)+"."+slug(last), "-", "") + "@" + domain
			r.Confidence = 60 + rng.IntN(39)
		}
		out = append(out, r)
	}
	return out, nil
}

// fakeSeed is the query normalized and its digest: the same search, however typed, gives the same people.
func fakeSeed(q core.RecruiterQuery) (core.RecruiterQuery, [32]byte) {
	norm := func(in []string) []string {
		out := make([]string, 0, len(in))
		for _, s := range in {
			if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
				out = append(out, s)
			}
		}
		slices.Sort(out)
		return slices.Compact(out)
	}
	n := core.RecruiterQuery{
		Skills: norm(q.Skills), Keywords: norm(q.Keywords), Company: strings.ToLower(strings.TrimSpace(q.Company)),
		Domain: strings.ToLower(strings.TrimSpace(q.Domain)), Location: strings.ToLower(strings.TrimSpace(q.Location)), Limit: q.Limit,
	}
	b, _ := json.Marshal(n)
	return n, sha256.Sum256(b)
}

// fakeCompaniesFor is the company asked for (as typed), else names made from the normalized domain or first keyword or skill.
func fakeCompaniesFor(company string, q core.RecruiterQuery, rng *rand.Rand) []string {
	if c := strings.TrimSpace(company); c != "" {
		return []string{c}
	}
	if d := strings.TrimPrefix(q.Domain, "www."); d != "" {
		label, _, _ := strings.Cut(d, ".")
		return []string{titleCase(label)}
	}
	word := ""
	for _, s := range append(slices.Clone(q.Keywords), q.Skills...) {
		if s = strings.TrimSpace(s); s != "" {
			word = s
			break
		}
	}
	if word == "" {
		return fakeCompanies
	}
	out := make([]string, 0, 3)
	for _, i := range rng.Perm(len(fakeSuffixes))[:3] {
		out = append(out, titleCase(word)+" "+fakeSuffixes[i])
	}
	return out
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	r, size := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[size:]
}

// slug keeps ASCII letters and digits, lower case, joined by dashes.
func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return "company"
	}
	return out
}
