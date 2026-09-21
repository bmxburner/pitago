package app

import (
	"errors"
	"testing"
)

func TestHandleUpdateCheckManualOpensDialog(t *testing.T) {
	var m Model
	m.handleUpdateCheck(UpdateCheckMsg{Current: "v0.0.1", Latest: "v0.0.2", IsNew: true})
	if len(m.Dialogs) != 1 {
		t.Fatalf("expected 1 dialog, got %d", len(m.Dialogs))
	}
	d := m.Dialogs[0]
	if d.Kind != "update" || d.UpdateTo != "v0.0.2" {
		t.Errorf("unexpected dialog %+v", d)
	}
}

func TestHandleUpdateCheckAutoSilent(t *testing.T) {
	var m Model
	// auto, nothing new → no dialog, no blocks
	m.handleUpdateCheck(UpdateCheckMsg{Current: "v0.0.2", Latest: "v0.0.2", Auto: true})
	if len(m.Dialogs) != 0 {
		t.Error("auto check must not open dialogs when up to date")
	}
	// auto, error (offline) → silent
	m.handleUpdateCheck(UpdateCheckMsg{Current: "v0.0.2", Auto: true, Err: errors.New("no route")})
	if len(m.Dialogs) != 0 {
		t.Error("auto check must stay silent on error")
	}
}

func TestHandleUpdateDone(t *testing.T) {
	var m Model
	m.handleUpdateDone(UpdateDoneMsg{From: "v0.0.1", To: "v0.0.2"})
	if len(m.blocks) != 1 {
		t.Fatalf("expected 1 notice block, got %d", len(m.blocks))
	}
}
