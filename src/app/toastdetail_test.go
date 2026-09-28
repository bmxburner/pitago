package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- popup rendering -------------------------------------------------------

// A toast wider than the popup column must say so. The original bug was a
// silent cut: the user could not tell the text was truncated at all.
func TestToastRowsMarkHorizontalTruncation(t *testing.T) {
	rows := toastRows(Toast{Text: strings.Repeat("verbose-notification-text ", 8), At: time.Now()}, 40)
	joined := stripANSI(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "…") {
		t.Fatalf("a cut message must carry an ellipsis:\n%s", joined)
	}
	for _, r := range rows {
		if w := lipgloss.Width(stripANSI(r)); w > 40 {
			t.Fatalf("row %d cells exceeds the 40-cell popup: %q", w, r)
		}
	}
}

// No row may exceed the content width. Rows are not padded individually
// (lipgloss trims trailing spaces inside Render; cmdPopStyle pads the
// block), so this asserts the overflow direction, which is what once
// pushed the countdown out of the box.
func TestToastRowsNeverExceedWidth(t *testing.T) {
	for _, text := range []string{
		"short",
		strings.Repeat("x", 200),
		"one\ntwo\nthree\nfour\nfive",
		"trailing blank\n\n\n",
	} {
		for _, cw := range []int{22, 38, 70} {
			for _, r := range toastRows(Toast{Text: text, At: time.Now()}, cw) {
				if got := lipgloss.Width(stripANSI(r)); got > cw {
					t.Fatalf("text=%q cw=%d: row is %d cells: %q", text, cw, got, stripANSI(r))
				}
			}
		}
	}
}

// Multi-line text used to be flattened to "⏎" by format.Short. A multi-line
// extension notice must now occupy several rows, and the elided tail must
// be advertised rather than dropped silently.
func TestToastRowsSplitLinesAndAdvertiseElision(t *testing.T) {
	rows := toastRows(Toast{Text: "l1\nl2\nl3\nl4\nl5", At: time.Now()}, 40)
	joined := stripANSI(strings.Join(rows, "\n"))
	// 5 lines, a 3-row budget: 2 text rows plus the elision pointer.
	for _, want := range []string{"l1", "l2", "…(+3 lines)", "ctrl+x"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "l5") {
		t.Errorf("the elided tail must not render:\n%s", joined)
	}
	if len(rows) > maxToastLines {
		t.Fatalf("a toast must never exceed its row budget: %d rows", len(rows))
	}
}

// A titled toast spends its first row on the heading — this is the notify
// title pi already sends, which pitago used to drop.
func TestToastRowsShowTitle(t *testing.T) {
	rows := toastRows(Toast{Title: "Marketplace", Text: "3 updates", At: time.Now()}, 40)
	joined := stripANSI(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "Marketplace") || !strings.Contains(joined, "3 updates") {
		t.Fatalf("title and message must both render:\n%s", joined)
	}
}

// The countdown rides the last row, so a multi-line toast still shows how
// long it has left.
func TestToastRowsCountdownOnLastRow(t *testing.T) {
	rows := toastRows(Toast{Text: "a\nb", At: time.Now()}, 40)
	if !strings.Contains(stripANSI(rows[len(rows)-1]), "s") {
		t.Fatalf("last row must carry the countdown: %q", stripANSI(rows[len(rows)-1]))
	}
	if stripANSI(rows[0]) == stripANSI(rows[len(rows)-1]) {
		t.Fatal("the countdown must appear once, on the last row only")
	}
}

// --- freeze / resume -------------------------------------------------------

// Freeze: a toast held open by the history must not expire, however old.
func TestFrozenToastSurvivesPruning(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("held", false)
	m.OpenNotificationsFromToast() // freezes the newest popup
	if m.toasts[0].FrozenAt.IsZero() {
		t.Fatal("opening the history must freeze the popup it shows")
	}
	m.toasts[0].At = time.Now().Add(-2 * toastTTL) // far past expiry
	m.pruneToasts()
	if len(m.toasts) != 1 {
		t.Fatalf("a frozen toast must survive pruning, got %d", len(m.toasts))
	}
}

