package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// Toast is one ephemeral popup notification: transient confirmations like
// "model switched → …" or "yanked (N chars)". Toasts float top-right over
// the chat and auto-dismiss after 5s — they never enter the chat history
// (m.blocks), so switching models mid-session doesn't spam the transcript.
type Toast struct {
	ID    int    // monotonic; matches a viewer back to its live popup
	Text  string // message, may be multi-line (extension notices often are)
	Title string // optional heading above the message (extension notify title)
	Err   bool
	At    time.Time
	// FrozenAt is non-zero while a detail viewer holds this toast open. The
	// countdown pauses instead of expiring the text under the user, and
	// resumeToasts rewinds At by the frozen span so the remaining seconds
	// on close are the ones left when the viewer opened.
	FrozenAt time.Time
}

const (
	toastTTL      = 10 * time.Second // every popup auto-dismisses after 10s
	maxToasts     = 10               // cap: oldest drops when full
	maxToastLines = 3                // rows one toast may occupy before eliding
	// maxToastRows is the default cap for a roomy terminal; overlayToasts
	// lowers it to what the real layout has left (see toastStack).
	maxToastRows = 12
)

// toastTickMsg fires every second while a popup is visible (live dismiss
// countdown) and on expiry, so Update can prune + repaint even when the
// user is idle (no keystrokes to trigger a refresh).
type toastTickMsg struct{}

// Notify shows an info popup. It rides AddBlock so every existing
// "notice" call site becomes a toast with no changes there.
func (m *Model) Notify(text string) { m.AddBlock(Block{Kind: "notice", Text: text}) }

// NotifyErr shows an error popup (red, same 10s TTL).
func (m *Model) NotifyErr(text string) { m.AddBlock(Block{Kind: "notice", Text: text, Err: true}) }

// scheduleToastTick repaints 1s later: it drives the live dismiss
// countdown and prunes on expiry. Each toastTickMsg reschedules while a
// popup remains, so the loop runs only while something is visible.
func scheduleToastTick() {
	if ProgRef == nil {
		return
	}
	time.AfterFunc(time.Second, func() { ProgRef.Send(toastTickMsg{}) })
}

// pushToast appends a toast, caps the stack, and starts the tick loop.
// Expiry also runs opportunistically on every Update, so a missed tick
// still clears (no timers in tests — ProgRef is nil there).
func (m *Model) pushToast(text string, isErr bool) { m.pushToastTitled("", text, isErr) }

// pushToastTitled is pushToast with a heading. Extensions already send a
// title on notify (pi's extension_ui_request carries one); before this,
// pitago dropped it for notifications and only used it on dialogs.
func (m *Model) pushToastTitled(title, text string, isErr bool) {
	m.pruneToasts()
	now := time.Now()
	m.toastSeq++
	t := Toast{ID: m.toastSeq, Text: text, Title: title, Err: isErr, At: now}
	m.toasts = append(m.toasts, t)
	if len(m.toasts) > maxToasts {
		m.toasts = m.toasts[len(m.toasts)-maxToasts:]
	}
	m.notificationHistory = append(m.notificationHistory, t)
	if len(m.notificationHistory) > maxNotificationHistory {
		m.notificationHistory = m.notificationHistory[len(m.notificationHistory)-maxNotificationHistory:]
	}
	scheduleToastTick()
}

// pruneToasts drops expired toasts (uniform 5s TTL).
func (m *Model) pruneToasts() {
	if len(m.toasts) == 0 {
		return
	}
	now := time.Now()
	kept := m.toasts[:0]
	// A freeze is only meaningful while the history is open. dismissDialog
	// resumes on the normal Esc/Ctrl+C close, but several other paths pop
	// dialogs (answerDialog, CloseAllDialogs, the rename flow, respawn), and
	// a leaked freeze pins the popup on screen AND keeps toastTickMsg
	// rescheduling itself at 1Hz forever. So the freeze is re-derived from
	// the dialog stack here rather than trusted: any frozen toast with no
	// history open is released on the next prune.
	held := m.notificationHistoryOpen()
	for _, t := range m.toasts {
		if !t.FrozenAt.IsZero() {
			if !held {
				// Released without a rewind: the elapsed time is spent
				// either way, and At is already in the past.
				t.FrozenAt = time.Time{}
			} else {
				kept = append(kept, t)
				continue
			}
		}
		if now.Sub(t.At) < toastTTL {
			kept = append(kept, t)
		}
	}
	// ponytail: global slice reuse is fine here — toasts are UI-only,
	// single-goroutine (Bubble Tea), no shared ceiling.
	m.toasts = kept
}

// freezeToast suspends expiry for one toast while its detail viewer is open.
// No-op for an ID that is no longer live (expired, or already dropped).
func (m *Model) freezeToast(id int) {
	now := time.Now()
	for i := range m.toasts {
		if m.toasts[i].ID == id && m.toasts[i].FrozenAt.IsZero() {
			m.toasts[i].FrozenAt = now
		}
	}
}

