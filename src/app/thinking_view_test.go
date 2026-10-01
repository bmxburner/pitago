package app

import (
	"fmt"
	"strings"
	"testing"
)

// Thinking display modes. The three-mode cycle replaced one flat
// truncated line, so the tests pin what each mode must and must not
// show — a tail that silently drops the elision count would read as a
// complete thought that is not.

// thinkingLines builds n numbered lines. Numbered rather than patterned so
// a test can assert on a specific end of the block without the fixture's
// own repetition making "first line" and "last line" look alike.
func thinkingLines(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "L%02d thought line\n", i)
	}
	return b.String()
}

func thinkingRender(mode, text string) string {
	m := Model{ThinkingView: mode}
	return renderThinkingBody(&m, text, 80)
}

func TestThinkingViewCollapsedShowsOneLineOnly(t *testing.T) {
	got := stripSelectionANSI(thinkingRender(thinkingCollapsed, thinkingLines(12)))
	if lines := strings.Count(strings.TrimRight(got, "\n"), "\n") + 1; lines != 1 {
		t.Errorf("collapsed mode must render exactly one line, got %d: %q", lines, got)
	}
	if strings.Contains(got, "earlier lines elided") {
		t.Error("collapsed mode has no tail to count")
	}
}

func TestThinkingViewTailKeepsLastLinesAndCountsElision(t *testing.T) {
	got := stripSelectionANSI(thinkingRender(thinkingTail, thinkingLines(12)))
	if !strings.Contains(got, "elided") {
		t.Errorf("a truncated tail must say how much it dropped: %q", got)
	}
	if !strings.Contains(got, "L12") {
		t.Errorf("tail must keep the LAST line, not the first: %q", got)
	}
	if strings.Contains(got, "L01") {
		t.Errorf("tail kept the opening line, which is what collapsed is for: %q", got)
	}
}

func TestThinkingViewTailIsNoElisionWhenShort(t *testing.T) {
	got := stripSelectionANSI(thinkingRender(thinkingTail, "just one short thought\n"))
	if strings.Contains(got, "elided") {
		t.Errorf("a short thought is not truncated and must not claim it was: %q", got)
	}
}

func TestThinkingViewFullRendersTheWholeBlock(t *testing.T) {
	got := stripSelectionANSI(thinkingRender(thinkingFull, thinkingLines(12)))
	if !strings.Contains(got, "L01") {
		t.Errorf("full mode must include the first line: %q", got)
	}
	if !strings.Contains(got, "L12") {
		t.Errorf("full mode must include the last line: %q", got)
	}
}

func TestThinkingViewUnknownFallsBackToTail(t *testing.T) {
	// A hand-edited prefs.json must not be able to blank the transcript:
	// an unrecognized mode resolves to the default rather than hiding
	// reasoning or rendering nothing.
	if got := normalizeThinkingView("nonsense"); got != thinkingTail {
		t.Errorf("unknown mode = %q, want %q", got, thinkingTail)
	}
	if got := normalizeThinkingView(""); got != thinkingTail {
		t.Errorf("absent pref = %q, want %q", got, thinkingTail)
	}
	if got := stripSelectionANSI(thinkingRender("nonsense", "a thought\n")); !strings.Contains(got, "a thought") {
		t.Errorf("an unknown mode rendered nothing: %q", got)
	}
}

func TestNextThinkingViewCyclesAndWraps(t *testing.T) {
	// The cycle is the primary control, so a broken order would strand
	// the user in a mode with no way back out.
	want := map[string]string{
		thinkingCollapsed: thinkingTail,
		thinkingTail:      thinkingFull,
		thinkingFull:      thinkingCollapsed,
	}
	for from, to := range want {
		if got := nextThinkingView(from); got != to {
			t.Errorf("next(%s) = %s, want %s", from, got, to)
		}
	}
}

func TestThinkingViewIsPartOfTheRenderCacheKey(t *testing.T) {
	bl := Block{Kind: "thinking", Text: "a thought that is quite long\n"}
	a := blockKey(bl, 80, false, false, "default", false, thinkingCollapsed)
	b := blockKey(bl, 80, false, false, "default", false, thinkingFull)
	if a == b {
		t.Error("blockKey must differ when the thinking view changes (stale cache otherwise)")
	}
}
