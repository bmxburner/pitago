package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

func pasteTestModel() Model {
	var m Model
	m.ta = textarea.New()
	m.ta.Focus()
	m.ta.SetWidth(80)
	m.tools = make(map[string]int)
	return m
}

func runPasteCmd(t *testing.T, m Model, secret bool) pasteDoneMsg {
	t.Helper()
	cmd := m.pasteCmd(secret)
	if cmd == nil {
		t.Fatal("pasteCmd returned nil")
	}
	msg, ok := cmd().(pasteDoneMsg)
	if !ok {
		t.Fatalf("pasteCmd msg = %T", cmd())
	}
	return msg
}

// Ctrl+V must be owned by gotui (multi-backend + visible errors), not the
// textarea's silent built-in paste.
func TestCtrlVOwned(t *testing.T) {
	m := pasteTestModel()
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+V produced no command")
	}
	if got := m.ta.Value(); got != "" {
		t.Fatalf("Ctrl+V must paste async, value = %q", got)
	}
}

// End-to-end: Ctrl+V → clipboard text lands in the input.
func TestPasteInserts(t *testing.T) {
	old := clipRead
	clipRead = func() (string, error) { return "pasted text", nil }
	defer func() { clipRead = old }()

	m := pasteTestModel()
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if got := m.ta.Value(); got != "pasted text" {
		t.Fatalf("value = %q", got)
	}
}

// Mid-line + multi-line paste splices correctly and parks the cursor.
func TestPasteSplice(t *testing.T) {
	m := pasteTestModel()
	m.ta.SetValue("abXXcd")
	m.ta.SetCursor(2) // after "ab"
	m.insertAtCursor("1\n23")
	if got := m.ta.Value(); got != "ab1\n23XXcd" {
		t.Fatalf("value = %q", got)
	}
	row, col := m.cursorPos()
	if row != 1 || col != 2 {
		t.Fatalf("cursor = (%d,%d), want (1,2)", row, col)
	}
}

// Backend failure must explain itself (old behavior: silent nothing).
func TestPasteErrorNotice(t *testing.T) {
	old := clipRead
	clipRead = func() (string, error) { return "", errors.New("xclip not found") }
	defer func() { clipRead = old }()

	m := pasteTestModel()
	m.applyPaste(runPasteCmd(t, m, false))
	if len(m.blocks) != 1 || !m.blocks[0].Err {
		t.Fatalf("blocks = %+v", m.blocks)
	}
	if !strings.Contains(m.blocks[0].Text, "Cmd+V") {
		t.Fatalf("notice = %q", m.blocks[0].Text)
	}
}

func TestPasteEmptyNotice(t *testing.T) {
	old := clipRead
	clipRead = func() (string, error) { return "  \n", nil }
	defer func() { clipRead = old }()
	oldImg := clipImage
	clipImage = func() (string, error) { return "", errNoImage }
	defer func() { clipImage = oldImg }()

	m := pasteTestModel()
	m.applyPaste(runPasteCmd(t, m, false))
	if len(m.blocks) != 1 || strings.Contains(m.ta.Value(), " ") {
		t.Fatalf("blocks=%+v value=%q", m.blocks, m.ta.Value())
	}
}

// Ctrl+V inside /login secret dialog pastes the API key.
func TestPasteSecret(t *testing.T) {
	old := clipRead
	clipRead = func() (string, error) { return "gsk-key-123\n", nil }
	defer func() { clipRead = old }()

	m := pasteTestModel()
	m.Dialogs = []*Dialog{{Kind: "secret", Title: "API key"}}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("Ctrl+V in secret dialog produced no command")
	}
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if m.Dialogs[0].Filter != "gsk-key-123" {
		t.Fatalf("filter = %q", m.Dialogs[0].Filter)
	}
}