// resumeToasts advances each frozen toast's clock by the time it spent
// frozen, so the countdown continues where it stood instead of restarting
// at a full 10s. Idempotent: a second call is a no-op.
//
// The clamp below is a guard, not the mechanism: with At in the past at
// freeze time (always true in production — a negative age cannot arise any
// more), the rewind lands at or before now by arithmetic alone. It stays
// because the invariant it protects is cheap and the failure it prevents is
// silent: an At in the future would render as a negative age, and a
// negative age is always under the TTL, so the toast would both display a
// nonsense countdown ("· 40s") and never expire.
//
// Note the real long-read behavior, which is a product choice rather than a
// bug: a toast held longer than the life it had left is simply gone the
// moment the viewer closes. The popup said "held" throughout, so nothing
// promised otherwise.
func (m *Model) resumeToasts() {
	now := time.Now()
	for i := range m.toasts {
		f := m.toasts[i].FrozenAt
		if f.IsZero() {
			continue
		}
		m.toasts[i].At = m.toasts[i].At.Add(now.Sub(f))
		// Belt-and-braces (see the doc comment): an At in the future would
		// read as a negative age, which both inflates the countdown and
		// slips under the TTL forever. Clamping to now makes it a full
		// countdown that then runs out.
		if m.toasts[i].At.After(now) {
			m.toasts[i].At = now
		}
		m.toasts[i].FrozenAt = time.Time{}
	}
}

// toastRemain is the live dismiss countdown in whole seconds (ceil, min 1).
func toastRemain(t Toast) int {
	rem := int((toastTTL - time.Since(t.At) + 999*time.Millisecond) / time.Second)
	if rem < 1 {
		rem = 1
	}
	return rem
}

// renderToasts draws the popup stack: a ~1/3-width box, rows per toast
// (● info / × error) plus its live dismiss countdown ("· 4s"), repainted
// every second by the toast tick. "" when empty.
func (m Model) renderToasts() string {
	cw := m.toastContentWidth() - 2 // cmdPopStyle pads one cell each side
	return m.toastBox(cw, maxToastRows)
}

// toastContentWidth is the popup box width, the 1/3-column figure clamped so
// it can neither vanish nor overflow the chat column.
func (m Model) toastContentWidth() int {
	w := m.mainW() / 3
	if w < 24 {
		w = 24
	}
	if w > m.mainW()-2 {
		w = m.mainW() - 2
	}
	return w
}

// toastBox frames a row budget as the popup box. "" for no toasts.
func (m Model) toastBox(cw, budget int) string {
	if len(m.toasts) == 0 {
		return ""
	}
	return cmdPopStyle.Width(cw + 2).Render(
		lipgloss.JoinVertical(lipgloss.Left, m.toastStack(cw, budget)...))
}

// toastStack assembles the popup stack, newest-first, keeping only WHOLE
// toasts: a toast is either wholly visible or wholly absent.
//
// A toast is up to maxToastLines rows, so ten of them would want 30 — more
// than a short terminal has between the header and the editor, and
// overlayToasts must cut an over-long box with a blunt tlines[:avail]. That
// cut lands mid-toast, stranding a text row away from the countdown and
// elision row above it, so the stack is pre-trimmed at a toast boundary
// instead. budget is in content rows; overlayToasts passes the space it
// actually has, so a short terminal drops the oldest toast rather than
// slicing the newest one in half.
func (m Model) toastStack(cw, budget int) []string {
	if budget < 1 {
		budget = 1
	}
	lines := make([]string, 0, budget)
	for i := len(m.toasts) - 1; i >= 0; i-- {
		rows := toastRows(m.toasts[i], cw)
		if len(lines)+len(rows) > budget {
			break
		}
		lines = append(rows, lines...) // prepend: newest last, as before
	}
	return lines
}

