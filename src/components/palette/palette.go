// Package palette implements the / command-palette matching: a query
// matches every command whose name contains it (case-insensitive), so "/"
// shows the full list and the popup window scrolls. Pure, no TUI state.
package palette

import "strings"

// Win is the visible row window of the command popup (the full match list
// scrolls; the popup never grows past this). Var (not const) so the
// /settings "Autocomplete max" row can tune it like stock pi.
var Win = 10

// Match returns the indices of names matching query. Empty query matches
// everything.
func Match(query string, names []string) []int {
	q := strings.ToLower(query)
	var out []int
	for i, n := range names {
		if strings.Contains(strings.ToLower(n), q) {
			out = append(out, i)
		}
	}
	return out
}