// A freeze is a promise to a reader, so it only lasts while the history is
// open. dismissDialog resumes on the normal close, but several other paths
// pop dialogs (answerDialog, CloseAllDialogs, respawn) — a leak here would
// pin the popup on screen and keep the 1Hz tick alive forever, so prune
// re-derives the freeze from the dialog stack.
func TestFreezeReleasesWhenHistoryCloses(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("held", false)
	m.OpenNotificationsFromToast()
	m.toasts[0].At = time.Now().Add(-2 * toastTTL) // far past expiry

	// The history goes away by a route that is NOT dismissDialog — the leak
	// this guard exists for.
	m.Dialogs = nil
	m.pruneToasts()
	if len(m.toasts) != 0 {
		t.Fatalf("a freeze with no reader must be released, got %+v", m.toasts)
	}
}

// Resume: the countdown continues where it stood instead of restarting.
func TestResumeRestoresRemainingTime(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("held", false)
	// Age it 4s first, so a 3s rewind lands inside the toast's life and is
	// observable (a rewind past the TTL is clamped, see the over-read test).
	m.toasts[0].At = time.Now().Add(-4 * time.Second)
	before := m.toasts[0].At
	m.OpenNotificationsFromToast()
	m.toasts[0].FrozenAt = time.Now().Add(-3 * time.Second)
	m.resumeToasts()
	if !m.toasts[0].FrozenAt.IsZero() {
		t.Fatal("resumeToasts must clear the freeze")
	}
	if elapsed := m.toasts[0].At.Sub(before); elapsed < 2*time.Second || elapsed > 4*time.Second {
		t.Fatalf("the 3s frozen span must be applied to the clock, got %v", elapsed)
	}
}

// resumeToasts runs on every dialog close; it must be free when nothing was
// frozen.
func TestResumeToastsNoopWithoutFreeze(t *testing.T) {
	m := &Model{}
	m.pushToast("untouched", false)
	before := m.toasts[0].At
	m.resumeToasts()
	if !m.toasts[0].At.Equal(before) {
		t.Fatal("resumeToasts must not touch an unfrozen toast")
	}
}

// A toast read far past its life must leave an honest countdown: the rewind
// cannot buy more than one TTL, and the age must never go negative (that
// reads as "· 40s" and never expires).
func TestOverReadToastKeepsHonestCountdown(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("read far too long", false)
	m.OpenNotificationsFromToast()
	m.toasts[0].FrozenAt = time.Now().Add(-3 * toastTTL) // 30s of a 10s toast
	m.resumeToasts()
	if rem := toastRemain(m.toasts[0]); rem > int(toastTTL/time.Second)+1 {
		t.Fatalf("an over-read toast must not show more than one TTL, got %ds", rem)
	}
	if age := time.Since(m.toasts[0].At); age < 0 {
		t.Fatalf("age must never be negative, got %v", age)
	}
	m.toasts[0].At = time.Now().Add(-2 * toastTTL)
	m.pruneToasts()
	if len(m.toasts) != 0 {
		t.Fatalf("an expired toast must be pruned, got %d", len(m.toasts))
	}
}

// A frozen toast reports "held" rather than a countdown that is lying.
func TestFrozenToastShowsHeld(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("frozen", true)
	m.OpenNotificationsFromToast()
	if out := stripANSI(m.renderToasts()); !strings.Contains(out, "held") {
		t.Fatalf("a frozen toast must not show a countdown:\n%s", out)
	}
}

