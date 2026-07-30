package designhealth

import (
	"sort"
	"strings"
)

// sortedKeys returns the map's keys sorted, joined for a stable message rendering.
func sortedKeys(m map[string]bool) string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ", ")
}

// containsFold reports whether haystack contains needle, case-insensitively. Used
// by the interim name-in-blurb volatility-encapsulation fallback join (the regime
// for older states in which no component carries the typed encapsulatesVolatilities
// list).
func containsFold(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
