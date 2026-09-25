package app

// Centered /subagents management overlay (Phase 2). Native pitago dialog
// (Kind "subagent-herd") over the Phase-1 data layer (m.Subagents): list with
// active/finished scopes + type-to-filter, detail pane with activity header
// + session transcript tail, and model-mediated manage actions (the overlay
// composes prompts that invoke the model's existing subagent-family tools —
// no pi-RPC protocol changes).
//
// Keymap follows /sessions + /pitago-setting: ↑↓ select, Tab scope,
// type-to-filter, Enter/→ open, Esc/← back. Action letters dispatch on the
// focused row: x stop · w wait · R resume · X dismiss · f surface · s steer.

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// subagentsScopeActive / Finished are the Dialog.Scope values (Tab toggles).
const (
	subagentsScopeActive   = "active"
	subagentsScopeFinished = "finished"
)

// DETAIL_SIZES parity with pi-agents/footer.ts: overlay width presets cycled
// with ^O in the detail pane.
var subagentsDetailSizes = []struct{ pad, maxH int }{
	{pad: 10, maxH: 0}, // adaptive (renderDialog default: winW-10, 62..100)
	{pad: 6, maxH: 6},  // wide
	{pad: 2, maxH: 2},  // near-fullscreen
}

// subagentRowByID finds a live row by its dialog payload ID.
func subagentRowByID(m Model, id string) (SubagentRow, bool) {
	for _, r := range m.Subagents {
		if r.ID == id {
			return r, true
		}
	}
	return SubagentRow{}, false
}

// subagentsInScope splits rows into the Tab scopes: active holds
// starting/active/waiting, finished holds done/stalled/error.
func subagentsInScope(rows []SubagentRow, scope string) []SubagentRow {
	var out []SubagentRow
	for _, r := range rows {
		finished := r.Status == SubagentDone || r.Status == SubagentStalled || r.Status == SubagentError
		if scope == subagentsScopeFinished && finished {
			out = append(out, r)
		} else if scope != subagentsScopeFinished && !finished {
			out = append(out, r)
		}
	}
	return out
}

// subagentsRowDesc is the right-hand description per row.
func subagentsRowDesc(r SubagentRow) string {
	end := r.StartedAt
	done := false
	if r.DoneAt != nil {
		end = *r.DoneAt
		done = true
	}
	el := formatSubagentElapsed(end.Sub(r.StartedAt).Milliseconds())
	bits := []string{}
	if r.StatusLabel != "" {
		bits = append(bits, r.StatusLabel)
	} else {
		bits = append(bits, string(r.Status))
	}
	bits = append(bits, el)
	if r.AgentName != "" {
		bits = append(bits, r.AgentName)
	}
	if done && r.Result != "" {
		bits = append(bits, firstLine(r.Result, 60))
	} else if r.Task != "" {
		bits = append(bits, firstLine(r.Task, 60))
	}
	return strings.Join(bits, " · ")
}

// rebuildSubagentsOptions re-derives the dialog list from m.Subagents,
// preserving the focused row by ID across live reordering.
func rebuildSubagentsOptions(m Model, d *Dialog) {
	sel := ""
	if len(d.Payload) > 0 && d.Cursor >= 0 && d.Cursor < len(d.FIdx) {
		if ri := d.FIdx[d.Cursor]; ri >= 0 && ri < len(d.Payload) {
			sel = d.Payload[ri]
		}
	}
	rows := subagentsInScope(m.Subagents, d.Scope)
	d.Options = make([]string, 0, len(rows))
	d.Descs = make([]string, 0, len(rows))
	d.Payload = make([]string, 0, len(rows))
	for _, r := range rows {
		d.Options = append(d.Options, r.Name)
		d.Descs = append(d.Descs, subagentsRowDesc(r))
		d.Payload = append(d.Payload, r.ID)
	}
	d.Reindex()
	if sel != "" {
		for i, fi := range d.FIdx {
			if d.Payload[fi] == sel {
				d.Cursor = i
				break
			}
		}
	}
	if d.Cursor >= len(d.FIdx) {
		d.Cursor = len(d.FIdx) - 1
	}
	if d.Cursor < 0 {
		d.Cursor = 0
	}
}

