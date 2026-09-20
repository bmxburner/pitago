package app

import (
	"strings"
	"testing"

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
