package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/components/recent"
)

// recentBaseRows is the fixed rows above RECENT MODELS without detail
// extras: SESSION(3) first(4) sess(5) sep(6) model(7) ctx(8) toks(9)
// sep(10) STATS head(11) time(12) last(13) speed(14) turns(15) left(16)
// sep(17) RECENT MODELS(18) models(19…) — PET takes rows 0-2 (petRows),
// the file/msgs/cached detail rows and the COST section add sideExtraRows.
const recentBaseRows = 16

// recentContentRow is the sidebar content row of the first recent model.
// Screen y = 1 (box border) + row; recentAt must match.
func (m Model) recentContentRow() int {
	return recentBaseRows + petRows + m.sideExtraRows()
}

// sideExtraRows counts the SESSION-detail rows above RECENT MODELS: file
// + msgs + cached (always rendered) plus the COST section (header + rows +
// separator, only when the session spans more than one model).
func (m Model) sideExtraRows() int {
	n := 3
	if c := len(m.sideCostRows()); c > 0 {
		n += 2 + c
	}
	return n
}

// sideCostRows formats the sidebar COST breakdown: top-3 models by cost,
// only when the session spans more than one model (same rule as /session).
// Render and the click mapping both use it so the row math stays in sync.
func (m Model) sideCostRows() []string {
	if len(m.sessBreak) <= 1 {
		return nil
	}
	n := len(m.sessBreak)
	if n > 3 {
		n = 3
	}
	rows := make([]string, 0, n)
	for _, b := range m.sessBreak[:n] {
		rows = append(rows, Short(Short(b.Key, sideInnerW-8)+" "+fmt.Sprintf("$%.3f", b.Cost), sideInnerW))
	}
	return rows
}

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
	if x < m.mainW()+1 || x > m.winW || y < 1+m.recentContentRow() {
		return 0, false
	}
	// visible rows start at YOffset when the sidebar is scrolled
	idx := y - (1 + m.recentContentRow()) + m.sideVp.YOffset
	if idx < 0 || idx >= len(m.recentModels) {
		return 0, false
	}
	return idx, true
}

// pluginHeaderRow is the sidebar content row of the PLUGINS toggle header:
// first model row + recent rows + hint + sep + COMMANDS header + command
// rows. Everything after it (MCP/Todos/WORKSPACE) doesn't affect the row.
func (m Model) pluginHeaderRow() int {
	r := len(m.recentModels)
	if r == 0 {
		r = 1 // empty state renders one "—" row
	}
	c := 1 // COMMANDS counts (or "—")
	if len(m.queue.Steering)+len(m.queue.FollowUp) > 0 {
		c++ // queue line
	}
	return m.recentContentRow() + r + 3 + c
}

// pluginToggleAt reports a click on the PLUGINS header (collapses/expands
// the list). Same screen→content mapping as recentAt (box border 1,
// plus YOffset when scrolled).
func (m Model) pluginToggleAt(x, y int) bool {
	if !m.ready || !m.showSide() || len(m.Dialogs) > 0 {
		return false
	}
	if x < m.mainW()+1 || x > m.winW {
		return false
	}
	return y-1+m.sideVp.YOffset == m.pluginHeaderRow()
}

// recentHint advertises click-switch only when the terminal reports mouse
// events (--mouse); otherwise clicks never reach the app, so show keys.
// Sidebar scrolls with Ctrl+↑↓ (Alt+↑↓ also works, wheel with --mouse).
func (m Model) recentHint() string {
	if m.Mouse {
		return "click to switch · ^↑↓ scroll"
	}
	return "^R list · ^↑↓ scroll"
}

// command palette (/ autocomplete) --------------------------------------------

// cmdPrefix returns the text after / when the input is an unfinished command.