// The countdown rides the last row actually EMITTED. It used to ride the
// last text row, which the elision row replaced — so any toast tall enough
// to be elided showed no expiry indicator at all.
func TestElidedToastStillShowsCountdown(t *testing.T) {
	rows := toastRows(Toast{Text: "l1\nl2\nl3\nl4\nl5", At: time.Now()}, 40)
	joined := stripANSI(strings.Join(rows, "\n"))
	if !strings.Contains(joined, "…(+3 lines)") {
		t.Fatalf("expected an elided toast:\n%s", joined)
	}
	if !strings.Contains(joined, "s") || !strings.Contains(joined, "·") {
		t.Fatalf("an elided toast must still show its countdown:\n%s", joined)
	}
}

// A narrow column drops the "ctrl+x" hint rather than half-cutting it to
// "c…", which would read as a truncation of the message itself. The count
// and the countdown always survive.
func TestElisionHintIsNeverHalfCut(t *testing.T) {
	for _, cw := range []int{22, 26, 30, 40, 70} {
		rows := toastRows(Toast{Text: "l1\nl2\nl3\nl4", At: time.Now()}, cw)
		last := stripANSI(rows[len(rows)-1])
		if !strings.Contains(last, "…(+2 lines)") {
			t.Fatalf("cw=%d: the line count must always show: %q", cw, last)
		}
		if !strings.Contains(last, "·") {
			t.Fatalf("cw=%d: the countdown must always show: %q", cw, last)
		}
		if strings.Contains(last, "c…") || strings.Contains(last, "ctrl…") {
			t.Fatalf("cw=%d: the key hint must be dropped whole, not cut: %q", cw, last)
		}
		if lipgloss.Width(last) > cw {
			t.Fatalf("cw=%d: row overflows: %q", cw, last)
		}
	}
}

// Same for the frozen state: "held", not a countdown that is lying — and
// not nothing at all.
func TestElidedFrozenToastStillShowsHeld(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("a\nb\nc\nd\ne", true)
	m.OpenNotificationsFromToast()
	out := stripANSI(m.renderToasts())
	if strings.Contains(out, "…(") && !strings.Contains(out, "held") {
		t.Fatalf("an elided frozen toast must still say held:\n%s", out)
	}
}

// The stack is trimmed at a TOAST boundary. Without the cap, ten 3-row
// toasts want 30 rows, overlayToasts cuts with tlines[:avail], and the cut
// lands mid-toast — dropping a text row away from the countdown/elision
// row above it.
func TestToastStackIsTrimmedWholeToasts(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	for i := range maxToasts {
		m.pushToast("toast number "+string(rune('a'+i))+"\nsecond line\nthird line", i%2 == 0)
	}
	rows := 0
	for _, r := range strings.Split(stripANSI(m.renderToasts()), "\n") {
		if strings.Contains(r, "╭") || strings.Contains(r, "╰") || strings.Contains(r, "│") {
			continue
		}
		if strings.TrimSpace(r) != "" {
			rows++
		}
	}
	if rows > maxToastRows {
		t.Fatalf("stack rendered %d content rows, cap is %d", rows, maxToastRows)
	}
	// The newest toast is the one that must survive the trim.
	if out := stripANSI(m.renderToasts()); !strings.Contains(out, "toast number j") {
		t.Fatalf("the trim must keep the newest toasts:\n%s", out)
	}
	// A stack cut mid-toast is the actual defect: its last content row is a
	// bare continuation with no countdown and no elision pointer. A whole
	// toast always ends on a row carrying one of those.
	var content []string
	for _, r := range strings.Split(stripANSI(m.renderToasts()), "\n") {
		if strings.TrimSpace(r) == "" || strings.Contains(r, "╭") || strings.Contains(r, "╰") {
			continue
		}
		content = append(content, r)
	}
	if last := content[len(content)-1]; !strings.Contains(last, "·") {
		t.Fatalf("the stack must end on a toast's final row, got %q", last)
	}
}

