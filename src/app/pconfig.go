package app

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/pirpc"
)

// Pitago settings hub (/pitago-setting): two-pane dialog like the /model
// picker — left = sections, right = the selected section's rows. All rows
// come from Model state (no RPC), so the hub opens instantly.
// Keys: ↑↓ move in the focused pane, ←/→/Tab switch pane, typing filters
// the right pane, Enter runs/opens, Esc closes.

// Hub section ids (Dialog.PsecIDs parallels the left pane).
const (
	PsecAgent  = "agent"
	PsecSkill  = "skill"
	PsecPrompt = "prompt"
	PsecExt    = "extension"
	PsecPlugin = "plugin"
	PsecMCP    = "mcp"
	PsecTool   = "tool"
	PsecTheme  = "theme"
	PsecLogin  = "login"
)

// Payload markers for non-runnable right rows: "@<section>" opens the
// classic single dialog, "" is info-only (toast hint on Enter).
const (
	psecActAgent = "@agent"
	psecActTheme = "@theme"
	psecActLogin = "@login"
)

// CountCmds tallies extension catalog commands per source (skill/prompt/
// extension) for the section labels.
func CountCmds(cmds []pirpc.RepoCommand) (skill, prompt, ext int) {
	for _, c := range cmds {
		switch c.Source {
		case "skill":
			skill++
		case "prompt":
			prompt++
		case "extension":
			ext++
		}
	}
	return skill, prompt, ext
}

// psecCount returns the left-pane badge for a section (-1 = no badge).
func psecCount(m *Model, id string) int {
	sk, pr, ex := CountCmds(m.Cmds)
	switch id {
	case PsecSkill:
		return sk
	case PsecPrompt:
		return pr
	case PsecExt:
		return ex
	case PsecPlugin:
		return len(m.Plugins)
	case PsecMCP:
		return len(m.MCP)
	case PsecTool:
		return m.Stats.ToolCalls
	}
	return -1
}

// OpenPconfig pushes the two-pane settings hub (focus starts on sections).
func (m *Model) OpenPconfig() {
	d := &Dialog{Kind: "pconfig", Title: "Pitago settings",
		Provs:     []string{"Agent", "Skills", "Prompts", "Extensions", "Plugins", "MCP", "Tools", "Theme", "Login"},
		PsecIDs:   []string{PsecAgent, PsecSkill, PsecPrompt, PsecExt, PsecPlugin, PsecMCP, PsecTool, PsecTheme, PsecLogin},
		ProvFocus: true}
	m.LoadPsecRows(d)
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
}

// CurPsec is the selected section id (left pane cursor).
func (d *Dialog) CurPsec() string {
	if len(d.PsecIDs) == 0 {
		return ""
	}
	if d.ProvCursor < 0 || d.ProvCursor >= len(d.PsecIDs) {
		return d.PsecIDs[0]
	}
	return d.PsecIDs[d.ProvCursor]
}

// LoadPsecRows rebuilds the right pane for the selected section.
func (m *Model) LoadPsecRows(d *Dialog) {
	opts, descs, payload, msg := psecRows(m, d.CurPsec())
	d.Options, d.Descs, d.Payload = opts, descs, payload
	d.Message = msg
	d.Cursor = 0
	d.Reindex()
}

