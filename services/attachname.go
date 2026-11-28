package services

import (
	"regexp"
	"strings"
	"unicode"
)

// maxStemRunes is the longest file name (without extension) an attachment gets.
const maxStemRunes = 80

var stemExt = regexp.MustCompile(`(?i)\.(pdf|docx|doc)$`)

// cleanStem turns what the student typed into a safe file name without extension: no folder, no .pdf/.docx/.doc
// at the end, letters (any script), marks, digits, spaces and _ - . ( ) kept, anything else turned into _, a run of
// spaces and _ made its first rune, no space . _ - at the ends, at most 80 runes. Empty: fallback. Same rule as
// resume-api's CV file name, so that the letter-vs-CV name check agrees with it.
func cleanStem(in, fallback string) string {
	if i := strings.LastIndexAny(in, `/\`); i >= 0 {
		in = in[i+1:]
	}
	in = stemExt.ReplaceAllString(strings.TrimSpace(in), "")
	var b strings.Builder
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		b.WriteRune(run[0])
		run = run[:0]
	}
	for _, r := range in {
		switch {
		case r == ' ' || r == '_':
			run = append(run, r)
			continue
		case unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) || strings.ContainsRune("-.()", r):
		default:
			run = append(run, '_')
			continue
		}
		flush()
		b.WriteRune(r)
	}
	flush()
	out := strings.Trim(b.String(), " ._-")
	if r := []rune(out); len(r) > maxStemRunes {
		out = strings.Trim(string(r[:maxStemRunes]), " ._-")
	}
	if out == "" {
		return fallback
	}
	return out
}

// letterPrefixes start the default name of the cover letter, per language.
var letterPrefixes = map[string]string{
	"fr": "Lettre_de_motivation",
	"en": "Cover_letter",
	"ar": "رسالة_تحفيزية",
}

// defaultLetterStem is the name the cover letter gets when the student gave none: the prefix of its language, then
// the name on the CV, words joined by _ (case, accents and Arabic kept). No name: the prefix alone.
func defaultLetterStem(language, contactName string) string {
	prefix, ok := letterPrefixes[language]
	if !ok {
		prefix = letterPrefixes["fr"]
	}
	if words := strings.Fields(contactName); len(words) > 0 {
		return cleanStem(prefix+"_"+strings.Join(words, "_"), prefix)
	}
	return prefix
}

// cvStem is the name of the CV file without extension, as resume-api gives it: the CV's title, else the name on
// it, else CV.
func cvStem(title, contactName string) string {
	return cleanStem(title, cleanStem(strings.Join(strings.Fields(contactName), "_"), "CV"))
}

// letterLanguage is the language of the cover letter: the one asked (fr, en, ar), else ar when the body is written
// in Arabic (more Arabic letters than Latin ones), else fr.
func letterLanguage(asked, body string) string {
	if _, ok := letterPrefixes[asked]; ok {
		return asked
	}
	arabic, latin := 0, 0
	for _, r := range body {
		switch {
		case unicode.Is(unicode.Arabic, r):
			arabic++
		case unicode.Is(unicode.Latin, r):
			latin++
		}
	}
	if arabic > latin {
		return "ar"
	}
	return "fr"
}
