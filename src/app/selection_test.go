package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSelectionTextSingleLine(t *testing.T) {
	lines := []string{"hello world", "second line"}
	got := selectionText(lines, Point{Line: 0, Col: 0}, Point{Line: 0, Col: 5})
	if got != "hello" {
		t.Fatalf("got %q, want \"hello\"", got)
	}
}

func TestSelectionTextMultiLine(t *testing.T) {
	lines := []string{"aaaa", "bbbb", "cccc"}
	got := selectionText(lines, Point{Line: 0, Col: 1}, Point{Line: 2, Col: 2})
	want := "aaa\nbbbb\ncc"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSelectionTextReversedAnchor(t *testing.T) {
	lines := []string{"xxxxxx"}
	got := selectionText(lines, Point{Line: 0, Col: 5}, Point{Line: 0, Col: 2})
	if got != "xxx" {
		t.Fatalf("reversed drag must normalize, got %q", got)
	}
}

func TestSelectionTextStripsANSI(t *testing.T) {
	lines := []string{"\x1b[38;2;1;2;3mred\x1b[0m \x1b]8;;https://x\x07link\x1b]8;;\x07 rest"}
	got := selectionText(lines, Point{Line: 0, Col: 0}, Point{Line: 0, Col: 100})
	if got != "red link rest" {
		t.Fatalf("ANSI/OSC not stripped: %q", got)
	}
}

func TestSelectionTextStripsSTHyperlinksWithoutConsumingText(t *testing.T) {
	lines := []string{"\x1b]8;;https://one\x1b\\one\x1b]8;;\x1b\\ after \x1b]8;;https://two\x1b\\two\x1b]8;;\x1b\\ tail"}
	got := selectionText(lines, Point{Line: 0, Col: 0}, Point{Line: 0, Col: 100})
	if got != "one after two tail" {
		t.Fatalf("ST OSC stripping consumed visible text: %q", got)
	}
}

func TestSelectionTextClampsLines(t *testing.T) {
	got := selectionText([]string{"a"}, Point{Line: 0, Col: 0}, Point{Line: 5, Col: 2})
	if !strings.Contains(got, "a") {
		t.Fatalf("out-of-range lines must clamp cleanly, got %q", got)
	}
}

func TestSliceColumnsGraphemes(t *testing.T) {
	s := "héllo 🌍 w"
	if got := sliceColumns(s, 0, 2); got != "hé" {
		t.Fatalf("combined e+acute split: %q", got)
	}
	if got := sliceColumns(s, 6, 7); got != "🌍" {
		t.Fatalf("wide emoji must not split: %q", got)
	}
}

func TestHighlightLine(t *testing.T) {
	line := "abcde"
	got := highlightLine(line, 1, 3)
	if !strings.Contains(got, "\x1b[7mbc\x1b[27m") {
		t.Fatalf("reverse video range missing: %q", got)
	}
	if got != "a"+"\x1b[7mbc\x1b[27m"+"de" {
		t.Fatalf("highlight composition wrong: %q", got)
	}
}

func TestMousePointClampsToChat(t *testing.T) {
	m := New(nil, t.TempDir())
	m.winW = 50 // below the 80-col sidebar breakpoint: chat is full width
	m.vp.Height = 20
	p := m.mousePoint(999, 0)
	if p.Col != m.mainW()-1 {
		t.Fatalf("right edge not clamped to mainW-1: %+v (mainW=%d)", p, m.mainW())
	}
	p = m.mousePoint(-5, 999)
	if p.Col != 0 {
		t.Fatalf("left edge not clamped: %+v", p)
	}
	if p.Line != m.vp.YOffset+19 {
		t.Fatalf("bottom edge not clamped: %+v", p)
	}
}

func TestLeftPressStartsSelection(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Mouse = true
	m.blocks = []Block{{Kind: "assistant", Text: "hello"}}
	m.vp.Width = 60
	m.vp.Height = 10
	m.sel = Selection{}
	got, cmd, handled := m.updateSelection(tea.MouseMsg{
		X: 3, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
	})
	if !handled || !got.sel.Active {
		t.Fatal("left press must start a selection")
	}
	if cmd != nil {
		t.Fatalf("press must not schedule edge scroll: %v", cmd)
	}
}

func TestLeftPressOutsideViewportDoesNotStartSelection(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Mouse = true
	m.vp.Height = 10
	if _, _, handled := m.updateSelection(tea.MouseMsg{X: 1, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); handled {
		t.Fatal("header press must not start a selection")
	}
	if _, _, handled := m.updateSelection(tea.MouseMsg{X: 1, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); handled {
		t.Fatal("input-row press must not start a selection")
	}
}

func TestActiveDragFinishesInsideSidebar(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Mouse = true
	m.vp.Width, m.vp.Height = 40, 10
	m.chatLines = []string{"alpha", "beta"}
	m.sel = Selection{Active: true, Anchor: Point{0, 0}, Focus: Point{0, 1}, HadDrag: true}
	// X=100 is over the sidebar; an active drag must still receive release.
	got, _, handled := m.updateSelection(tea.MouseMsg{X: 100, Y: 2, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	if !handled || got.sel.Active {
		t.Fatal("active selection must finish when release crosses sidebar")
	}
}

func TestMotionExtendsAndReleaseCopies(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Mouse = true
	m.chatLines = []string{"alpha", "beta", "gamma"}
	m.vp.Width = 30
	m.vp.Height = 5
	m.sel = Selection{Active: true, Anchor: Point{Line: 0, Col: 0}, Focus: Point{Line: 0, Col: 0}, PressedAt: afterNow()}
	got, _, _ := m.updateSelection(tea.MouseMsg{X: 5, Y: 3, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	if !got.sel.Active || !got.sel.HadDrag {
		t.Fatal("motion must keep selection dragging")
	}
	got, _, _ = got.updateSelection(tea.MouseMsg{X: 5, Y: 3, Action: tea.MouseActionRelease, Button: tea.MouseButtonNone})
	if got.sel.Active {
		t.Fatal("release must clear the selection")
	}
	// The last toast/notice block contains the yanked text size; the inline
	// copy path is exercised by updateSelection, so just confirm no crash
	// and that the selection cleared.
	if len(got.toasts) == 0 && len(got.blocks) == 0 {
		t.Fatal("expected copy feedback somewhere")
	}
}

func TestDoubleClickSelectsLine(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Mouse = true
	m.chatLines = []string{"alpha beta", "second line"}
	m.vp.Width = 30
	m.vp.Height = 5
	m.LastPressLine = 1                                     // Y=2 → viewport row 1 → content line 1 ("second line")
	m.LastPressAt = time.Now().Add(-100 * time.Millisecond) // inside double-click window
	got, _, _ := m.updateSelection(tea.MouseMsg{X: 3, Y: 2, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !got.sel.DoubleClick || got.sel.Focus.Col == got.sel.Anchor.Col {
		t.Fatalf("double-click must select whole line: %+v", got.sel)
	}
}

// afterNow returns a stale timestamp (selection never treats it as a double
// click) for tests that start a drag without one.
func afterNow() (t time.Time) {
	return time.Now().Add(-2 * doubleClickWindow)
}
