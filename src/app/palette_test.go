package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"pitago/src/components/format"
	"pitago/src/components/palette"

	"pitago/src/pirpc"
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
	if h := m.popupH(); h > palette.Win+5 {
		t.Fatalf("popup height %d exceeds window %d", h, palette.Win)
	}
	m.cmdCursor = len(m.cmdItems) - 1
	m.ensureCmdVisible()
	if m.cmdOffset+palette.Win != len(m.cmdItems) {
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

// Extension commands show pi's "[u:npm:ext] desc" tag and match by extension
// name; builtins keep the trailing [builtin].
func TestCmdExtensionTag(t *testing.T) {
	var m Model
	m.UseBuiltins([]Builtin{{Name: "model", Desc: "Select model"}}, nil)
	m.Cmds = append(BuiltinRepo(m.builtins), []pirpc.RepoCommand{
		{Name: "subagents", Description: "Administer subagents", Source: "extension",
			SourceInfo: &pirpc.SourceInfo{Scope: "user", Source: "npm:pi-subagents"}},
		{Name: "council", Description: "Run a council", Source: "prompt"},
	}...)
	m.ta = textarea.New()
	m.winW = 120

	m.ta.SetValue("/pi-subagents")
	m.refreshCmds()
	if len(m.cmdItems) != 1 || m.Cmds[m.cmdItems[0]].Name != "subagents" {
		t.Fatalf("filter by extension must match /subagents, got %v", m.cmdItems)
	}

	m.ta.SetValue("/")
	m.refreshCmds()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(termenv.Ascii)
	out := m.renderCmdPopup()
	plain := format.StripANSI(out)
	for _, want := range []string{"/subagents — [u:npm:pi-subagents]", "/model — Select model [builtin]", "/council — Run a council [prompt]"} {
		if !strings.Contains(plain, want) {
			t.Errorf("popup missing %q\n%s", want, plain)
		}
	}
	if !strings.Contains(out, "[36m") {
		t.Errorf("command names must render cyan, got\n%s", out)
	}
}