// toastRows renders one toast as 1..maxToastLines rows of exactly cw cells
// (the box content width: cmdPopStyle pads one cell on each side).
//
// Every source of truncation is made visible, because a silent cut is why a
// long error was unreadable in the first place:
//   - a message wider than the column is fitted with a trailing "…" (Fit),
//   - a message taller than the row budget spends its last row on
//     "…(+N lines) ctrl+x" instead of dropping the tail silently,
//   - the countdown rides the last row actually emitted, so an elided toast
//     still says how long it has left. It used to ride the last *text* row,
//     which the elision row replaced — leaving a truncated toast with no
//     expiry indicator at all, and a frozen one with neither a countdown
//     nor "held".
//
// A titled toast (extension notify) spends its first row on the title.
// Rows are built to cw and cmdPopStyle pads what lipgloss trims, so the box
// is the same width no matter how long the message is — overlayToasts
// positions the box by the width of its first row, and a ragged row would
// walk it left.
func toastRows(t Toast, cw int) []string {
	head := "● "
	if t.Err {
		head = "× "
	}
	headW := lipgloss.Width(head)
	indent := strings.Repeat(" ", headW)
	tag := " · " + strconv.Itoa(toastRemain(t)) + "s"
	if !t.FrozenAt.IsZero() {
		tag = " · held" // a viewer has it; the countdown is paused
	}
	tagW := lipgloss.Width(tag)

	body := make([]string, 0, maxToastLines)
	if strings.TrimSpace(t.Title) != "" {
		body = append(body, t.Title)
	}
	body = append(body, strings.Split(t.Text, "\n")...)
	// Trailing blank lines say nothing; a stack trace's leading indent does.
	for len(body) > 0 && strings.TrimSpace(body[len(body)-1]) == "" {
		body = body[:len(body)-1]
	}
	if len(body) == 0 {
		body = []string{""}
	}

	pointer := ""
	if len(body) > maxToastLines {
		hidden := len(body) - (maxToastLines - 1)
		// The key hint is worth the two cells, but not a half-cut one: a
		// narrow column drops back to the bare count rather than rendering
		// "…(+2 lines) c…", which reads as a truncation of the message.
		pointer = fmt.Sprintf("…(+%d lines)", hidden)
		if lipgloss.Width(pointer)+len(" ctrl+x")+headW+tagW <= cw {
			pointer += " ctrl+x"
		}
		body = body[:maxToastLines-1]
	}

	out := make([]string, 0, maxToastLines)
	for i, ln := range body {
		// The last text row gives up its width to the countdown only when
		// there is no elision row to carry it.
		lastText := i == len(body)-1 && pointer == ""
		bw := cw - headW
		if lastText {
			bw -= tagW
		}
		if bw < 1 {
			bw = 1
		}
		pre := indent
		if i == 0 {
			pre = head
			if t.Err {
				pre = errStyle.Render(head)
			} else {
				pre = okStyle.Render(head)
			}
		}
		row := pre + statusBarStyle.Render(Fit(ln, bw))
		if lastText {
			row += toolStyle.Render(tag)
		}
		out = append(out, row)
	}
	if pointer != "" {
		// The elision row is the last row emitted, so it carries the
		// countdown: "…(+2 lines) ctrl+x · 4s".
		bw := cw - headW - tagW
		if bw < 1 {
			bw = 1
		}
		out = append(out, indent+toolStyle.Render(Fit(pointer, bw)+tag))
	}
	return out
}

// truncANSI fits s into exactly n display cells for overlaying: escape
// sequences (CSI colors, OSC hyperlinks) pass through whole and count zero
// width, printable runes stop at the budget, and a reset closes any cut
// mid-style so colors can't bleed into the toast box. Narrows pad with
// spaces so the popup always lands on the same right column.
func truncANSI(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if w := lipgloss.Width(s); w <= n {
		return s + strings.Repeat(" ", n-w)
	}
	var b strings.Builder
	cells := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[': // CSI: copy whole, zero width
				j := i + 2
				for j < len(s) && !isCSIEnd(s[j]) {
					j++
				}
				if j < len(s) {
					j++
				}
				b.WriteString(s[i:j])
				i = j
				continue
			case ']': // OSC hyperlink: ends with BEL or ESC\
				j := i + 2
				for j < len(s) {
					if s[j] == 0x07 {
						j++
						break
					}
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				b.WriteString(s[i:j])
				i = j
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if w := lipgloss.Width(string(r)); cells+w > n {
			break
		} else {
			cells += w
		}
		b.WriteRune(r)
		i += size
	}
	b.WriteString("\x1b[0m")
	return b.String() + strings.Repeat(" ", n-cells)
}

func isCSIEnd(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// overlayToasts floats the popup over the top rows of the chat column,
// right-aligned at ~1/3 width. Only the right third is covered — the left
// 2/3 of each row keeps its text and colors (truncANSI), so the popup
// reads as transparent over the transcript instead of erasing it. Total
// line count never changes: the input box and sidebar stay put — zero
// layout shift. No-op when empty or the terminal is too short.
func (m Model) overlayToasts(left string) string {
	lines := strings.Split(left, "\n")
	// Input box is fixed-frame: textarea 3 + chips 0/1 + footer 1 + border 2.
	inputH := 6 + m.chipH()
	// panelHeight, not lipgloss.Height: an empty string measures as one row
	// under lipgloss, so a hidden task widget (prefs.taskWidgetOff) or an
	// empty list would reserve a phantom row and shift the toast up.
	taskH := panelHeight(m.renderTaskWidget())
	avail := len(lines) - 1 - inputH - taskH // rows below header, above persistent blocks
	if avail <= 0 {
		return left
	}
	// The stack is budgeted to the rows that actually exist here, not to a
	// constant, so a short terminal drops the OLDEST toast whole instead of
	// letting the tlines[:avail] cut below slice the newest one mid-way —
	// which stranded a text row away from its countdown/elision row.
	cw := m.toastContentWidth() - 2 // the box's own border row either side
	budget := min(maxToastRows, max(1, avail-2))
	box := m.toastBox(cw, budget)
	if box == "" {
		return left
	}
	tlines := strings.Split(box, "\n")
	if len(tlines) > avail {
		tlines = tlines[:avail]
	}
	leftW := m.mainW() - lipgloss.Width(tlines[0])
	if leftW < 0 {
		leftW = 0
	}
	for i, tl := range tlines {
		lines[1+i] = truncANSI(lines[1+i], leftW) + tl
	}
	return strings.Join(lines, "\n")
}
