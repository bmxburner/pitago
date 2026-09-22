package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/pirpc"
)

func testPconfigModel() *Model {
	m := &Model{
		Cmds: []pirpc.RepoCommand{
			{Name: "skill:archify", Description: "arch diagrams", Source: "skill"},
			{Name: "council", Description: "advisor council", Source: "prompt"},
			{Name: "mcp", Description: "MCP status", Source: "extension",
				SourceInfo: &pirpc.SourceInfo{Source: "npm:pi-mcp-adapter", Scope: "user"}},
		},
		Plugins: []Plugin{{Spec: "npm:pi-lens", Name: "pi-lens"}},
		MCP:     []McpServer{{Name: "notion", Direct: 2, Total: 5, Tokens: 1200, Connected: true}},
		Stats:   pirpc.Stats{ToolCalls: 3},
	}
	m.AddBlock(Block{Kind: "tool", ToolName: "read", ToolStatus: "done", ToolArgs: "pi.json"})
	m.AddBlock(Block{Kind: "tool", ToolName: "read", ToolStatus: "done"})
	m.AddBlock(Block{Kind: "tool", ToolName: "bash", ToolStatus: "error"})
	return m
}

func TestOpenPconfigTwoPane(t *testing.T) {
	m := testPconfigModel()
	m.OpenPconfig()
	if len(m.Dialogs) != 1 || m.Dialogs[0].Kind != "pconfig" {
		t.Fatalf("expected one pconfig dialog, got %+v", m.Dialogs)
	}
	d := m.Dialogs[0]
	if len(d.Provs) != 9 || len(d.PsecIDs) != 9 {
		t.Fatalf("left pane needs 9 sections, got %d/%d", len(d.Provs), len(d.PsecIDs))
	}
	if !d.ProvFocus {
		t.Error("focus should start on the left (sections) pane")
	}
	// initial section = Agent action row
	if len(d.Options) != 1 || d.Payload[0] != "@agent" {
		t.Fatalf("agent section should show one action row, got %v/%v", d.Options, d.Payload)
	}
}

func TestPconfigSectionSwitch(t *testing.T) {
	m := testPconfigModel()
	m.OpenPconfig()
	d := m.Dialogs[0]
	d.ProvCursor = 1 // Skills
	m.LoadPsecRows(d)
	if len(d.Options) != 1 || d.Options[0] != "/skill:archify" {
		t.Fatalf("unexpected skill rows: %v", d.Options)
	}
	if d.Payload[0] != "skill:archify" {
		t.Errorf("payload should carry the runnable name, got %q", d.Payload[0])
	}
	// filter narrows the right pane
	d.Filter = "arch"
	d.Reindex()
	if len(d.FIdx) != 1 {
		t.Errorf("filter should match 1 row, got %d", len(d.FIdx))
	}
	d.Filter = "zzz"
	d.Reindex()
	if len(d.FIdx) != 0 {
		t.Errorf("filter should match 0 rows, got %d", len(d.FIdx))
	}
	// Tools section aggregates the transcript
	for i, id := range d.PsecIDs {
		if id == PsecTool {
			d.ProvCursor = i
		}
	}
	m.LoadPsecRows(d)
	if len(d.Options) != 2 || d.Options[0] != "bash" || d.Options[1] != "read" {
		t.Fatalf("tool rows should be sorted names, got %v", d.Options)
	}
	if d.Descs[1] != "2x · 2 done — pi.json" {
		t.Errorf("read aggregate wrong: %q", d.Descs[1])
	}
}

func TestPconfigEmptySections(t *testing.T) {
	m := &Model{}
	m.OpenPconfig()
	d := m.Dialogs[0]
	d.ProvCursor = 1 // Skills, empty
	m.LoadPsecRows(d)
	if d.Options[0] != "— no skills —" || d.Payload[0] != "" {
		t.Errorf("empty skills should show an info placeholder, got %v/%v", d.Options, d.Payload)
	}
}

func TestPconfigCurPsecBounds(t *testing.T) {
	d := &Dialog{PsecIDs: []string{"a", "b"}, ProvCursor: 9}
	if d.CurPsec() != "a" {
		t.Errorf("out-of-range cursor should fall back to first, got %q", d.CurPsec())
	}
	if (&Dialog{}).CurPsec() != "" {
		t.Error("no sections should give empty id")
	}
}

func TestPconfigKeys(t *testing.T) {
	m := testPconfigModel()
	m.OpenPconfig()
	key := func(k tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: k} }
	step := func(mm tea.Model) *Model {
		m2 := mm.(Model)
		return &m2
	}

	// ↓ on sections reloads the right pane to Skills
	m = step(mustUpdate(t, m, key(tea.KeyDown)))
	d := m.Dialogs[0]
	if d.CurPsec() != PsecSkill || len(d.Options) != 1 {
		t.Fatalf("down should select skills, got %s %v", d.CurPsec(), d.Options)
	}
	// Enter on the left focuses the right pane
	m = step(mustUpdate(t, m, key(tea.KeyEnter)))
	if m.Dialogs[0].ProvFocus {
		t.Error("enter on sections should focus the right pane")
	}
	// ← goes back to sections
	m = step(mustUpdate(t, m, key(tea.KeyLeft)))
	if !m.Dialogs[0].ProvFocus {
		t.Error("left should refocus sections")
	}
	// typing filters the right pane
	m = step(mustUpdate(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")}))
	if len(m.Dialogs[0].FIdx) != 0 {
		t.Error("typing should filter right rows to zero")
	}
}

func mustUpdate(t *testing.T, m *Model, km tea.KeyMsg) tea.Model {
	t.Helper()
	mm, _ := m.updatePconfigDialog(km, m.Dialogs[0])
	return mm
}