// OpenSubagentHerd opens the centered management overlay. It is the live
// view of running subagents, kept separate from OpenSubagents (the
// ext-backed picker that selects the current subagent): that one owns the
// "/subagents" command and dialog kind "subagents", so the herd takes its
// own kind and its own key routing rather than shadowing the picker.
func (m *Model) OpenSubagentHerd() {
	m.refreshSubagents(false)
	d := &Dialog{
		Kind:  "subagent-herd",
		Title: "Subagents",
		Scope: subagentsScopeActive,
	}
	m.Dialogs = append([]*Dialog{d}, m.Dialogs...)
	rebuildSubagentsOptions(*m, d)
	m.applyPopupH()
	m.Refresh()
}

// openSubagentsDetail flips the dialog into detail mode for one row:
// activity header + cached transcript lines for scrolling.
func openSubagentsDetail(m Model, d *Dialog, row SubagentRow) {
	d.SubDetail = true
	d.SubID = row.ID
	d.SubOffset = 0
	end := row.StartedAt
	if row.DoneAt != nil {
		end = *row.DoneAt
	}
	head := []string{string(row.Status)}
	if row.StatusLabel != "" {
		head = []string{row.StatusLabel}
	}
	head = append(head, formatSubagentElapsed(end.Sub(row.StartedAt).Milliseconds()))
	if row.AgentName != "" {
		head = append(head, row.AgentName)
	}
	if row.Surface != "" {
		head = append(head, row.Surface)
	}
	d.SubHeader = strings.Join(head, " · ")
	var lines []string
	if row.Task != "" {
		lines = append(lines, "task: "+row.Task)
	}
	if row.Result != "" {
		lines = append(lines, "result: "+firstLine(row.Result, 300))
	}
	if row.SessionFile != "" {
		if tail := readSubagentTail(row.SessionFile, subagentTailBytes); strings.TrimSpace(tail) != "" {
			raw := strings.Split(tail, "\n")
			if len(raw) > 150 {
				raw = raw[len(raw)-150:]
			}
			lines = append(lines, "─ transcript ─")
			lines = append(lines, raw...)
		}
	}
	if len(lines) == 0 {
		lines = []string{"(no transcript yet)"}
	}
	d.SubLines = lines
	m.applyPopupH()
}

// subagentActionPrompt composes the model-mediated prompt per action letter.
func subagentActionPrompt(action string, row SubagentRow) (string, bool) {
	ref := fmt.Sprintf("\"%s\" (%s)", row.Name, row.ID)
	switch action {
	case "x":
		return fmt.Sprintf("Use the subagent_interrupt tool on subagent %s now.", ref), true
	case "w":
		return fmt.Sprintf("Use the subagent_wait tool on subagent %s and report its result.", ref), true
	case "R":
		return fmt.Sprintf("Use the subagent_resume tool on subagent %s and continue its task.", ref), true
	case "s":
		return "", false // steer opens the nested input dialog instead
	}
	return "", false
}

