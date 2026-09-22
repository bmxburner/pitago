package app

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/components/theme"
)

// applyTheme swaps the palette + persists + refreshes input styles.
// Quiet: no chat notice (used for live preview while browsing the picker).
func (m *Model) applyTheme(name string) {
	t := theme.Get(name)
	ApplyTheme(t)
	m.ThemeName = t.Name
	_ = theme.Save(m.themePath, t.Name)
	m.ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(cInput)
	m.ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(cMuted)
	m.ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(cMuted)
}

// SetTheme applies a palette by name, persists it to theme.json, refreshes
// the input styles (built once in New) and posts a toast popup.
func (m *Model) SetTheme(name string) {
	m.applyTheme(name)
	m.AddBlock(Block{Kind: "notice", Text: "theme → " + m.ThemeName})
	m.Refresh()
}

// previewTheme live-applies the highlighted row of the theme picker
// (no-op for other dialogs or when already on that theme).
func (m *Model) previewTheme(d *Dialog) {
	if d.Kind != "theme" || len(d.FIdx) == 0 {
		return
	}
	if d.Cursor < 0 || d.Cursor >= len(d.FIdx) {
		return
	}
	ri := d.FIdx[d.Cursor]
	if ri < 0 || ri >= len(d.Options) || d.Options[ri] == m.ThemeName {
		return
	}
	m.applyTheme(d.Options[ri])
	m.Refresh()
}

// OpenTheme shows the theme picker (/theme with no args).
func (m *Model) OpenTheme() tea.Cmd {
	cur := m.ThemeName
	if cur == "" {
		cur = "default"
	}
	return func() tea.Msg {
		return PickerMsg{Kind: "theme", Options: theme.Names(), Current: cur}
	}
}