// psecRows builds one section's right pane. Payload parallels Options: the
// /command name for runnable rows, "@agent"/"@theme"/"@login" for action
// rows, "" for info-only rows.
func psecRows(m *Model, id string) (opts, descs, payload []string, msg string) {
	switch id {
	case PsecAgent:
		msg = "Enter opens the agent settings dialog · Esc closes"
		return []string{"Open agent settings →"},
			[]string{"model · thinking · steering · images · skills · … (pi parity)"},
			[]string{psecActAgent}, msg
	case PsecTheme:
		msg = "Enter opens the theme picker · Esc closes"
		return []string{"Open theme picker →"},
			[]string{"switch TUI theme"},
			[]string{psecActTheme}, msg
	case PsecLogin:
		msg = "Enter opens login · Esc closes"
		return []string{"Open login →"},
			[]string{"providers · keys · OAuth"},
			[]string{psecActLogin}, msg
	case PsecSkill:
		msg = "Enter fills /command in the input · Esc closes"
		for _, c := range m.Cmds {
			if c.Source != "skill" {
				continue
			}
			opts = append(opts, "/"+c.Name)
			descs = append(descs, shortDesc(c.Description, 48))
			payload = append(payload, c.Name)
		}
		if len(opts) == 0 {
			opts = []string{"— no skills —"}
			descs = []string{"/reload refreshes the catalog"}
			payload = []string{""}
		}
	case PsecPrompt:
		msg = "Enter fills /command in the input · Esc closes"
		for _, c := range m.Cmds {
			if c.Source != "prompt" {
				continue
			}
			opts = append(opts, "/"+c.Name)
			descs = append(descs, shortDesc(c.Description, 48))
			payload = append(payload, c.Name)
		}
		if len(opts) == 0 {
			opts = []string{"— no prompts —"}
			descs = []string{"/reload refreshes the catalog"}
			payload = []string{""}
		}
	case PsecExt:
		msg = "Enter fills /command in the input · Esc closes"
		for _, c := range m.Cmds {
			if c.Source != "extension" {
				continue
			}
			d := shortDesc(c.Description, 40)
			if tag := extTag(c); tag != "" {
				d += " [" + tag + "]"
			}
			opts = append(opts, "/"+c.Name)
			descs = append(descs, d)
			payload = append(payload, c.Name)
		}
		if len(opts) == 0 {
			opts = []string{"— no extensions —"}
			descs = []string{"/reload refreshes the catalog"}
			payload = []string{""}
		}
	case PsecPlugin:
		msg = "Read-only: `pi config` enables/disables package resources, then /reload · Esc closes"
		for _, p := range m.Plugins {
			opts = append(opts, p.Name)
			descs = append(descs, p.Spec)
			payload = append(payload, "")
		}
		if len(opts) == 0 {
			opts = []string{"— no packages —"}
			descs = []string{"pi install <source> adds one"}
			payload = []string{""}
		}
	case PsecMCP:
		msg = "Enter on a connected server fills /mcp · disabled ones change via mcp.json + /reload · Esc closes"
		for _, s := range m.MCP {
			switch {
			case s.Disabled:
				opts = append(opts, s.Name)
				descs = append(descs, "disabled in mcp.json")
				payload = append(payload, "")
			case s.Connected:
				opts = append(opts, s.Name)
				descs = append(descs, fmt.Sprintf("● %d/%d direct · ~%s tok", s.Direct, s.Total, fmtComma(s.Tokens)))
				payload = append(payload, "mcp")
			default:
				opts = append(opts, s.Name)
				descs = append(descs, "○ not connected")
				payload = append(payload, "")
			}
		}
		if len(opts) == 0 {
			opts = []string{"— no servers —"}
			descs = []string{"add one to ~/.pi/agent/mcp.json"}
			payload = []string{""}
		}
	case PsecTool:
		msg = "Per-tool calls this session (from the transcript) · Ctrl+G expands tool output · Esc closes"
		for _, t := range toolStats(m) {
			opts = append(opts, t.name)
			descs = append(descs, t.desc)
			payload = append(payload, "")
		}
		if len(opts) == 0 {
			opts = []string{"— no tool calls yet —"}
			descs = []string{"tools appear here as the agent works"}
			payload = []string{""}
		}
	}
	return opts, descs, payload, msg
}

