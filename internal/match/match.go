// Package match implements the keyword matching used by filters and scoring.
//
// Terms match case-insensitively on word boundaries, so "go" does not match
// "google" or "ago". A space in a term matches any run of spaces or hyphens,
// and a hyphen matches an optional space or hyphen, so "on-site" matches
// "onsite", "on site" and "on-site". A term prefixed with "=" matches
// case-sensitively: "=Go" matches the language but not "go the extra mile".
package match

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Term is a single compiled keyword.
type Term struct {
	Name string
	re   *regexp.Regexp
}

// Compile parses a term using the syntax described in the package doc.
func Compile(raw string) (Term, error) {
	caseSensitive := strings.HasPrefix(raw, "=")
	words := strings.Fields(strings.TrimPrefix(raw, "="))
	if len(words) == 0 {
		return Term{}, fmt.Errorf("empty match term %q", raw)
	}
	name := strings.Join(words, " ")

	var b strings.Builder
	for i, r := range name {
		switch {
		case r == ' ':
			b.WriteString(`[\s\-]+`)
		case r == '-' && i > 0:
			b.WriteString(`[\s\-]?`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	pattern := `(?:^|[^\p{L}\p{N}])` + b.String() + `(?:$|[^\p{L}\p{N}])`
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return Term{}, fmt.Errorf("match term %q: %w", raw, err)
	}
	return Term{Name: name, re: re}, nil
}

// In reports whether the term occurs in any of texts.
func (t Term) In(texts ...string) bool {
	for _, s := range texts {
		if t.re.MatchString(s) {
			return true
		}
	}
	return false
}

// Set is a list of terms where any one matching counts as a match.
type Set []Term

// NewSet compiles every raw term.
func NewSet(raws []string) (Set, error) {
	set := make(Set, 0, len(raws))
	for _, raw := range raws {
		t, err := Compile(raw)
		if err != nil {
			return nil, err
		}
		set = append(set, t)
	}
	return set, nil
}

// First returns the name of the first term found in any of texts.
func (s Set) First(texts ...string) (string, bool) {
	for _, t := range s {
		if t.In(texts...) {
			return t.Name, true
		}
	}
	return "", false
}

// Weighted is a term with a score contribution.
type Weighted struct {
	Term
	Weight int
}

// NewWeighted compiles a term→weight map, ordered by term for stable output.
func NewWeighted(m map[string]int) ([]Weighted, error) {
	raws := make([]string, 0, len(m))
	for raw := range m {
		raws = append(raws, raw)
	}
	sort.Strings(raws)
	out := make([]Weighted, 0, len(raws))
	for _, raw := range raws {
		t, err := Compile(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, Weighted{Term: t, Weight: m[raw]})
	}
	return out, nil
}
