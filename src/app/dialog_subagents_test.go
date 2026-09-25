package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func subagentsTestModel(t *testing.T) Model {
	t.Setenv("PI_AGENT_DIR", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", "")
	m := New(nil, t.TempDir())
	now := time.Now()
	done := now.Add(-time.Minute)
	m.Subagents = []SubagentRow{
		{ID: "c1", Name: "scout", AgentName: "explore", Task: "map auth", Tool: "subagent", Status: SubagentActive, StatusLabel: "read", StartedAt: now.Add(-90 * time.Second)},
		{ID: "c2", Name: "builder", Tool: "subagent", Status: SubagentDone, StartedAt: now.Add(-5 * time.Minute), DoneAt: &done, Result: "built it"},
	}
	return m
}

func openSubagents(t *testing.T, m Model) Model {
	t.Helper()
	m.OpenSubagentHerd()
	if len(m.Dialogs) != 1 || m.Dialogs[0].Kind != "subagent-herd" {
		t.Fatalf("dialogs = %+v", m.Dialogs)
	}
	return m
}

func pressSubagents(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	tm, _ := m.Update(msg)
	return tm.(Model)
}

func TestOpenSubagentsList(t *testing.T) {
	m := openSubagents(t, subagentsTestModel(t))
	d := m.Dialogs[0]
	if d.Scope != subagentsScopeActive {
		t.Fatalf("scope = %q", d.Scope)
	}
	if len(d.Options) != 1 || d.Options[0] != "scout" {
		t.Fatalf("active options = %v", d.Options)
	}
	out := stripANSI(m.renderDialog())
	if !strings.Contains(out, "scout") || !strings.Contains(out, "read") {
		t.Fatalf("render missing row:\n%s", out)
	}
}

func TestSubagentsFilter(t *testing.T) {
	m := openSubagents(t, subagentsTestModel(t))
	m.Subagents = append(m.Subagents, SubagentRow{ID: "c3", Name: "wraith", Status: SubagentActive, StartedAt: time.Now()})
	m.OpenSubagentHerd()
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sc")})
	d := m.Dialogs[0]
	if len(d.FIdx) != 1 {
		t.Fatalf("FIdx = %v for filter %q", d.FIdx, d.Filter)
	}
	// no match → empty state row
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")})
	if len(m.Dialogs[0].FIdx) != 0 {
		t.Fatalf("FIdx should be empty, got %v", m.Dialogs[0].FIdx)
	}
	if out := stripANSI(m.renderDialog()); !strings.Contains(out, "no subagents") {
		t.Fatalf("empty state missing:\n%s", out)
	}
}

func TestSubagentsTabScope(t *testing.T) {
	m := openSubagents(t, subagentsTestModel(t))
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyTab})
	d := m.Dialogs[0]
	if d.Scope != subagentsScopeFinished {
		t.Fatalf("scope = %q", d.Scope)
	}
	if len(d.Options) != 1 || d.Options[0] != "builder" {
		t.Fatalf("finished options = %v", d.Options)
	}
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyTab})
	if m.Dialogs[0].Scope != subagentsScopeActive {
		t.Fatal("second Tab should return to active")
	}
}

func TestSubagentsDetailOpenBack(t *testing.T) {
	m := openSubagents(t, subagentsTestModel(t))
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	d := m.Dialogs[0]
	if !d.SubDetail || d.SubID != "c1" {
		t.Fatalf("detail = %+v", d)
	}
	if d.SubHeader == "" || len(d.SubLines) == 0 {
		t.Fatalf("detail content missing: %+v", d)
	}
	out := stripANSI(m.renderDialog())
	if !strings.Contains(out, "scout") || !strings.Contains(out, "map auth") {
		t.Fatalf("detail render missing content:\n%s", out)
	}
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.Dialogs[0].SubDetail {
		t.Fatal("Esc should close detail, not the dialog")
	}
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if len(m.Dialogs) != 0 {
		t.Fatal("second Esc should close the dialog")
	}
}

