package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/components/theme"
)

func TestSetTheme(t *testing.T) {
	defer ApplyTheme(theme.Get("default")) // globals: don't leak into other tests
	m := New(nil, t.TempDir())
	m.SetTheme("gruvbox")
	if m.ThemeName != "gruvbox" {
		t.Fatalf("want gruvbox, got %q", m.ThemeName)
	}
	if cText != lipgloss.Color("#EBDBB2") {
		t.Fatalf("cText not swapped: %q", string(cText))
	}
	m.SetTheme("nope-unknown")
	if m.ThemeName != "default" {
		t.Fatalf("unknown should fall back to default, got %q", m.ThemeName)
	}
}

func TestOpenThemePicker(t *testing.T) {
	m := New(nil, t.TempDir())
	m.ThemeName = "dracula"
	cmd := m.OpenTheme()
	msg, ok := cmd().(PickerMsg)
	if !ok {
		t.Fatalf("want PickerMsg, got %T", cmd())
	}
	if msg.Kind != "theme" || len(msg.Options) != len(theme.Names()) {
		t.Fatalf("unexpected picker: %+v", msg)
	}
	if msg.Current != "dracula" {
		t.Fatalf("want current dracula, got %q", msg.Current)
	}
}

func TestThemeLivePreviewOnCursorMove(t *testing.T) {
	defer ApplyTheme(theme.Get("default")) // globals: don't leak into other tests
	m := New(nil, t.TempDir())
	m.ThemeName = "default"
	nm, _ := m.Update(PickerMsg{Kind: "theme", Options: theme.Names(), Current: "default"})
	got := nm.(Model)
	if len(got.Dialogs) == 0 {
		t.Fatal("want theme dialog open")
	}
	nm, _ = got.Update(tea.KeyMsg{Type: tea.KeyDown}) // default → one-dark
	got = nm.(Model)
	if got.ThemeName != "one-dark" {
		t.Fatalf("Down should live-apply one-dark, got %q", got.ThemeName)
	}
	if cText != lipgloss.Color("#ABB2BF") {
		t.Fatalf("cText not swapped: %q", string(cText))
	}
	nm, _ = got.Update(tea.KeyMsg{Type: tea.KeyUp}) // wraps back to default
	if nm.(Model).ThemeName != "default" {
		t.Fatalf("Up should wrap back to default, got %q", nm.(Model).ThemeName)
	}
}