// runSubagentAction closes the overlay and sends the action prompt (or opens
// the nested steer input for "s"). Surface ("f") and dismiss ("X") are
// local-only: Orca pane focus can't cross processes, and pi-agents
// auto-dismisses finished rows on the next user turn anyway.
func (m Model) runSubagentAction(d *Dialog, row SubagentRow, action string) (tea.Model, tea.Cmd) {
	switch action {
	case "f":
		surface := row.Surface
		if surface == "" {
			surface = "in-process (no Orca pane)"
		}
		m.Dialogs = m.Dialogs[1:]
		m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("%s → %s", row.Name, surface)})
		m.Refresh()
		return m, m.ReconcileTurnCmd()
	case "X":
		m.Dialogs = m.Dialogs[1:]
		m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("%s dismissed here; pi-agents clears finished rows on your next message", row.Name)})
		m.Refresh()
		return m, m.ReconcileTurnCmd()
	case "s":
		sd := &Dialog{
			Kind:   "subagents-steer",
			Title:  fmt.Sprintf("Steer %s", row.Name),
			SubID:  row.ID,
			Filter: "",
		}
		m.Dialogs = append([]*Dialog{sd}, m.Dialogs...)
		m.applyPopupH()
		m.Refresh()
		return m, nil
	default:
		text, ok := subagentActionPrompt(action, row)
		if !ok {
			return m, nil
		}
		m.Dialogs = m.Dialogs[1:]
		m.Refresh()
		return m, m.sendCmd(m.thinking, text, nil)
	}
}

// focusedSubagentRow resolves the dialog cursor to a live row.
func focusedSubagentRow(m Model, d *Dialog) (SubagentRow, bool) {
	if len(d.FIdx) == 0 || d.Cursor < 0 || d.Cursor >= len(d.FIdx) {
		return SubagentRow{}, false
	}
	ri := d.FIdx[d.Cursor]
	if ri < 0 || ri >= len(d.Payload) {
		return SubagentRow{}, false
	}
	return subagentRowByID(m, d.Payload[ri])
}

// updateSubagentsDialog navigates the overlay (list + detail) and dispatches
// row actions. List mode refreshes its options from live state on every key
// so spawns completing behind the dialog appear without reopening.
func (m Model) updateSubagentsDialog(km tea.KeyMsg, d *Dialog) (tea.Model, tea.Cmd) {
	// Nested steer input: typing + submit/cancel only.
	if d.Kind == "subagents-steer" {
		switch km.Type {
		case tea.KeyEsc:
			m.Dialogs = m.Dialogs[1:]
			m.applyPopupH()
			m.Refresh()
			return m, nil
		case tea.KeyEnter:
			text := strings.TrimSpace(d.Filter)
			id := d.SubID
			m.Dialogs = m.Dialogs[1:]
			if text == "" {
				m.Refresh()
				return m, nil
			}
			row, ok := subagentRowByID(m, id)
			name := id
			if ok {
				name = fmt.Sprintf("\"%s\" (%s)", row.Name, row.ID)
			}
			m.Refresh()
			return m, m.sendCmd(m.thinking, fmt.Sprintf("Send the following message to subagent %s via your subagent message path:\n\n%s", name, text), nil)
		case tea.KeyBackspace:
			if d.Filter != "" {
				r := []rune(d.Filter)
				d.Filter = string(r[:len(r)-1])
				m.applyPopupH()
			}
			return m, nil
		case tea.KeyRunes:
			d.Filter += string(km.Runes)
			m.applyPopupH()
			return m, nil
		case tea.KeySpace:
			d.Filter += " "
			m.applyPopupH()
			return m, nil
		}
		if km.Type == tea.KeyCtrlV {
			return m, m.pasteCmd(true)
		}
		return m, nil
	}
	// Detail mode: scroll + size + actions, no filter.
	if d.SubDetail {
		row, ok := subagentRowByID(m, d.SubID)
		switch km.Type {
		case tea.KeyEsc:
			d.SubDetail = false
			d.SubID = ""
			rebuildSubagentsOptions(m, d)
			m.applyPopupH()
			m.Refresh()
			return m, nil
		case tea.KeyUp:
			d.SubOffset += 5
			return m, nil
		case tea.KeyDown:
			if d.SubOffset > 0 {
				d.SubOffset -= 5
				if d.SubOffset < 0 {
					d.SubOffset = 0
				}
			}
			return m, nil
		case tea.KeyPgUp:
			d.SubOffset += 20
			return m, nil
		case tea.KeyPgDown:
			if d.SubOffset > 0 {
				d.SubOffset -= 20
				if d.SubOffset < 0 {
					d.SubOffset = 0
				}
			}
			return m, nil
		case tea.KeyCtrlO:
			d.SubSize = (d.SubSize + 1) % len(subagentsDetailSizes)
			m.applyPopupH()
			m.Refresh()
			return m, nil
		case tea.KeyRunes:
			if !ok {
				return m, nil
			}
			switch string(km.Runes) {
			case "x", "w", "R", "X", "f", "s":
				return m.runSubagentAction(d, row, string(km.Runes))
			}
			return m, nil
		}
		return m, nil
	}
	// List mode.
	rebuildSubagentsOptions(m, d)
	n := len(d.FIdx)
	switch km.Type {
	case tea.KeyUp:
		if n > 0 {
			if d.Cursor > 0 {
				d.Cursor--
			} else {
				d.Cursor = n - 1
			}
		}
		return m, nil
	case tea.KeyDown:
		if n > 0 {
			if d.Cursor < n-1 {
				d.Cursor++
			} else {
				d.Cursor = 0
			}
		}
		return m, nil
	case tea.KeyBackspace:
		if d.Filter != "" {
			r := []rune(d.Filter)
			d.Filter = string(r[:len(r)-1])
			d.Reindex()
			m.applyPopupH()
		}
		return m, nil
	case tea.KeyTab:
		if d.Scope == subagentsScopeActive {
			d.Scope = subagentsScopeFinished
		} else {
			d.Scope = subagentsScopeActive
		}
		d.Cursor = 0
		rebuildSubagentsOptions(m, d)
		m.applyPopupH()
		m.Refresh()
		return m, nil
	case tea.KeyEsc:
		m.Dialogs = m.Dialogs[1:]
		m.refreshPiTasks()
		m.refreshSubagents(false)
		m.Refresh()
		return m, m.ReconcileTurnCmd()
	case tea.KeyEnter:
		if row, ok := focusedSubagentRow(m, d); ok {
			openSubagentsDetail(m, d, row)
			m.Refresh()
		}
		return m, nil
	case tea.KeyRunes:
		// List mode is navigation + filter only (pitago-native): letter
		// actions live in detail mode, where typing can't collide with
		// filter text (names like "explore" contain action letters).
		d.Filter += string(km.Runes)
		d.Reindex()
		m.applyPopupH()
		return m, nil
	case tea.KeySpace:
		d.Filter += " "
		d.Reindex()
		m.applyPopupH()
		return m, nil
	}
	return m, nil
}