func TestSubagentsDetailScrollSize(t *testing.T) {
	m := openSubagents(t, subagentsTestModel(t))
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.Dialogs[0].SubOffset != 20 {
		t.Fatalf("offset = %d", m.Dialogs[0].SubOffset)
	}
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	if m.Dialogs[0].SubOffset != 0 {
		t.Fatalf("offset = %d", m.Dialogs[0].SubOffset)
	}
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyCtrlO})
	if m.Dialogs[0].SubSize != 1 {
		t.Fatalf("size = %d", m.Dialogs[0].SubSize)
	}
}

func TestSubagentActionPrompt(t *testing.T) {
	row := SubagentRow{ID: "c1", Name: "scout"}
	for _, a := range []string{"x", "w", "R"} {
		text, ok := subagentActionPrompt(a, row)
		if !ok || !strings.Contains(text, "scout") || !strings.Contains(text, "c1") {
			t.Errorf("action %q = %q %v", a, text, ok)
		}
	}
	if _, ok := subagentActionPrompt("s", row); ok {
		t.Error("steer should open the nested dialog, not a prompt")
	}
	if _, ok := subagentActionPrompt("bogus", row); ok {
		t.Error("unknown action should not dispatch")
	}
}

// openDetail focuses the first row and opens detail (actions live there:
// list mode is navigation + filter only, so typing never collides with
// action letters in names like "explore").
func openDetail(t *testing.T, m Model) Model {
	t.Helper()
	m = openSubagents(t, m)
	return pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyEnter})
}

func TestSubagentsActionDispatch(t *testing.T) {
	// x closes the dialog and yields a send command (not executed: no Pi)
	m := openDetail(t, subagentsTestModel(t))
	tm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = tm.(Model)
	if len(m.Dialogs) != 0 || cmd == nil {
		t.Fatalf("x: dialogs=%d cmd=%v", len(m.Dialogs), cmd != nil)
	}
	// s opens the nested steer input on top
	m = openDetail(t, subagentsTestModel(t))
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	if len(m.Dialogs) != 2 || m.Dialogs[0].Kind != "subagents-steer" {
		t.Fatalf("steer dialogs = %+v", m.Dialogs)
	}
	// typing + Enter submits (command not executed here); the list
	// dialog stays underneath
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("keep going")})
	m = tm.(Model)
	tm, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = tm.(Model)
	if len(m.Dialogs) != 1 || cmd == nil {
		t.Fatalf("steer submit: dialogs=%d cmd=%v", len(m.Dialogs), cmd != nil)
	}
	// f shows the surface and closes
	m = openDetail(t, subagentsTestModel(t))
	m = pressSubagents(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")})
	if len(m.Dialogs) != 0 {
		t.Fatal("f should close the dialog")
	}
	found := false
	for _, tst := range m.toasts {
		if strings.Contains(tst.Text, "scout") {
			found = true
		}
	}
	if !found {
		t.Fatalf("f should toast the surface, toasts=%+v", m.toasts)
	}
	// X dismisses locally with a notice, no turn spent (no send command)
	m = openDetail(t, subagentsTestModel(t))
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	m = tm.(Model)
	if len(m.Dialogs) != 0 {
		t.Fatalf("X: dialogs=%d", len(m.Dialogs))
	}
}

func TestSubagentsEmpty(t *testing.T) {
	t.Setenv("PI_AGENT_DIR", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", "")
	m := New(nil, t.TempDir())
	m.OpenSubagentHerd()
	if len(m.Dialogs[0].Options) != 0 {
		t.Fatalf("options = %v", m.Dialogs[0].Options)
	}
	if out := stripANSI(m.renderDialog()); !strings.Contains(out, "no subagents") {
		t.Fatalf("empty state missing:\n%s", out)
	}
}
