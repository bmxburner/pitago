package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPushRecent(t *testing.T) {
	var m Model // recentPath "" → no file writes
	m.pushRecent("anthropic", "claude-a", "claude-a")
	m.pushRecent("openai", "gpt-b", "gpt-b")
	m.pushRecent("", "claude-a", "claude-a") // dup by id moves to front
	if len(m.recentModels) != 2 {
		t.Fatalf("want 2, got %d", len(m.recentModels))
	}
	if m.recentModels[0].ID != "claude-a" {
		t.Fatalf("want claude-a first, got %+v", m.recentModels[0])
	}
	m.pushRecent("p", "", "x") // empty id ignored
	if len(m.recentModels) != 2 {
		t.Fatalf("empty id must be ignored, got %d", len(m.recentModels))
	}
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5", "m6"} {
		m.pushRecent("p", id, id)
	}
	if len(m.recentModels) != maxRecent {
		t.Fatalf("want cap %d, got %d", maxRecent, len(m.recentModels))
	}
	if m.recentModels[0].ID != "m6" || m.recentModels[len(m.recentModels)-1].ID != "m2" {
		t.Fatalf("cap window wrong: %+v", m.recentModels)
	}
}

func TestRecentAt(t *testing.T) {
	m := Model{winW: 120, winH: 30, ready: true} // mainW=81, sidebar x>=82
	m.recentModels = []RecentModel{{ID: "a"}, {ID: "b"}}
	m.ModelLbl = "a"
	y0 := 1 + m.recentContentRow() // first model row
	if idx, ok := m.recentAt(82, y0); !ok || idx != 0 {
		t.Fatalf("row 0: got %d,%v", idx, ok)
	}
	if idx, ok := m.recentAt(100, y0+1); !ok || idx != 1 {
		t.Fatalf("row 1: got %d,%v", idx, ok)
	}
	for _, pt := range [][2]int{{10, y0}, {82, y0 - 1}, {82, y0 + 2}, {82, y0 + 50}} {
		if _, ok := m.recentAt(pt[0], pt[1]); ok {
			t.Fatalf("point %v must miss", pt)
		}
	}
	m.ready = false
	if _, ok := m.recentAt(82, y0); ok {
		t.Fatal("not ready must miss")
	}
}

// Regression: recentAt must hit the rows renderSidebar actually draws.
// The hardcoded offset once ignored the PET section, so clicks landed
// 3 rows high (wrong model, or a miss with few recents).
func TestRecentAtMatchesRender(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Status = "ready"
	m.recentModels = []RecentModel{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}, {ID: "e"}}
	m.ModelLbl = "a"
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = tm.(Model)
	lines := strings.Split(stripANSI(m.renderSidebar()), "\n")
	head := -1
	for i, ln := range lines {
		if strings.Contains(ln, "RECENT MODELS") {
			head = i
			break
		}
	}
	if head < 0 {
		t.Fatal("rendered sidebar has no RECENT MODELS header")
	}
	x := m.mainW() + 1
	// output line 0 is the box top border, so content row = line-1 and
	// screen y = 1 (border) + content row.
	at := func(y, want int) {
		t.Helper()
		if idx, ok := m.recentAt(x, y); !ok || idx != want {
			t.Fatalf("y=%d: got %d,%v, want %d", y, idx, ok, want)
		}
	}
	at(1+head, 0)
	at(1+head+1, 1)
	// scrolled 2 rows down: the same screen rows map 2 models further
	m.sideVp.SetYOffset(2)
	if idx, ok := m.recentAt(x, 1+head); !ok || idx != 2 {
		t.Fatalf("scrolled y=%d: got %d,%v, want 2", 1+head, idx, ok)
	}
}
