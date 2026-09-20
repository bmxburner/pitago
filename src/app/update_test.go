package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Regression: /login → Enter API key → typing + Enter must reach the
// secret confirmer. The secret dialog has no Options (FIdx empty) and
// confirmDialog used to drop Enter for any such dialog, so the typed
// key could never be saved.
func TestSecretEnterReachesConfirmer(t *testing.T) {
	var m Model
	called := false
	m.confirm = map[string]ConfirmFunc{
		"secret": func(mm *Model, d *Dialog, ri int) (tea.Model, tea.Cmd) {
			called = true
			return mm, nil
		},
	}
	m.Dialogs = []*Dialog{{
		Kind: "secret", Title: "API key — groq",
		Filter: "gsk-test-key", LoginProvider: "groq", LoginEnv: "GROQ_API_KEY",
	}}
	_, _ = m.updateDialog(tea.KeyMsg{Type: tea.KeyEnter})
	if !called {
		t.Fatal("Enter on secret dialog did not reach secret confirmer")
	}
}
