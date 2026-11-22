package find

import (
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gdo33s/rig/internal/fff"
)

// fff ranks text matches without regard to case or word boundaries, so
// "func Run(" can come before the "func run(" that was typed. rig sorts
// matches into tiers by how exactly they contain the query, keeping fff's
// order within each tier.
const (
	exactWord  = iota // the query, as typed, as whole words
	exact             // the query, as typed, inside a longer word
	foldedWord        // whole words, ignoring case
	folded            // anywhere, ignoring case
	loose             // fff's fuzzy matches
)

func tier(line, query string) int {
	query = strings.TrimSpace(query)
	if query == "" {
		return loose
	}
	if strings.Contains(line, query) {
		if wholeWords(line, query, false) {
			return exactWord
		}
		return exact
	}
	if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
		if wholeWords(line, query, true) {
			return foldedWord
		}
		return folded
	}
	return loose
}

// wholeWords reports whether query appears in line with no letter, digit
// or underscore running on from either end.
func wholeWords(line, query string, fold bool) bool {
	hay, needle := line, query
	if fold {
		hay, needle = strings.ToLower(line), strings.ToLower(query)
	}
	for from := 0; ; {
		i := strings.Index(hay[from:], needle)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(needle)
		if !wordAt(hay, start-1, true) && !wordAt(hay, end, false) {
			return true
		}
		from = start + 1
	}
}

// wordAt reports whether the character ending at byte i (before) or
// starting at byte i belongs to a word.
func wordAt(s string, i int, before bool) bool {
	var r rune
	switch {
	case before && i >= 0:
		r, _ = utf8.DecodeLastRuneInString(s[:i+1])
	case !before && i < len(s):
		r, _ = utf8.DecodeRuneInString(s[i:])
	default:
		return false
	}
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// rankMatches sorts matches by tier, keeping fff's order within each.
func rankMatches(matches []fff.Match, query string) []fff.Match {
	ranked := append([]fff.Match(nil), matches...)
	sort.SliceStable(ranked, func(i, j int) bool {
		return tier(ranked[i].Text, query) < tier(ranked[j].Text, query)
	})
	return ranked
}

// namesMatch reports whether a file plainly matches query: its name or
// path contains it, ignoring case and spaces. fff's fuzzy matches that
// only share scattered letters do not count.
func namesMatch(file, query string) bool {
	q := strings.ToLower(strings.Join(strings.Fields(query), ""))
	if q == "" {
		return true
	}
	p := strings.ToLower(file)
	return strings.Contains(path.Base(p), q) || strings.Contains(p, q)
}
