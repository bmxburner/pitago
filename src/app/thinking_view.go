package app

import (
	"fmt"
	"strings"
)

// Thinking display modes.
//
// Before this, a thinking block rendered as one flat truncated line —
// first 300 characters, hard-cut, no way to see the rest. Three modes
// replace that single presentation:
//
//   - collapsed — one muted line. The reasoning is not on screen at all,
//     only the fact that it happened.
//   - tail      — the last few lines, muted, with a count of what was
//     elided. The default: where a thought ends is what you want when
//     deciding whether the next action makes sense.
//   - full      — the whole block as markdown.
//
// The cycle lives on the client, not in pi, so switching costs nothing
// and applies to the whole transcript at once. "Hide thinking" stays a
// separate pref and still wins over all three: it is an on/off about
// whether reasoning appears in chat at all, while these three are about
// how much of it does.

const (
	thinkingCollapsed = "collapsed"
	thinkingTail      = "tail"
	thinkingFull      = "full"

	// thinkingTailLines is how many trailing lines the tail mode keeps.
	// Six reads as "the last thought" rather than "a wall of reasoning",
	// which is what separates this from full mode in practice.
	thinkingTailLines = 6
)

// thinkingViews is the cycle order, collapsed → tail → full. Kept as a
// slice so the cycle and the settings row cannot disagree about what
// "next" means.
var thinkingViews = []string{thinkingCollapsed, thinkingTail, thinkingFull}

// normalizeThinkingView maps a stored or unknown value onto a real mode.
// An empty pref predates the feature and means tail, which is the
// intended default rather than the zero value.
func normalizeThinkingView(v string) string {
	for _, m := range thinkingViews {
		if v == m {
			return v
		}
	}
	return thinkingTail
}

// nextThinkingView is the mode after v in cycle order.
func nextThinkingView(v string) string {
	views := thinkingViews
	i := 0
	for j, m := range views {
		if m == normalizeThinkingView(v) {
			i = j
			break
		}
	}
	return views[(i+1)%len(views)]
}

// thinkingViewLabel names a mode for a toast or a settings row.
func thinkingViewLabel(v string) string { return normalizeThinkingView(v) }

// renderThinkingBody renders one thinking block for the given display
// mode. Returns the block body; the caller owns the surrounding chrome.
func renderThinkingBody(m *Model, text string, cw int) string {
	text = strings.TrimRight(text, "\n")
	switch normalizeThinkingView(m.ThinkingView) {
	case thinkingCollapsed:
		return renderThinkingCollapsed(text)
	case thinkingFull:
		return renderThinkingFull(m, text, cw)
	default:
		return renderThinkingTail(text, cw)
	}
}

// renderThinkingCollapsed is one muted line: the opening of the thought,
// cut at the first hard break so the line never reads as a mid-word
// truncation of a sentence the model did not end.
func renderThinkingCollapsed(text string) string {
	first := text
	if i := strings.IndexByte(first, '\n'); i >= 0 {
		first = first[:i]
	}
	first = strings.TrimSpace(first)
	if first == "" {
		return ""
	}
	return statusBarStyle.Render("○ ") + toolStyle.Render(Short(first, 160)) + "\n\n"
}

// renderThinkingTail keeps the last few lines and states how many were
// dropped, so a truncated thought never reads as a complete one.
func renderThinkingTail(text string, cw int) string {
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	elided := 0
	if len(lines) > thinkingTailLines {
		elided = len(lines) - thinkingTailLines
		lines = lines[elided:]
	}
	var b strings.Builder
	b.WriteString(statusBarStyle.Render("○ ") + toolStyle.Render(fmt.Sprintf("thinking · last %d", len(lines))))
	if elided > 0 {
		b.WriteString(toolStyle.Render(fmt.Sprintf(" (%d earlier lines elided)", elided)))
	}
	b.WriteString("\n")
	for _, ln := range lines {
		b.WriteString(toolStyle.Render(Short(ln, cw)) + "\n")
	}
	return b.String() + "\n"
}

// renderThinkingFull renders the whole block as markdown, under a dim
// label so a long passage of reasoning is still findable when scrolling
// back. Prose styling is kept rather than forced to muted: dimming a
// rendered markdown block means re-styling every span it emitted, and
// the label is enough to tell reasoning from an answer.
func renderThinkingFull(m *Model, text string, cw int) string {
	return statusBarStyle.Render("○ thinking") + "\n" + renderMarkdown(m, text, cw) + "\n\n"
}