// extTag shortens an extension source ("npm:pi-subagents" → "pi-subagents")
// for the row suffix.
func extTag(c pirpc.RepoCommand) string {
	if c.SourceInfo == nil {
		return ""
	}
	s := strings.TrimSpace(c.SourceInfo.Source)
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i:], "/") {
		return s[i+1:]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func shortDesc(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// toolStat is one per-tool aggregate for the Tools section.
type toolStat struct {
	name string
	desc string
}

// toolStats aggregates the transcript's tool blocks by name (done/running/
// error split). Reads m.blocks directly — same source the chat renders.
func toolStats(m *Model) []toolStat {
	type agg struct {
		n, done, run, err int
		lastArgs          string
	}
	byName := map[string]*agg{}
	var order []string
	for _, b := range m.blocks {
		if b.Kind != "tool" || b.ToolName == "" {
			continue
		}
		a, ok := byName[b.ToolName]
		if !ok {
			a = &agg{}
			byName[b.ToolName] = a
			order = append(order, b.ToolName)
		}
		a.n++
		switch b.ToolStatus {
		case "done":
			a.done++
		case "error":
			a.err++
		default:
			a.run++
		}
		if b.ToolArgs != "" {
			a.lastArgs = b.ToolArgs
		}
	}
	sort.Strings(order)
	out := make([]toolStat, 0, len(order))
	for _, n := range order {
		a := byName[n]
		var parts []string
		parts = append(parts, fmt.Sprintf("%dx", a.n))
		if a.done > 0 {
			parts = append(parts, fmt.Sprintf("%d done", a.done))
		}
		if a.run > 0 {
			parts = append(parts, fmt.Sprintf("%d running", a.run))
		}
		if a.err > 0 {
			parts = append(parts, fmt.Sprintf("%d error", a.err))
		}
		d := strings.Join(parts, " · ")
		if a.lastArgs != "" {
			d += " — " + shortDesc(a.lastArgs, 32)
		}
		out = append(out, toolStat{name: n, desc: d})
	}
	return out
}

// FillCommand closes every dialog and stages a slash command in the input
// (palette parity: user reviews, then Enter sends).
func (m *Model) FillCommand(name string) {
	m.Dialogs = nil
	m.ta.SetValue("/" + name + " ")
	m.refreshCmds()
	m.refreshAt()
	m.Refresh()
}

// isFilterKind reports pickers whose typing filters the list (generic
// updateDialog path).
func isFilterKind(kind string) bool {
	switch kind {
	case "model", "thinking", "sessions", "login", "logout":
		return true
	}
	return false
}

// updatePconfigDialog navigates the two-pane hub: ↑↓ moves in the focused
// pane (moving sections reloads the right pane), ←/→/Tab switches pane,
// typing filters the right pane, Enter on the left focuses the right,
// Enter on the right runs (confirm lives in src/builtin).
func (m Model) updatePconfigDialog(km tea.KeyMsg, d *Dialog) (tea.Model, tea.Cmd) {
	switch km.Type {
	case tea.KeyUp, tea.KeyDown:
		down := km.Type == tea.KeyDown
		if d.ProvFocus {
			if n := len(d.Provs); n > 0 {
				if down {
					d.ProvCursor = (d.ProvCursor + 1) % n
				} else {
					d.ProvCursor = (d.ProvCursor - 1 + n) % n
				}
				m.LoadPsecRows(d)
			}
		} else if n := len(d.FIdx); n > 0 {
			if down {
				d.Cursor = (d.Cursor + 1) % n
			} else {
				d.Cursor = (d.Cursor - 1 + n) % n
			}
		}
		return m, nil
	case tea.KeyLeft:
		d.ProvFocus = true
		return m, nil
	case tea.KeyRight:
		d.ProvFocus = false
		return m, nil
	case tea.KeyTab:
		d.ProvFocus = !d.ProvFocus
		return m, nil
	case tea.KeyBackspace:
		if d.Filter != "" {
			d.Filter = d.Filter[:len(d.Filter)-1]
			d.Reindex()
		}
		return m, nil
	case tea.KeyEsc:
		m.Dialogs = m.Dialogs[1:]
		m.Refresh()
		return m, nil
	case tea.KeyEnter:
		if d.ProvFocus {
			d.ProvFocus = false
			return m, nil
		}
		return m.confirmDialog(d)
	}
	if km.Type == tea.KeyRunes {
		d.Filter += km.String()
		d.Reindex()
		return m, nil
	}
	return m, nil
}

// renderPconfigDialog draws the two-pane hub: left = sections with counts,
// right = the selected section's rows. Layout math mirrors the /model
// picker (fixed scroll windows so the box never resizes while scrolling).
func (m Model) renderPconfigDialog(d *Dialog) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(d.Title) + "\n")
	if d.Message != "" {
		b.WriteString(statusBarStyle.Render(d.Message) + "\n")
	}
	b.WriteString(statusBarStyle.Render("filter: "+d.Filter+"▌") + "\n")
	b.WriteString("\n")

	boxW := m.winW - 10
	if boxW < 70 {
		boxW = 70
	}
	if boxW > 150 {
		boxW = 150
	}
	leftW := 30
	if boxW < 100 {
		leftW = 24
	}
	rightW := boxW - 8 - leftW - 3
	if rightW < 30 {
		rightW = 30
	}
	win := m.winH - 14
	if win < 12 {
		win = 12
	}
	if win > 24 {
		win = 24
	}

	// left window (sections): label + count badge.
	ptotal := len(d.Provs)
	pstart, pend, pAbove, pBelow := fixedWin(d.ProvCursor, ptotal, win)
	var leftLines []string
	if pAbove {
		leftLines = append(leftLines, "  "+toolStyle.Width(leftW-2).Render(fmt.Sprintf("…(+%d above)", pstart)))
	}
	for pi := pstart; pi < pend; pi++ {
		cw := leftW - 2
		nm := Short(d.Provs[pi], cw-5)
		cnt := ""
		if pi < len(d.PsecIDs) {
			if n := psecCount(&m, d.PsecIDs[pi]); n >= 0 {
				cnt = fmt.Sprintf("%d", n)
			}
		}
		pad := cw - 2 - lipgloss.Width(nm) - len(cnt)
		if pad < 1 {
			pad = 1
		}
		content := Fit(nm+strings.Repeat(" ", pad)+cnt, cw-2)
		mark := "  "
		style := statusBarStyle
		if pi == d.ProvCursor {
			mark = "▸ "
			if d.ProvFocus {
				style = rowHiStyle
			} else {
				style = lipgloss.NewStyle().Foreground(cText)
			}
		}
		leftLines = append(leftLines, mark+style.Width(leftW-2).Render(content))
	}
	if pBelow {
		leftLines = append(leftLines, "  "+toolStyle.Width(leftW-2).Render(fmt.Sprintf("…(+%d below)", ptotal-pend)))
	}
	for len(leftLines) < win {
		leftLines = append(leftLines, "  "+statusBarStyle.Width(leftW-2).Render(""))
	}

	// right window (section rows): same fixed-win rule as the left pane.
	total := len(d.FIdx)
	start, end, rAbove, rBelow := fixedWin(d.Cursor, total, win)
	var rightLines []string
	if rAbove {
		rightLines = append(rightLines, "  "+toolStyle.Width(rightW-2).Render(fmt.Sprintf("…(+%d above)", start)))
	}
	optW := 28
	if rightW-10 < optW {
		optW = rightW - 10
	}
	if optW < 10 {
		optW = 10
	}
	for fi := start; fi < end; fi++ {
		ri := d.FIdx[fi]
		mark := "  "
		style := statusBarStyle
		if fi == d.Cursor {
			mark = "▸ "
			if d.ProvFocus {
				style = lipgloss.NewStyle().Foreground(cText)
			} else {
				style = rowHiStyle
			}
		}
		row := Short(d.Options[ri], optW)
		if desc := DescOf(d, ri); desc != "" {
			row += "  " + toolStyle.Render("— "+Short(desc, rightW-4-optW-3))
		}
		rightLines = append(rightLines, mark+style.Width(rightW-2).Render(row))
	}
	if rBelow {
		rightLines = append(rightLines, "  "+toolStyle.Width(rightW-2).Render(fmt.Sprintf("…(+%d below)", total-end)))
	}
	if total == 0 {
		rightLines = append(rightLines, "  "+toolStyle.Width(rightW-2).Render("— no match —"))
	}
	for len(rightLines) < win {
		rightLines = append(rightLines, "  "+statusBarStyle.Width(rightW-2).Render(""))
	}

	secName := d.CurPsec()
	if d.ProvCursor >= 0 && d.ProvCursor < len(d.Provs) {
		secName = d.Provs[d.ProvCursor]
	}
	b.WriteString("  " + sideTitleStyle.Width(leftW-2).Render("SECTIONS") + " │ " +
		"  " + sideTitleStyle.Width(rightW-2).Render(strings.ToUpper(secName)+" · "+fmt.Sprintf("%d", total)) + "\n")

	n := len(leftLines)
	if len(rightLines) > n {
		n = len(rightLines)
	}
	sep := sepStyle.Render("│")
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(leftLines) {
			l = leftLines[i]
		} else {
			l = "  " + statusBarStyle.Width(leftW-2).Render("")
		}
		if i < len(rightLines) {
			r = rightLines[i]
		} else {
			r = "  " + statusBarStyle.Width(rightW-2).Render("")
		}
		b.WriteString(l + " " + sep + " " + r + "\n")
	}

	foot := "↑↓ sections · → contents · Enter open · Esc close"
	if !d.ProvFocus {
		foot = "↑↓ select · ← sections · Tab switch · type filters · Enter run · Esc close"
	}
	b.WriteString("\n" + toolStyle.Render(foot))
	box := dlgStyle.Width(boxW).Render(b.String())
	hint := ""
	if len(m.Dialogs) > 1 {
		hint = statusBarStyle.Render(fmt.Sprintf("(%d more dialogs pending)", len(m.Dialogs)-1))
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center, box),
		hint,
	)
}