// A title shown in the popup must be recoverable everywhere else: the
// detail pane and Ctrl+Y. It used to be the one piece of a notification
// that survived in no other view.
func TestNotificationTitleReachesPaneAndClipboard(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToastTitled("Build Failed", "cannot find module foo\nat main.go:12", true)
	m.OpenNotifications("")
	d := m.Dialogs[0]

	plain := stripANSI(m.renderNotificationDialog(d))
	if !strings.Contains(plain, "Build Failed") {
		t.Fatalf("the pane must show the title:\n%s", plain)
	}
	if copied := trajSelected(d); !strings.Contains(copied, "Build Failed") ||
		!strings.Contains(copied, "at main.go:12") {
		t.Fatalf("Ctrl+Y must copy the title and the body, got %q", copied)
	}
	// The title is a heading, not a row of its own: it must not displace
	// the body in the copy.
	if strings.Count(trajSelected(d), "Build Failed") != 1 {
		t.Fatalf("title must appear once: %q", trajSelected(d))
	}
}

// At winH <= 11 there is no pane, so the footer must not advertise ←/→ — and
// those keys must not write an offset nothing renders, which would resurface
// as a pane scrolled to the middle the moment the terminal grew a row.
func TestNotificationDetailKeysInertWithoutPane(t *testing.T) {
	m := Model{winW: 100, winH: 10}
	_, _, _, _, _, _, detH := notificationGeom(100, 10)
	if detH != 0 {
		t.Fatalf("setup: winH=10 should have no detail pane, got detH=%d", detH)
	}
	m.pushToast(strings.Repeat("body\n", 100), true)
	m.OpenNotifications("")

	if foot := stripANSI(m.renderNotificationDialog(m.Dialogs[0])); strings.Contains(foot, "←/→") {
		t.Fatalf("footer must not advertise pane keys with no pane:\n%s", foot)
	}
	m.updateNotificationDetail(tea.KeyMsg{Type: tea.KeyRight}, m.Dialogs[0])
	if got := m.Dialogs[0].TrajOff; got != 0 {
		t.Fatalf("an invisible pane must not move the offset, got %d", got)
	}
}

// The stack is trimmed at a TOAST boundary, against the rows the layout
// ACTUALLY has. Capping at a constant is not enough: overlayToasts used to
// cut with tlines[:avail], and on a short terminal avail is below the cap, so
// the cut landed mid-toast and stranded a text row away from its countdown.
// This drives overlayToasts, not renderToasts, because that is where the
// second cut lived.
func TestOverlayTrimsAtToastBoundaryOnShortTerminal(t *testing.T) {
	for _, h := range []int{12, 16, 18, 20, 24, 40} {
		m := Model{winW: 120, winH: h}
		for i := range maxToasts {
			m.pushToast("toast "+string(rune('a'+i))+"\nsecond line\nthird line", i%2 == 0)
		}
		// A frame with the real number of rows: overlayToasts budgets from
		// len(lines), so a 3-line stub makes avail negative and the test
		// would pass vacuously.
		frame := make([]string, h)
		for i := range frame {
			frame[i] = "chat text here"
		}
		out := stripANSI(m.overlayToasts(strings.Join(frame, "\n")))

		// Every toast must be WHOLE: the last row inside the box belongs to
		// a complete toast, so it carries a countdown or the elision pointer.
		// A mid-toast cut leaves a bare continuation there instead.
		var content []string
		inBox := false
		for _, ln := range strings.Split(out, "\n") {
			switch {
			case strings.Contains(ln, "╭"):
				inBox = true
			case strings.Contains(ln, "╰"):
				inBox = false
			case inBox:
				content = append(content, ln)
			}
		}
		if len(content) == 0 {
			t.Fatalf("winH=%d: expected visible toasts:\n%s", h, out)
		}
		last := content[len(content)-1]
		if !strings.Contains(last, "·") {
			t.Fatalf("winH=%d: the stack must end on a toast's final row, got %q", h, last)
		}
	}
}

