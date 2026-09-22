package app

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/pirpc"
)

// Resume picker (pi's /resume): current-project sessions newest-first, Tab
// toggles the All scope. "/resume <path>" resumes a file directly; any
// other arg pre-filters the picker.

// OpenResume loads the current-project scope (pi's Current Folder).
func (m *Model) OpenResume(arg string) tea.Cmd {
	if m.spawnOpts.NoSession {
		m.AddBlock(Block{Kind: "notice", Text: "sessions aren't saved in --no-session mode"})
		m.Refresh()
		return nil
	}
	dir := pirpc.SessionDirFor(m.cwd)
	if arg != "" {
		if p := resolveSessionArg(dir, arg); p != "" {
			return m.SwitchSession(p)
		}
	}
	m.Status = "loading sessions…"
	m.Refresh()
	cwd, current := m.cwd, m.sessionFile
	return func() tea.Msg {
		list := pirpc.ListSessions(dir, cwd, 40, true)
		if len(list) == 0 {
			return SettingsRefreshMsg{Notice: "no saved sessions for this project yet — Tab for all"}
		}
		return resumePickerMsg("current", list, current, arg, false)
	}
}

// ReloadResumeScope reloads the picker in the other scope (Tab), keeping the
// typed filter.
func (m *Model) ReloadResumeScope(d *Dialog) tea.Cmd {
	scope := "all"
	if d.Scope == "all" {
		scope = "current"
	}
	m.Status = "loading sessions…"
	m.Refresh()
	cwd, current, filter := m.cwd, m.sessionFile, d.Filter
	return func() tea.Msg {
		var list []pirpc.SessionInfo
		if scope == "all" {
			list = pirpc.ListAllSessions(pirpc.SessionRoot(), 100)
		} else {
			list = pirpc.ListSessions(pirpc.SessionDirFor(cwd), cwd, 40, true)
		}
		if len(list) == 0 {
			return SettingsRefreshMsg{Notice: "no sessions in this scope"}
		}
		return resumePickerMsg(scope, list, current, filter, true)
	}
}

// resumePickerMsg builds the picker rows: title + "N msgs · age" (+ cwd in
// the All scope, like pi), current session marked and preselected.
func resumePickerMsg(scope string, list []pirpc.SessionInfo, current, filter string, replace bool) PickerMsg {
	opts := make([]string, 0, len(list))
	descs := make([]string, 0, len(list))
	paths := make([]string, 0, len(list))
	for _, s := range list {
		opts = append(opts, s.Title())
		desc := fmt.Sprintf("%d msgs · %s", s.MessageCount, pirpc.Ago(s.Modified))
		if scope == "all" && s.Cwd != "" {
			desc = pirpc.Shorten(s.Cwd) + " · " + desc
		}
		if s.Path == current {
			desc = "current · " + desc
		}
		descs = append(descs, desc)
		paths = append(paths, s.Path)
	}
	return PickerMsg{Kind: "sessions", Scope: scope, Options: opts, Descs: descs, Paths: paths, Filter: filter, Current: current, Replace: replace}
}

// DeleteResumeSession deletes the highlighted session file (Del / ⌫ with
// empty filter / Ctrl+D). Stays on the picker; closes it when empty.
// The current session can't be deleted — switch first.
func (m *Model) DeleteResumeSession(d *Dialog) (tea.Model, tea.Cmd) {
	if len(d.FIdx) == 0 || d.Cursor < 0 || d.Cursor >= len(d.FIdx) {
		return m, nil
	}
	ri := d.FIdx[d.Cursor]
	if ri < 0 || ri >= len(d.Paths) {
		return m, nil
	}
	path := d.Paths[ri]
	if path == "" {
		return m, nil
	}
	if path == m.sessionFile {
		m.Status = "can't delete the current session — switch first"
		m.Refresh()
		return m, nil
	}
	if err := pirpc.DeleteSession(path); err != nil {
		m.Status = "delete failed: " + err.Error()
		m.Refresh()
		return m, nil
	}
	title := ""
	if ri < len(d.Options) {
		title = d.Options[ri]
	}
	d.Options = append(d.Options[:ri], d.Options[ri+1:]...)
	if ri < len(d.Descs) {
		d.Descs = append(d.Descs[:ri], d.Descs[ri+1:]...)
	}
	d.Paths = append(d.Paths[:ri], d.Paths[ri+1:]...)
	d.Reindex()
	if d.Cursor >= len(d.FIdx) {
		d.Cursor = len(d.FIdx) - 1
	}
	if d.Cursor < 0 {
		d.Cursor = 0
	}
	if len(d.Options) == 0 {
		m.Dialogs = m.Dialogs[1:]
		m.AddBlock(Block{Kind: "notice", Text: "deleted session — no sessions left in this scope"})
		m.Refresh()
		return m, nil
	}
	if title == "" {
		title = path
	}
	m.Status = "deleted: " + Short(title, 50)
	m.Refresh()
	return m, nil
}

// resolveSessionArg maps "/resume <arg>" to a file: existing path as-is,
// else a name inside the session dir (pi --session <path|id> parity).
func resolveSessionArg(dir, arg string) string {
	if fi, err := os.Stat(arg); err == nil && !fi.IsDir() {
		return arg
	}
	if p := filepath.Join(dir, arg); p != "" {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}