// renderSubagentsDialog draws the overlay: list mode (rows + scope footer)
// or detail mode (activity header + scrollable transcript).
func (m Model) renderSubagentsDialog(d *Dialog) string {
	// Nested steer input: compact prompt box.
	if d.Kind == "subagents-steer" {
		var b strings.Builder
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(d.Title) + "\n")
		b.WriteString(statusBarStyle.Render("message to send (empty cancels)") + "\n\n")
		b.WriteString(cmdHiStyle.Render(d.Filter+"▌") + "\n")
		b.WriteString("\n" + toolStyle.Render("Enter send · Esc cancel"))
		box := dlgStyle.Width(60).Render(b.String())
		hint := ""
		if len(m.Dialogs) > 1 {
			hint = statusBarStyle.Render(fmt.Sprintf("(%d more dialogs pending)", len(m.Dialogs)-1))
		}
		return lipgloss.JoinVertical(lipgloss.Center,
			lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center, box),
			hint,
		)
	}
	size := subagentsDetailSizes[0]
	if d.SubSize >= 0 && d.SubSize < len(subagentsDetailSizes) {
		size = subagentsDetailSizes[d.SubSize]
	}
	boxW := m.winW - size.pad
	if boxW < 62 {
		boxW = 62
	}
	if d.SubSize == 0 && boxW > 100 {
		boxW = 100
	}
	var b strings.Builder
	if d.SubDetail {
		row, _ := subagentRowByID(m, d.SubID)
		title := row.Name
		if title == "" {
			title = "Subagent"
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(title) + "\n")
		if d.SubHeader != "" {
			b.WriteString(statusBarStyle.Render(d.SubHeader) + "\n")
		}
		b.WriteString("\n")
		win := m.winH - 12 - size.maxH
		if win < 6 {
			win = 6
		}
		if win > 40 {
			win = 40
		}
		lines := d.SubLines
		total := len(lines)
		// SubOffset counts lines hidden below the window (0 = tail
		// visible); ↑/PgUp raise it toward older lines, ↓/PgDn lower it.
		maxOff := total - win
		if maxOff < 0 {
			maxOff = 0
		}
		off := d.SubOffset
		if off > maxOff {
			off = maxOff
		}
		if off < 0 {
			off = 0
		}
		start := total - win - off
		if start < 0 {
			start = 0
		}
		end := start + win
		if end > total {
			end = total
		}
		if start > 0 {
			b.WriteString(toolStyle.Render(fmt.Sprintf("…(+%d above)", start)) + "\n")
		}
		rowW := boxW - 6
		for _, ln := range lines[start:end] {
			b.WriteString(statusBarStyle.Render(Short(ln, rowW)) + "\n")
		}
		if end < total {
			b.WriteString(toolStyle.Render(fmt.Sprintf("…(+%d below)", total-end)) + "\n")
		}
		b.WriteString("\n" + toolStyle.Render("↑↓/PgUp/PgDn scroll · ^O size · x stop · w wait · R resume · s steer · Esc back"))
	} else {
		active := subagentsInScope(m.Subagents, subagentsScopeActive)
		finished := subagentsInScope(m.Subagents, subagentsScopeFinished)
		scopeName := "active"
		count := len(active)
		other := len(finished)
		if d.Scope == subagentsScopeFinished {
			scopeName = "finished"
			count = len(finished)
			other = len(active)
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(
			fmt.Sprintf("Subagents — %s (%d)", scopeName, count)) + "\n")
		if d.Filter != "" {
			b.WriteString(statusBarStyle.Render("filter: "+d.Filter+"▌") + "\n")
		} else {
			b.WriteString(statusBarStyle.Render(fmt.Sprintf("Tab: %s (%d) · type to filter", otherScopeName(d.Scope), other)) + "\n")
		}
		b.WriteString("\n")
		rowW := boxW - 10
		win := 12
		if h := m.winH - 12; h > win {
			win = h
		}
		if win > 20 {
			win = 20
		}
		total := len(d.FIdx)
		start := d.Cursor - 4
		if start < 0 {
			start = 0
		}
		if start+win > total {
			start = total - win
		}
		if start < 0 {
			start = 0
		}
		end := start + win
		if end > total {
			end = total
		}
		if start > 0 {
			b.WriteString(toolStyle.Render(fmt.Sprintf("…(+%d above)", start)) + "\n")
		}
		for fi := start; fi < end; fi++ {
			ri := d.FIdx[fi]
			cursor := "  "
			style := statusBarStyle
			if fi == d.Cursor {
				cursor = "▸ "
				style = rowHiStyle
			}
			row := Short(d.Options[ri], 44)
			if desc := DescOf(d, ri); desc != "" {
				row += "  " + toolStyle.Render("— "+Short(desc, rowW-47))
			}
			if fi == d.Cursor {
				b.WriteString(cursor + style.Width(rowW).Render(row) + "\n")
			} else {
				b.WriteString(cursor + style.Render(row) + "\n")
			}
		}
		if end < total {
			b.WriteString(toolStyle.Render(fmt.Sprintf("…(+%d below)", total-end)) + "\n")
		}
		if total == 0 {
			b.WriteString(toolStyle.Render("— no subagents —") + "\n")
		}
		b.WriteString("\n" + toolStyle.Render("↑↓ select · Enter open · Tab scope · Esc close · (x/w/R/X/f/s act inside)"))
	}
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

func otherScopeName(scope string) string {
	if scope == subagentsScopeFinished {
		return "active"
	}
	return "finished"
}
