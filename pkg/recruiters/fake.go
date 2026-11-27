package recruiters

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"9itchi-email-api/core"
)

// Fake invents plausible companies and recruiters, the same ones for the same query, without calling anyone. Its
// domains and addresses all end in .example (reserved, never a real company), so a letter written to one of its
// addresses can never reach anybody. For tests, e2e and development only.
type Fake struct{}

var (
	_ core.ContactFinder     = Fake{}
	_ core.CompanyDiscoverer = Fake{}
)

func (Fake) Name() string { return "fake" }

// Ready: the fake needs no key.
func (Fake) Ready() bool { return true }

var (
	fakeFirstNames = []string{"Amine", "Ines", "Mehdi", "Sarra", "Youssef", "Rania", "Karim", "Mariem", "Skander", "Nour",
		"Hamza", "Emna", "Aymen", "Salma", "Wassim", "Asma", "Oussema", "Chaima", "Bilel", "Yasmine"}
	fakeLastNames = []string{"Ben Ali", "Trabelsi", "Gharbi", "Jaziri", "Mejri", "Chaabane", "Ayari", "Hammami", "Bouazizi",
		"Ben Salah", "Khelifi", "Sassi", "Dridi", "Mansouri", "Zouari", "Ferchichi", "Jlassi", "Masmoudi", "Kammoun", "Baccouche"}
	fakeTitles = []string{"Responsable RH", "Chargée de recrutement", "Talent Acquisition Specialist", "Talent Acquisition Manager",
		"HR Business Partner", "Directrice des ressources humaines", "Campus Recruiter", "CTO",
		"Responsable recrutement", "Head of Engineering"}
	fakeSuffixes = []string{"Labs", "Solutions", "Digital", "Systems", "Consulting", "Technologies"}
	fakeSizes    = []string{"1-10", "11-50", "51-200", "201-500"}
)

func fakeRand(parts ...string) *rand.Rand {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.Join(parts, "|"))))
	return rand.New(rand.NewPCG(binary.BigEndian.Uint64(sum[:8]), binary.BigEndian.Uint64(sum[8:16])))
}

// Find invents two or three people at the company. A real domain is never used: the people get the .example
// domain made from its first label (or from the company name when there is no domain).
func (Fake) Find(_ context.Context, q core.ContactQuery) (*core.ContactLookup, error) {
	domain := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(q.Domain)), "www.")
	label, _, _ := strings.Cut(domain, ".")
	if label == "" {
		if strings.TrimSpace(q.Company) == "" {
			return &core.ContactLookup{}, nil
		}
		label = slug(q.Company)
	}
	domain = slug(label) + ".example"
	rng := fakeRand(domain)
	n := 2 + rng.IntN(2)
	out := &core.ContactLookup{Domain: domain, Company: titleCase(label), Contacts: make([]core.Contact, 0, n)}
	used := map[string]bool{}
	for len(out.Contacts) < n {
		first, last := fakeFirstNames[rng.IntN(len(fakeFirstNames))], fakeLastNames[rng.IntN(len(fakeLastNames))]
		if used[first+last] {
			continue
		}
		used[first+last] = true
		c := core.Contact{
			FirstName: first, LastName: last, Title: fakeTitles[rng.IntN(len(fakeTitles))], Department: "hr",
			Email:       strings.ReplaceAll(slug(first)+"."+slug(last), "-", "") + "@" + domain,
			LinkedInURL: fmt.Sprintf("https://www.linkedin.com/in/%s-%s-%06x", slug(first), slug(last), rng.Uint32()&0xffffff),
			Confidence:  60 + rng.IntN(39),
		}
		if rng.IntN(2) == 0 {
			c.Verification = "valid"
		}
		out.Contacts = append(out.Contacts, c)
	}
	return out, nil
}

// Discover invents three to six companies named after the first terms, in the country and city asked for.
func (Fake) Discover(_ context.Context, q core.CompanyQuery) ([]core.Company, error) {
	terms := slices.Clone(q.Terms)
	slices.Sort(terms)
	terms = slices.Compact(terms)
	rng := fakeRand(strings.Join(terms, ","), q.Country, q.City)
	word := "carthage"
	if len(q.Terms) > 0 && strings.TrimSpace(q.Terms[0]) != "" {
		word = q.Terms[0]
	}
	n := 3 + rng.IntN(4)
	if q.Limit > 0 && n > q.Limit {
		n = q.Limit
	}
	out := make([]core.Company, 0, n)
	for _, i := range rng.Perm(len(fakeSuffixes))[:min(n, len(fakeSuffixes))] {
		name := titleCase(word) + " " + fakeSuffixes[i]
		kw := terms[:min(3, len(terms))]
		out = append(out, core.Company{
			Domain: slug(name) + ".example", Name: name, Industry: "Information Technology",
			Description: name + " builds software products (" + strings.Join(kw, ", ") + ").",
			SizeRange:   fakeSizes[rng.IntN(len(fakeSizes))], Country: q.Country, City: q.City, Keywords: slices.Clone(kw),
		})
	}
	return out, nil
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
