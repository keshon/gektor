// Package discovery ranks metadata without loading tools or instructions.
package discovery

import "strings"

// Score counts matching query words in any order. Exact fields rank first.
// A zero score means no match; an empty query includes every entry.
func Score(query string, fields ...string) int {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return 1
	}

	text := strings.ToLower(strings.Join(fields, " "))
	score := 0
	seen := map[string]bool{}
	for _, term := range terms {
		if !seen[term] && strings.Contains(text, term) {
			score++
		}
		seen[term] = true
	}
	if score == 0 {
		return 0
	}

	query = strings.Join(terms, " ")
	for _, field := range fields {
		if strings.EqualFold(field, query) {
			return score + len(terms)
		}
	}
	return score
}
