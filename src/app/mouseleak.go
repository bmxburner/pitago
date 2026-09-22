package app

import (
	"regexp"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	// One complete SGR mouse report with the ESC byte lost:
	// "[<Cb;Cx;CyM" (press) or "[<Cb;Cx;Cy m" (release). Terminals only
	// emit these while mouse reporting is on; when a trackpad swipe floods
	// dozens of reports into one input read, bubbletea can split the ESC
	// off as an Alt modifier and the remainder arrives as plain KeyRunes —
	// which the textarea would otherwise insert as literal garbage.
	sgrReport = regexp.MustCompile(`\[<(\d+);(\d+);(\d+)([Mm])`)
	// A report cut off mid-burst ("[<", "[<65", "[<65;50", …).
	sgrTail = regexp.MustCompile(`\[<\d{0,3};?\d{0,4};?\d{0,4}$`)
	// Head of a (possibly partial) report: Cb plus optional Cx/Cy.
	sgrHead = regexp.MustCompile(`^\[<(\d+)(?:;(\d*))?(?:;(\d*))?`)
)

// cleanMouseLeak strips SGR mouse-report remnants from KeyRunes. It returns
// the decoded wheel events (so a swipe still scrolls) and the remaining
// runes. ok=false means no leak — runes are untouched.
func cleanMouseLeak(runes []rune) (events []tea.MouseMsg, cleaned []rune, ok bool) {
	s := string(runes)
	full := sgrReport.FindAllStringSubmatch(s, -1)
	if len(full) == 0 {
		// No complete report: swallow only a digit-bearing tail
		// ("[<65", "[<65;50", …). A bare "[<" alone might be real typing.
		loc := sgrTail.FindStringIndex(s)
		if loc == nil || !sgrHead.MatchString(s[loc[0]:]) {
			return nil, runes, false
		}
		if ev, good := leakEvent(s[loc[0]:]); good {
			events = append(events, ev)
		}
		return events, []rune(s[:loc[0]]), true
	}
	for _, mt := range full {
		if ev, good := leakEventParts(mt[1], mt[2], mt[3]); good {
			events = append(events, ev)
		}
	}
	s = sgrReport.ReplaceAllString(s, "")
	// A burst split across reads can leave a tail fragment behind; in burst
	// context even a bare "[<" is residue, not typing.
	if loc := sgrTail.FindStringIndex(s); loc != nil {
		if ev, good := leakEvent(s[loc[0]:]); good {
			events = append(events, ev)
		}
		s = s[:loc[0]]
	}
	return events, []rune(s), true
}

// leakEvent decodes one (possibly partial) "[<Cb[;Cx[;Cy]]" fragment.
// Clicks/drags carry no scroll direction, so only wheel reports yield an
// event — everything else is still swallowed as noise.
func leakEvent(frag string) (tea.MouseMsg, bool) {
	mt := sgrHead.FindStringSubmatch(frag)
	if mt == nil {
		return tea.MouseMsg{}, false
	}
	return leakEventParts(mt[1], mt[2], mt[3])
}

func leakEventParts(cbS, cxS, cyS string) (tea.MouseMsg, bool) {
	cb, err := strconv.Atoi(cbS)
	if err != nil {
		return tea.MouseMsg{}, false
	}
	// Mirror bubbletea's parseMouseButton: bit 64 = wheel, low 2 bits pick
	// up/down/left/right (00 up, 01 down, 10 left, 11 right).
	if cb&64 == 0 {
		return tea.MouseMsg{}, false
	}
	var btn tea.MouseButton
	switch cb & 3 {
	case 0:
		btn = tea.MouseButtonWheelUp
	case 1:
		btn = tea.MouseButtonWheelDown
	case 2:
		btn = tea.MouseButtonWheelLeft
	default:
		btn = tea.MouseButtonWheelRight
	}
	cx, _ := strconv.Atoi(cxS)
	cy, _ := strconv.Atoi(cyS)
	return tea.MouseMsg{X: cx - 1, Y: cy - 1, Action: tea.MouseActionPress, Button: btn}, true
}

// scrollLeak replays scrubbed wheel reports into the viewports, mirroring
// the normal MouseMsg routing (wheel over the sidebar scrolls it).
func (m *Model) scrollLeak(events []tea.MouseMsg) tea.Cmd {
	if !m.ready || len(events) == 0 {
		return nil
	}
	var cmds []tea.Cmd
	for _, ev := range events {
		var c tea.Cmd
		if ev.Action == tea.MouseActionPress &&
			(ev.Button == tea.MouseButtonWheelUp || ev.Button == tea.MouseButtonWheelDown) &&
			m.overSide(ev.X) {
			m.sideVp, c = m.sideVp.Update(ev)
		} else {
			m.vp, c = m.vp.Update(ev)
		}
		if c != nil {
			cmds = append(cmds, c)
		}
	}
	m.Refresh()
	return tea.Batch(cmds...)
}