// A freeze is a promise to a reader, so it survives closing an unrelated
// dialog sitting on the history. dismissDialog runs for EVERY dialog close,
// and pruneToasts only releases (never re-freezes), so an unguarded resume
// ended the hold while the reader was still there.
//
// Not reachable today: drainQueuedDialogs only promotes a queued extension
// dialog when len(m.Dialogs) == 0, and the history occupies that slot, so
// nothing can stack on it. The stack is built by hand here to pin the
// invariant for whenever stacking is allowed.
func TestFreezeSurvivesUnrelatedDialogClose(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("read me", true)
	m.OpenNotificationsFromToast()
	history := m.Dialogs[0]

	// Dialogs[0] is the ACTIVE one (updateDialog always reads index 0), so
	// the unrelated dialog goes in front, not at the end.
	m.Dialogs = append([]*Dialog{{Kind: "ui", Method: "confirm"}}, m.Dialogs...)
	nm, _ := m.updateDialog(tea.KeyMsg{Type: tea.KeyEsc})
	got := nm.(Model)

	if len(got.Dialogs) != 1 || got.Dialogs[0] != history {
		t.Fatalf("the history must still be open, got %d dialogs", len(got.Dialogs))
	}
	if got.toasts[0].FrozenAt.IsZero() {
		t.Fatal("closing an unrelated dialog must not end the hold: the reader is still there")
	}
}

// Closing the history itself does release it (the sibling of the case above).
func TestFreezeReleasedWhenHistoryCloses(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("read me", true)
	m.OpenNotificationsFromToast()
	nm, _ := m.updateDialog(tea.KeyMsg{Type: tea.KeyEsc})
	got := nm.(Model)
	if len(got.Dialogs) != 0 {
		t.Fatalf("Esc must close the history, got %+v", got.Dialogs)
	}
	if !got.toasts[0].FrozenAt.IsZero() {
		t.Fatal("closing the history must end the hold")
	}
}

// --- Ctrl+X → the history, with the newest entry held ----------------------

// Ctrl+X opens the one notification dialog, with the newest entry selected.
func TestOpenNotificationsFromToastSelectsNewest(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("oldest", false)
	m.pushToast("newest and much longer than the popup column can show", true)

	m.OpenNotificationsFromToast()
	if len(m.Dialogs) != 1 {
		t.Fatalf("Ctrl+X must open exactly one dialog, got %d", len(m.Dialogs))
	}
	d := m.Dialogs[0]
	if d.Kind != "notification" {
		t.Fatalf("kind = %q: there must be no second notification surface", d.Kind)
	}
	if got := notificationSelected(d); got != "newest and much longer than the popup column can show" {
		t.Fatalf("newest entry must be selected, got %q", got)
	}
}

// The opened popup is frozen, and only it: an unrelated live toast keeps
// counting down.
func TestOpenNotificationsFromToastFreezesOnlyThatToast(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("unrelated", false)
	m.pushToast("the one being read", true)

	m.OpenNotificationsFromToast()
	var frozen, live int
	for _, t := range m.toasts {
		if t.FrozenAt.IsZero() {
			live++
		} else {
			frozen++
		}
	}
	if frozen != 1 || live != 1 {
		t.Fatalf("want exactly one frozen and one live toast, got frozen=%d live=%d", frozen, live)
	}
}

// An already-expired popup is still readable: the history outlives the
// stack, and there is nothing to freeze.
func TestOpenNotificationsFromToastWorksAfterExpiry(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("gone from the corner already", true)
	m.toasts = nil

	m.OpenNotificationsFromToast()
	if len(m.Dialogs) != 1 {
		t.Fatalf("history must still open, got %d dialogs", len(m.Dialogs))
	}
	if got := notificationSelected(m.Dialogs[0]); got != "gone from the corner already" {
		t.Fatalf("selected = %q, want the expired entry's text", got)
	}
	if len(m.toasts) != 0 {
		t.Fatalf("an expired entry must not be resurrected, got %+v", m.toasts)
	}
}

