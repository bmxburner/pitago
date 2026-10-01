package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"pitago/src/components/format"
	"pitago/src/components/theme"
)

// Tidy-mode tool pills. The pill replaces the collapsed header-in-a-box
// with one frameless line: bullet, chip, pretty args. These pin the three
// things that would silently regress — the frame must be gone, the args
// must survive, and the execution state must still be readable.

func renderPill(t *testing.T, bl Block, w int) string {
	t.Helper()
	m := Model{Tidy: true}
	out := m.renderToolBlock(bl, w)
	for _, r := range out {
		if r == '\n' {
			t.Fatalf("pill rendered more than one line: %q", out)
		}
	}
	return out
}

func TestToolPillCollapsesToOneFramelessLine(t *testing.T) {
	bl := readBlock("done")
	out := renderPill(t, bl, 100)

	if strings.Contains(out, "╭") || strings.Contains(out, "│") || strings.Contains(out, "╰") {
		t.Errorf("pill kept the frame a collapsed block does not need: %q", stripSelectionANSI(out))
	}
	if !strings.Contains(out, "READ") {
		t.Errorf("pill dropped the tool name chip: %q", stripSelectionANSI(out))
	}
	if !strings.Contains(stripSelectionANSI(out), "src/app/view.go") {
		t.Errorf("pill dropped the args preview: %q", stripSelectionANSI(out))
	}
}

func TestToolPillAdvertisesExpandOnlyWhenSomethingIsHidden(t *testing.T) {
	withBody := renderPill(t, readBlock("done"), 100)
	if !strings.Contains(withBody, expandHint) {
		t.Errorf("a call with a result must offer ctrl+g, got %q", stripSelectionANSI(withBody))
	}

	// A bare status refresh: no detail line, no result. Collapsing hides
	// nothing, so an expand hint would promise a difference that is not there.
	bare := Block{Kind: "tool", ToolName: "read", ToolStatus: "running"}
	if got := renderPill(t, bare, 100); strings.Contains(got, expandHint) {
		t.Errorf("bare call advertised a hidden expansion: %q", stripSelectionANSI(got))
	}
}

func TestToolPillKeepsExecutionStateTint(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	defer ApplyTheme(theme.Get("default"))

	ok := lipgloss.NewStyle().Background(toolFill(format.StatusSuccess)).Render("X")
	bad := lipgloss.NewStyle().Background(toolFill(format.StatusError)).Render("X")
	got := renderPill(t, readBlock("error"), 100)

	if strings.Contains(got, bad) {
		t.Error("a failed call's pill lost the error fill its frame carried")
	}
	if strings.Contains(got, ok) {
		t.Error("a failed call's pill is tinted as if it succeeded")
	}
}

func TestToolPillTruncatesArgsToWidth(t *testing.T) {
	long := strings.Repeat("a", 400)
	bl := Block{
		Kind: "tool", ToolName: "read", ToolStatus: "done",
		ToolArgs:    long,
		ToolArgsRaw: `{"file_path":"` + long + `"}`,
	}
	got := stripSelectionANSI(renderPill(t, bl, 60))
	if w := lipgloss.Width(got); w > 60 {
		t.Errorf("pill overflowed its width: %d cols for %q", w, got)
	}
	if !strings.Contains(got, "…") {
		t.Error("truncated args should end in an ellipsis")
	}
}

func TestToolPillYieldsToExpandedFrame(t *testing.T) {
	m := Model{Tidy: true, expandTools: true}
	out := m.renderToolBlock(readBlock("done"), 100)
	if !strings.Contains(out, "╭") {
		t.Error("ctrl+g under tidy mode must restore the framed render, not the pill")
	}
}
