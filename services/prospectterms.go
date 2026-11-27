package services

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// maxSearchTerms caps the terms one search sends to the pool and to job-api.
const maxSearchTerms = 30

// goalStopWords are the words of a goal that say nothing about a company: French and English stop words, the
// contract, durations and months.
var goalStopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`
		les des une un le la de du et ou en au aux pour par sur dans avec sans chez que qui quoi dont mon ma mes ton ta tes
		son sa ses notre nos votre vos leur leurs ce cet cette ces est sont suis être etre avoir fait faire plus moins tres
		très bien comme mais donc car pas ne je tu il elle nous vous ils elles on partir depuis entre vers apres après avant
		cherche recherche recherchons souhaite veux voudrais aimerais trouver poste offre offres entreprise entreprises
		domaine secteur ville region région pays tunisie tunis
		the and for with without from into onto about this that these those are was were been being have has had will
		would could should can may might must not but any all some your our their his her its who what which where when
		how looking look want find job jobs position company companies role field area
		pfe stage stages stagiaire alternance alternant cdi cdd internship intern apprenticeship fin etudes études
		jour jours semaine semaines mois mois an ans annee année annees années day days week weeks month months year years
		janvier fevrier février mars avril mai juin juillet aout août septembre octobre novembre decembre décembre
		january february march april may june july august september october november december`) {
		goalStopWords[fold(w)] = true
	}
}

// fold lower-cases and removes accents: "Développeur" becomes "developpeur".
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// words are the runs of letters and digits of a folded text.
func words(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// wordText is the text folded, as its words separated by single spaces.
func wordText(s string) string { return strings.Join(words(fold(s)), " ") }

// goalWords are the significant words of a goal: folded, at least 3 letters, not a stop word, not a number.
func goalWords(goal string) []string {
	var out []string
	for _, w := range words(fold(goal)) {
		if len([]rune(w)) < 3 || goalStopWords[w] || strings.IndexFunc(w, unicode.IsLetter) < 0 {
			continue
		}
		out = append(out, w)
	}
	return out
}

// searchTerms are the keywords, then the CV skills, then the significant words of the goal, folded, each once,
// at most maxSearchTerms.
func searchTerms(q prospectQuery) []string {
	var out []string
	seen := map[string]bool{}
	add := func(t string) {
		if t = wordText(t); t != "" && !seen[t] && len(out) < maxSearchTerms {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range q.Keywords {
		add(t)
	}
	for _, t := range q.Skills {
		add(t)
	}
	for _, t := range goalWords(q.Goal) {
		add(t)
	}
	return out
}

// tsQuery is the terms as a PostgreSQL tsquery: any term (OR), the words of one term in sequence. Terms are folded
// words of letters and digits only, so nothing in them can break the query syntax.
func tsQuery(terms []string) string {
	parts := make([]string, 0, len(terms))
	for _, t := range terms {
		if ws := words(t); len(ws) > 0 {
			parts = append(parts, strings.Join(ws, " <-> "))
		}
	}
	return strings.Join(parts, " | ")
}

// matchedTerms are the terms found, as whole words, in a search text (wordText form).
func matchedTerms(terms []string, searchText string) []string {
	text := " " + searchText + " "
	out := []string{}
	for _, t := range terms {
		if t != "" && strings.Contains(text, " "+t+" ") {
			out = append(out, t)
		}
	}
	return out
}

// cut keeps at most n characters of s.
func cut(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n]))
}
