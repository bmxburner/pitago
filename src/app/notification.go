package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxNotificationHistory = 200

// OpenNotifications opens the in-memory notification log newest-first. The
// optional argument pre-fills the generic picker filter.
func (m *Model) OpenNotifications(arg string) {
	d := &Dialog{
		Kind:  "notification",
		Title: "Notification history",
	}
	for i := len(m.notificationHistory) - 1; i >= 0; i-- {
		t := m.notificationHistory[i]
		kind := "INFO"
		if t.Err {
			kind = "ERROR"
		}
		// The title pi sent on notify is part of the text the user came
		// for: the popup spends a row on it, so leaving it out here would
		// make the heading the one piece of a notification that survives in
		// no other view — not the detail pane, not Ctrl+Y.
		body := t.Text
		if title := strings.TrimSpace(t.Title); title != "" {
			body = title + "\n" + t.Text
		}
		d.Options = append(d.Options, Short(strings.TrimSpace(body), 180))
		d.Descs = append(d.Descs, t.At.Format("01-02 15:04:05")+" · "+kind)
		// Payload is what Ctrl+Y copies. Options truncates to 180 cells for
		// the row, so it cannot double as the copy source: an error toast
		// routinely runs longer than that. It is also what the detail pane
		// renders — the row is a fitted summary, this is the whole text.
		d.Payload = append(d.Payload, body)
	}
	d.Filter = strings.TrimSpace(arg)
	d.Reindex()
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
}

// notificationHistoryOpen reports whether a notification history dialog is
// on the stack. It gates the toast freeze: a frozen popup is held for a
// reader, and with no reader there is nothing to hold it for.
func (m *Model) notificationHistoryOpen() bool {
	for _, d := range m.Dialogs {
		if d.Kind == "notification" {
			return true
		}
	}
	return false
}

// OpenNotificationsFromToast is the Ctrl+X path: the history with the newest
// entry selected, and that entry's popup frozen.
//
// The freeze is the reason this is not just OpenNotifications: the user
// reaches for the text because the corner popup was unreadable or already
// gone, and a countdown that keeps ticking under the reader is the same
// problem one level down. The popup is addressed by its Toast.ID, so an
// entry that has already expired is simply not frozen — the pane still
// shows it, because the history outlives the popup.
func (m *Model) OpenNotificationsFromToast() {
	m.OpenNotifications("")
	if n := len(m.notificationHistory); n > 0 {
		m.freezeToast(m.notificationHistory[n-1].ID)
		m.Refresh()
	}
}

// notificationGeom derives the box and list geometry from the terminal size.
//
// dlgStyle is Border + Padding(1,3) and lipgloss Width() covers the padding
// but not the border, so the box costs:
//   - width:  boxW + 2 (border); content width cw = boxW - 6 (padding 3+3)
//   - height: lines + 4 (border 2 + vertical padding 2)
//
// Width: boxW is clamped into [40,120] and then into winW-2, so the box plus
// border never exceeds the terminal (a 40-col screen gets a 38-col box
// instead of the old 70-col floor that rendered 72 wide). Every emitted line
// is fitted to cw, so lipgloss never wraps and the height stays predictable.
//
// Height budget (box must fit placeH = winH-2). The list and the detail pane
// share it, which is the whole point: the row is a fitted summary, and the
// pane below it is where a long error is actually readable.
//
//	placeH = winH - 2
//	frame  = 4   (border 2 + dlgStyle vertical padding 2)
//	chrome = 4   (title + meta + filter + footer)
//	blanks = 2   (after filter, before footer; dropped on short terminals)
//	rest   = placeH - frame - chrome - blanks
//	sep    = 1 when rest >= 3 (divider above the pane; never on a tiny box)
//	detH   = clamp(rest-sep)/2, 1..6 (the pane)
//	win    = rest - sep - detH, capped at 20 (list rows)
//
// The pane yields to the list: detH never takes more than half of what is
// left, and win is floored at 1, so a 40x12 terminal still shows a row and
// a line of its text. fixedWin spends at most two of the win rows on the
// "…(+N above/below)" markers, and the list region is padded to exactly win
// rows, so the box height is constant regardless of match count or cursor
// position.
func notificationGeom(winW, winH int) (boxW, cw, rowW, win, blanks, sep, detH int) {
	boxW = winW - 10
	if boxW > 120 {
		boxW = 120
	}
	if boxW < 40 {
		boxW = 40
	}
	if boxW > winW-2 { // border adds 2: never overflow a narrow terminal
		boxW = winW - 2
	}
	if boxW < 12 {
		boxW = 12
	}
	cw = boxW - 6
	if cw < 8 {
		cw = 8
	}
	rowW = cw - 2
	if rowW < 6 {
		rowW = 6
	}
	placeH := winH - 2
	blanks = 2
	if placeH < 14 {
		blanks = 1
	}
	if placeH < 11 {
		blanks = 0
	}
	rest := placeH - 4 - 4 - blanks
	if rest < 1 {
		rest = 1
	}
	if rest >= 3 {
		sep = 1
	}
	if rest-sep >= 2 {
		detH = (rest - sep) / 2
		if detH > 6 { // a 6-row pane at ~98 cells already shows plenty
			detH = 6
		}
	}
	win = rest - sep - detH
	if win < 1 {
		win = 1
	}
	if win > 20 { // same list cap /trajectory and the generic picker use
		win = 20
	}
	return boxW, cw, rowW, win, blanks, sep, detH
}