// Closing the history hands the countdown back.
func TestClosingHistoryResumesTheCountdown(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("read me", true)
	m.OpenNotificationsFromToast()
	frozenAt := m.toasts[0].FrozenAt

	nm, _ := m.updateDialog(tea.KeyMsg{Type: tea.KeyEsc})
	got := nm.(Model)
	if len(got.Dialogs) != 0 {
		t.Fatalf("Esc must close the history, got %+v", got.Dialogs)
	}
	if len(got.toasts) != 1 {
		t.Fatalf("the toast must survive the close, got %d", len(got.toasts))
	}
	if !got.toasts[0].FrozenAt.IsZero() {
		t.Fatalf("closing must unfreeze (was frozen at %v)", frozenAt)
	}
}

// --- the detail pane -------------------------------------------------------

// The pane is the reason this dialog needs the width: the row is cut to fit,
// so the text the user came for must be readable underneath it.
func TestNotificationDetailShowsFullText(t *testing.T) {
	full := "pi error: the whole untruncated body of a very long error message"
	m := &Model{winW: 120, winH: 40}
	m.pushToast(full, true)
	m.OpenNotifications("")

	plain := stripANSI(m.renderNotificationDialog(m.Dialogs[0]))
	if !strings.Contains(plain, "the whole untruncated body of a very long") {
		t.Fatalf("detail pane must show the text the row cut:\n%s", plain)
	}
	if !strings.Contains(plain, "detail") {
		t.Fatalf("the pane must be labelled:\n%s", plain)
	}
}

// The pane reads exactly what Ctrl+Y copies — same resolver, so the two can
// never disagree.
func TestNotificationDetailMatchesCopyPayload(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.pushToast("copy me in full", true)
	m.OpenNotifications("")
	d := m.Dialogs[0]
	if notificationSelected(d) != trajSelected(d) {
		t.Fatal("pane and clipboard must resolve the same text")
	}
	if !dialogCopyable(d) {
		t.Fatal("the history must stay Ctrl+Y copyable")
	}
}

// The pane preserves an extension's structure: blank lines and indentation,
// which is what a stack trace needs and what a one-line popup never showed.
func TestNotificationDetailKeepsStructure(t *testing.T) {
	m := &Model{winW: 120, winH: 60}
	m.pushToast("Error: boom\n\n  at frameOne\n  at frameTwo", true)
	m.OpenNotifications("")
	plain := stripANSI(m.renderNotificationDialog(m.Dialogs[0]))
	if !strings.Contains(plain, "at frameOne") || !strings.Contains(plain, "at frameTwo") {
		t.Fatalf("pane must keep the trace lines:\n%s", plain)
	}
}

// ←/→ scroll the pane, and clamp at both ends.
func TestNotificationDetailScrollsAndClamps(t *testing.T) {
	m := &Model{winW: 100, winH: 30}
	m.pushToast(strings.Repeat("long line\n", 200), true)
	m.OpenNotifications("")
	d := m.Dialogs[0]
	_, _, rowW, _, _, _, detH := notificationGeom(m.winW, m.winH)
	total := len(toastWrap(notificationSelected(d), rowW))

	nm, _ := m.updateDialog(tea.KeyMsg{Type: tea.KeyRight})
	if nm.(Model).Dialogs[0].TrajOff != 1 {
		t.Fatalf("→ must step one line, got %d", nm.(Model).Dialogs[0].TrajOff)
	}
	nm, _ = nm.(Model).updateDialog(tea.KeyMsg{Type: tea.KeyLeft})
	if nm.(Model).Dialogs[0].TrajOff != 0 {
		t.Fatalf("← must step back to the top, got %d", nm.(Model).Dialogs[0].TrajOff)
	}
	nm, _ = nm.(Model).updateDialog(tea.KeyMsg{Type: tea.KeyLeft})
	if got := nm.(Model).Dialogs[0].TrajOff; got != 0 {
		t.Fatalf("offset must clamp at 0, got %d", got)
	}
	nm, _ = nm.(Model).updateDialog(tea.KeyMsg{Type: tea.KeyEnd})
	if got := nm.(Model).Dialogs[0].TrajOff; got != total-detH {
		t.Fatalf("End = %d, want the last page %d (total %d, pane %d)", got, total-detH, total, detH)
	}
}

