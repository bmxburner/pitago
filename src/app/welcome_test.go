package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"pitago/src/pirpc"
)

func TestWelcomeView(t *testing.T) {
	m := Model{AppVersion: "0.0.1", Cmds: []pirpc.RepoCommand{
		{Name: "mcp", Source: "extension"},
		{Name: "council", Source: "prompt"},
		{Name: "skill:archify", Source: "skill"},
		{Name: "model", Source: "builtin"},
	}}
	for _, w := range []int{100, 40} {
		got := stripANSI(m.welcomeView(w))
		for _, want := range []string{"Pitago v0.0.1", "ready", "Resources", "New session started", "█████"} {
			if !strings.Contains(got, want) {
				t.Fatalf("width %d missing %q:\n%s", w, want, got)
			}
		}
	}
}

// Regression (screenshot): the side-by-side condition guessed details at
// +30 cols, narrower than the hints line — medium terminals wrapped
// mid-logo and the block letters came out garbled. Joined rows must never
// exceed the chat width.
func TestWelcomeSideBySideFits(t *testing.T) {
	m := Model{AppVersion: "v0.0.1", Cmds: []pirpc.RepoCommand{
		{Name: "mcp", Source: "extension"},
		{Name: "council", Source: "prompt"},
		{Name: "skill:archify", Source: "skill"},
		{Name: "model", Source: "builtin"},
	}}
	for _, w := range []int{60, 66, 69, 72, 81, 120} {
		got := stripANSI(m.welcomeView(w))
		if strings.Contains(got, "vv") {
			t.Fatalf("width %d: doubled version prefix:\n%s", w, got)
		}
		for _, ln := range strings.Split(got, "\n") {
			// skip the trailing Resources line: it is long by design and
			// wraps in the viewport on narrow screens (out of scope)
			if strings.Contains(ln, "Resources") {
				continue
			}
			if wd := lipgloss.Width(ln); wd > w {
				t.Fatalf("width %d: row overflows (%d): %q", w, wd, ln)
			}
		}
	}
	// the title sits one row down with a blank lead-in above it
	wide := strings.Split(stripANSI(m.welcomeView(120)), "\n")
	if strings.Contains(wide[0], "Pitago") {
		t.Fatalf("title must not share the first logo row:\n%s", wide[0])
	}
}
