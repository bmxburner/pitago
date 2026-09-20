package app

import (
	"testing"

	"github.com/charmbracelet/bubbles/textarea"

	"gotui/src/pirpc"
)

// "/" must match everything (no 8-item cap); the popup window scrolls.
func TestRefreshCmdsShowsAll(t *testing.T) {
	var m Model
	builtins := []Builtin{
		{Name: "model", Desc: "Select model"},
		{Name: "recent", Desc: "Switch recent model"},
	}
	for i := 0; i < 15; i++ {
		builtins = append(builtins, Builtin{Name: "fake" + string(rune('a'+i)), Desc: "fake"})
	}
	m.UseBuiltins(builtins, nil)
	m.Cmds = append(BuiltinRepo(m.builtins), []pirpc.RepoCommand{
		{Name: "mcp", Description: "Show MCP server status", Source: "extension"},
		{Name: "council", Description: "Run a council", Source: "prompt"},
		{Name: "skill:archify", Description: "Diagrams", Source: "skill"},
	}...)
	m.ta = textarea.New()
	m.ta.SetValue("/")
	m.refreshCmds()
	if !m.cmdOpen {
		t.Fatal("popup must open on /")
	}
	if len(m.cmdItems) != len(m.Cmds) {
		t.Fatalf("want all %d cmds, got %d", len(m.Cmds), len(m.cmdItems))
	}
	if h := m.popupH(); h > cmdWin+5 {
		t.Fatalf("popup height %d exceeds window %d", h, cmdWin)
	}
	m.cmdCursor = len(m.cmdItems) - 1
	m.ensureCmdVisible()
	if m.cmdOffset+cmdWin != len(m.cmdItems) {
		t.Fatalf("last row must scroll into view, offset=%d", m.cmdOffset)
	}
}

// Builtins are intercepted locally; RPC commands (extension/prompt/skill)
// must fall through to Prompt forwarding.
func TestFindBuiltinRouting(t *testing.T) {
	var m Model
	m.UseBuiltins([]Builtin{{Name: "model"}, {Name: "export"}}, nil)
	if _, _, ok := m.FindBuiltin("/mcp"); ok {
		t.Error("/mcp is an RPC command, must not be intercepted")
	}
	if b, arg, ok := m.FindBuiltin("/model anthropic/x"); !ok || b.Name != "model" || arg != "anthropic/x" {
		t.Errorf("want model+arg, got %q %q %v", b.Name, arg, ok)
	}
	if b, _, ok := m.FindBuiltin("/export"); !ok || b.Name != "export" {
		t.Error("/export must be intercepted (pi-TUI-only, never chat text)")
	}
	if m.RunBuiltin("nope", "") != nil {
		t.Error("unknown builtin must return nil cmd")
	}
}