// Paging the list selects a new document, so the pane returns to its top.
func TestNotificationPagingResetsDetail(t *testing.T) {
	m := &Model{winW: 100, winH: 30}
	// Each entry must wrap to more rows than the pane shows, or there is
	// nothing to scroll and the test proves nothing.
	for i := range 40 {
		m.pushToast(strings.Repeat("entry text ", 120)+string(rune('a'+i)), i%2 == 0)
	}
	m.OpenNotifications("")
	nm, _ := m.updateDialog(tea.KeyMsg{Type: tea.KeyRight})
	nm, _ = nm.(Model).updateDialog(tea.KeyMsg{Type: tea.KeyEnd})
	if nm.(Model).Dialogs[0].TrajOff == 0 {
		t.Fatal("setup: End must scroll the pane")
	}
	nm, _ = nm.(Model).updateDialog(tea.KeyMsg{Type: tea.KeyPgDown})
	got := nm.(Model).Dialogs[0]
	if got.TrajOff != 0 {
		t.Fatalf("paging to a new row must reset the pane, got %d", got.TrajOff)
	}
	if got.Cursor == 0 {
		t.Fatal("PgDown must still move the list cursor")
	}
}

// The pane must never eat the list: a row and a line of its text survive
// even on a 40x12 terminal.
func TestNotificationDetailYieldsToListOnTinyTerminal(t *testing.T) {
	m := &Model{winW: 40, winH: 12}
	m.pushToast(strings.Repeat("packed ", 40), true)
	m.OpenNotifications("")
	_, _, _, win, _, _, detH := notificationGeom(40, 12)
	if win < 1 || detH < 1 {
		t.Fatalf("tiny terminal must still afford a row and a detail line: win=%d detH=%d", win, detH)
	}
	if plain := stripANSI(m.renderNotificationDialog(m.Dialogs[0])); !strings.Contains(plain, "packed") {
		t.Fatalf("the row must still render")
	}
}

// With nothing selected the pane says so instead of rendering a stale body.
func TestNotificationDetailEmptyState(t *testing.T) {
	m := &Model{winW: 120, winH: 40}
	m.OpenNotifications("no such thing")
	plain := stripANSI(m.renderNotificationDialog(m.Dialogs[0]))
	if !strings.Contains(plain, "no matching notifications") {
		t.Fatalf("list empty state missing:\n%s", plain)
	}
	if !strings.Contains(plain, "no notification selected") {
		t.Fatalf("pane empty state missing:\n%s", plain)
	}
}

// toastWrap keeps the author's line structure: blank lines stay blank and
// indentation survives.
func TestToastWrapKeepsStructure(t *testing.T) {
	got := toastWrap("first\n\n  indented\nlast", 40)
	if len(got) != 4 {
		t.Fatalf("want 4 rows (blank preserved), got %d: %q", len(got), got)
	}
	if got[1] != "" {
		t.Errorf("blank line must stay blank, got %q", got[1])
	}
	if !strings.HasPrefix(got[2], "  ") {
		t.Errorf("indent must survive: %q", got[2])
	}
}

// A long single line wraps rather than being cut.
func TestToastWrapLongLineWraps(t *testing.T) {
	got := toastWrap(strings.Repeat("w", 100), 30)
	if len(got) < 4 {
		t.Fatalf("a 100-cell line at 30 cells must wrap to 4+ rows, got %d", len(got))
	}
	for _, r := range got {
		if lipgloss.Width(r) > 30 {
			t.Fatalf("row wider than the budget: %d: %q", lipgloss.Width(r), r)
		}
	}
}