// notificationWindow bounds rendered list rows independently of retained items.
func notificationWindow(winH int) int {
	_, _, _, win, _, _, _ := notificationGeom(100, winH)
	return win
}

// notificationRow is one pre-fitted plain row. Segments are styled only
// after fitting, so no ANSI sequence is ever truncated or wrapped.
type notificationRow struct {
	marker string // "● INFO" / "× ERROR"
	err    bool
	body   string // fitted to the body budget
	desc   string // timestamp + kind
	gap    int    // cells between marker/body/desc (2, or 1 when narrow)
}

// plain composes the already-fitted segments; with the caller's 2-cell
// indent the result is exactly rowW. The selected row is fitted as one plain
// string so no ANSI sequence is ever cut in half.
func (r notificationRow) plain() string {
	return r.marker + strings.Repeat(" ", r.gap) + r.body +
		strings.Repeat(" ", r.gap) + r.desc
}

// buildNotificationRows fits every row to exactly rowW cells:
// 2 (indent/cursor) + markerW + bodyW + 2 (gap) + descW == rowW.
func buildNotificationRows(d *Dialog, fidx []int, rowW int) []notificationRow {
	rows := make([]notificationRow, 0, len(fidx))
	for _, ri := range fidx {
		if ri < 0 || ri >= len(d.Options) {
			continue
		}
		desc := DescOf(d, ri)
		row := notificationRow{marker: "● INFO", desc: desc, err: strings.Contains(desc, "ERROR")}
		if row.err {
			row.marker = "× ERROR"
		}
		markerW := lipgloss.Width(row.marker)
		descW := lipgloss.Width(row.desc)
		// Reserve the 2-cell indent, the marker, the desc and both gaps
		// before the body; the remainder is the body budget. A gap drops
		// to 1 before the body does, so a narrow terminal shrinks text
		// instead of overflowing the box.
		row.gap = 2
		if rowW-markerW-descW-2*row.gap-2 < 1 {
			row.gap = 1
		}
		bodyW := rowW - markerW - descW - 2*row.gap - 2
		if bodyW < 1 {
			bodyW = 1
		}
		row.marker = Fit(row.marker, markerW)
		row.desc = Fit(row.desc, descW)
		row.body = Fit(d.Options[ri], bodyW)
		rows = append(rows, row)
	}
	return rows
}

// renderNotificationDialog draws a fixed-height list over a fixed-height
// detail pane. Timestamps and explicit INFO/ERROR labels remain readable
// without relying on color alone; the pane below carries the selected row's
// untruncated text, which is what the row had to cut.
func (m Model) renderNotificationDialog(d *Dialog) string {
	boxW, cw, rowW, win, blanks, sep, detH := notificationGeom(m.winW, m.winH)
	rows := buildNotificationRows(d, d.FIdx, rowW)
	var b strings.Builder

	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(Fit(d.Title, cw)) + "\n")
	// The counter describes the snapshot this dialog was built from
	// (d.Options), not the live history: notifications arriving while the
	// window is open must not change the header under the user.
	b.WriteString(statusBarStyle.Render(Fit(fmt.Sprintf("Latest %d of %d · RAM only",
		min(len(d.Options), maxNotificationHistory), len(d.Options)), cw)) + "\n")
	b.WriteString(statusBarStyle.Render(Fit("filter: "+d.Filter+"▌", cw)) + "\n")
	if blanks > 0 {
		b.WriteString("\n")
	}

	// List region: markers + rows, padded to exactly win rows.
	var list []string
	start, end, above, below := fixedWin(d.Cursor, len(rows), win)
	if above {
		list = append(list, "  "+toolStyle.Render(Fit(fmt.Sprintf("…(+%d above)", start), rowW)))
	}
	if len(rows) == 0 {
		empty := "— no notifications —"
		if d.Filter != "" {
			empty = "— no matching notifications —"
		}
		list = append(list, "  "+toolStyle.Render(Fit(empty, rowW)))
	}
	for fi := start; fi < end; fi++ {
		r := rows[fi]
		if fi == d.Cursor {
			// Selected: highlight the whole row; the marker word keeps the
			// info/error distinction readable without inner colors (an inner
			// reset would kill the highlight background past the token).
			list = append(list, "▸ "+rowHiStyle.Width(rowW).Render(Fit(r.plain(), rowW)))
		} else {
			st := okStyle
			if r.err {
				st = errStyle
			}
			gap := strings.Repeat(" ", r.gap)
			list = append(list, "  "+st.Render(r.marker)+gap+statusBarStyle.Render(r.body)+toolStyle.Render(gap+r.desc))
		}
	}
	if below {
		list = append(list, "  "+toolStyle.Render(Fit(fmt.Sprintf("…(+%d below)", len(rows)-end), rowW)))
	}
	for len(list) > win { // defensive: never exceed the list budget
		list = list[:len(list)-1]
	}
	for len(list) < win {
		list = append(list, strings.Repeat(" ", rowW))
	}
	for _, ln := range list {
		b.WriteString(ln + "\n")
	}
	b.WriteString(m.renderNotificationDetail(d, rowW, sep, detH))

	foot := "↑↓ select · PgUp/PgDn page · Ctrl+Y copy · Esc close"
	if detH > 0 {
		// Only advertise the pane keys when there is a pane to drive: at
		// winH <= 11 the geometry leaves detH at 0, and ←/→ would then move
		// an offset nothing draws.
		foot = "↑↓ select · ←/→ read · PgUp/PgDn page · Ctrl+Y copy · Esc close"
	}
	if len(m.Dialogs) > 1 {
		foot += fmt.Sprintf(" · (%d pending)", len(m.Dialogs)-1)
	}
	if blanks > 1 {
		b.WriteString("\n")
	}
	b.WriteString(toolStyle.Render(Fit(m.dialogFoot(foot), cw)))
	box := dlgStyle.Width(boxW).Render(b.String())
	return lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center, box)
}

