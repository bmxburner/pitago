package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"openpi/src/components/recent"
)

// recentContentRow is the sidebar content row of the first recent model:
// PET(0) face(1) sep(2) SESSION(3) first(4) sess(5) sep(6) model(7) ctx(8)
// toks(9) sep(10) STATS head(11) time(12) last(13) speed(14) turns(15)
// left(16) sep(17) RECENT MODELS(18) models(19…).
// Screen y = 2 (header + box border) + row; recentAt must match.
// petRows is added (not inlined) so a PET height change moves this too.
const recentContentRow = 16 + petRows

// pushRecent moves (provider, id) to the front, dedupes, caps, persists.
// Ordering/cap live in components/recent; empty ids stay ignored (no write).

func (m *Model) pushRecent(provider, id, label string) {
	if strings.TrimSpace(id) == "" {
		return
	}
	m.recentModels = recent.Push(m.recentModels, provider, id, label)
	recent.Save(m.recentPath, m.recentModels)
}

// openRecents shows the recent-models picker (Ctrl+R / /recent).

func (m *Model) OpenRecents() tea.Cmd {
	if len(m.recentModels) == 0 {
		m.AddBlock(Block{Kind: "notice", Text: "no recent models yet — switch with /model first"})
		m.Refresh()
		return nil
	}
	opts := make([]string, 0, len(m.recentModels))
	descs := make([]string, 0, len(m.recentModels))
	for _, r := range m.recentModels {
		opts = append(opts, r.DispLabel())
		desc := r.Provider
		if r.ID == m.ModelLbl || r.DispLabel() == m.ModelLbl {
			desc = "current"
			if r.Provider != "" {
				desc += " · " + r.Provider
			}
		}
		descs = append(descs, desc)
	}
	d := &Dialog{Kind: "recent", Title: "Recent models", Options: opts, Descs: descs}
	d.Reindex()
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
	return nil
}

// switchToRecent switches to recentModels[idx] (resolving provider via
// GetModels when the entry only has a label). Result reuses ModelCycleMsg.

func (m *Model) SwitchToRecent(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.recentModels) {
		return nil
	}
	r := m.recentModels[idx]
	m.Status = "switching model…"
	m.Refresh()
	return func() tea.Msg {
		prov, id := r.Provider, r.ID
		if prov == "" {
			models, err := m.Pi.GetModels()
			if err != nil {
				return ModelCycleMsg{Err: err}
			}
			found := false
			for _, mi := range models {
				if mi.ID == r.ID || mi.Name == r.ID || mi.Name == r.DispLabel() {
					prov, id = mi.Provider, mi.ID
					if id == "" {
						id = mi.Name
					}
					found = true
					break
				}
			}
			if !found {
				return ModelCycleMsg{Err: fmt.Errorf("model %q not in pi model list", r.DispLabel())}
			}
		}
		label, err := m.Pi.SetModelByID(prov, id)
		return ModelCycleMsg{Label: label, Provider: prov, ID: id, Err: err}
	}
}

// firstUser returns the first user message text (session title line).

func (m Model) recentAt(x, y int) (int, bool) {
	if !m.ready || !m.showSide() || len(m.Dialogs) > 0 || len(m.recentModels) == 0 {
		return 0, false
	}
	if x < m.mainW()+1 || x > m.winW || y < 2+recentContentRow {
		return 0, false
	}
	// visible rows start at YOffset when the sidebar is scrolled
	idx := y - (2 + recentContentRow) + m.sideVp.YOffset
	if idx < 0 || idx >= len(m.recentModels) {
		return 0, false
	}
	return idx, true
}

// recentHint advertises click-switch only when the terminal reports mouse
// events (--mouse); otherwise clicks never reach the app, so show keys.
func (m Model) recentHint() string {
	if m.Mouse {
		return "click to switch · Alt+↑↓ scroll"
	}
	return "^R list · Alt+1…5 · Alt+↑↓ scroll"
}

// command palette (/ autocomplete) --------------------------------------------

// cmdPrefix returns the text after / when the input is an unfinished command.