// renderNotificationDetail writes the pane below the list: the selected
// row's full text, wrapped, scrolled by d.TrajOff.
//
// Wrapping is the point. A list row is fitted to rowW and therefore cut —
// an error longer than the column could only leave this app via Ctrl+Y, to
// the clipboard. Here it is readable, and the blank lines and indentation
// an extension sent survive, which is what a stack trace needs.
//
// The pane is always exactly detH rows (separator included, when the
// terminal affords one), so the box height stays independent of text length.
func (m Model) renderNotificationDetail(d *Dialog, rowW, sep, detH int) string {
	var body []string
	label := "— no notification selected —"
	if text := notificationSelected(d); strings.TrimSpace(text) != "" {
		lines := toastWrap(text, rowW)
		off := trajScroll(d.TrajOff, len(lines), detH)
		// Position rides the separator line, where it costs no pane row.
		label = fmt.Sprintf("detail · line %d/%d · ←/→", off+1, len(lines))
		for i := off; i < off+detH && i < len(lines); i++ {
			body = append(body, "  "+statusBarStyle.Render(Fit(lines[i], rowW)))
		}
	} else {
		body = append(body, "  "+toolStyle.Render(Fit(label, rowW)))
		label = "detail"
	}
	for len(body) > detH {
		body = body[:len(body)-1]
	}
	for len(body) < detH {
		body = append(body, strings.Repeat(" ", rowW))
	}

	var b strings.Builder
	if sep > 0 {
		b.WriteString("  " + toolStyle.Render(Fit(label, rowW)) + "\n")
	}
	for _, ln := range body {
		b.WriteString(ln + "\n")
	}
	return b.String()
}

// notificationSelected is the selected row's full, untruncated text — the
// same resolver Ctrl+Y copies, so the pane can never show something
// different from what lands on the clipboard.
func notificationSelected(d *Dialog) string { return trajSelected(d) }

// updateNotificationDetail scrolls the pane. ←/→ step a line, Home/End jump
// to the ends, and PgUp/PgDn keep their existing job of paging the list, so
// nothing that already worked changed meaning.
//
// detH == 0 means this terminal has no pane (winH <= 11), and every key is
// refused rather than writing an offset nothing renders — which would
// otherwise resurface as a pane scrolled to the middle the moment the
// terminal grew a row.
func (m Model) updateNotificationDetail(km tea.KeyMsg, d *Dialog) {
	_, _, rowW, _, _, _, detH := notificationGeom(m.winW, m.winH)
	if detH == 0 {
		return
	}
	total := len(toastWrap(notificationSelected(d), rowW))
	var off int
	switch km.Type {
	case tea.KeyLeft:
		off = d.TrajOff - 1
	case tea.KeyRight:
		off = d.TrajOff + 1
	case tea.KeyHome:
		off = 0
	case tea.KeyEnd:
		off = total
	default:
		return
	}
	if off == d.TrajOff {
		return // at an end: consume the key, move nothing
	}
	d.TrajOff = trajScroll(off, total, detH)
	// No Refresh: this receiver is a value, so the pointer call would land
	// on a copy the caller throws away. Bubbletea repaints after every
	// Update, and the caller returns the mutated model.
}

// toastWrap folds text into rows of at most w cells, preserving its blank
// lines and per-line indentation — a stack trace or a multi-frame extension
// message reads as written. Unlike wrapWords it does not collapse
// whitespace, so structure survives the trip to the box.
func toastWrap(text string, w int) []string {
	if w < 8 {
		w = 8
	}
	wrap := lipgloss.NewStyle().Width(w)
	var out []string
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) == "" {
			out = append(out, "")
			continue
		}
		for _, row := range strings.Split(wrap.Render(ln), "\n") {
			out = append(out, strings.TrimRight(row, " "))
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}
