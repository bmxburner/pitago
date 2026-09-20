package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestSidebarHelpers(t *testing.T) {
	if got := fmtDur(157 * time.Second); got != "2m37s" {
		t.Fatalf("fmtDur 157s = %q", got)
	}
	if got := fmtDur(5 * time.Second); got != "5s" {
		t.Fatalf("fmtDur 5s = %q", got)
	}
	if got := ctxBar(50, 10); got != "█████░░░░░" {
		t.Fatalf("ctxBar 50%% = %q", got)
	}
	row := twoCol("time 2m37s", "in 17k", 30)
	if w := lipgloss.Width(row); w != 17+len("in 17k") {
		t.Fatalf("twoCol width = %d: %q", w, row)
	}
}

// tallModel builds a sidebar whose content overflows a short terminal:
// 5 recents, 2 MCP servers, 12 todos (capped view), 5 workspace files.
func tallModel(t *testing.T) Model {
	t.Helper()
	m := New(nil, t.TempDir())
	m.Status = "ready"
	m.ModelLbl = "a"
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		m.recentModels = append(m.recentModels, RecentModel{ID: id})
	}
	m.MCP = []McpServer{{Name: "alpha", Connected: true, Direct: 1, Total: 2}, {Name: "beta"}}
	for i := 0; i < 12; i++ {
		m.Todos = append(m.Todos, TodoItem{ID: string(rune('a' + i)), Content: "task", Status: TodoPending})
	}
	m.ws = wsData{ok: true, branch: "main", files: []wsFile{
		{"a.go", 1, 0}, {"b.go", 2, 1}, {"c.go", 3, 0}, {"d.go", 0, 4}, {"e.go", 5, 5},
	}, more: 30, untracked: 2}
	return m
}

// Regression (screenshot): a tall sidebar must clip to its box — the whole
// frame stays exactly winH rows so it never pushes the chat input up.
func TestSidebarFitsHeight(t *testing.T) {
	m := tallModel(t)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = tm.(Model)
	lines := strings.Split(m.View(), "\n")
	if len(lines) != 24 {
		t.Fatalf("frame is %d rows, want 24", len(lines))
	}
	box := strings.Split(stripANSI(m.renderSidebar()), "\n")
	if len(box) != m.sideH() {
		t.Fatalf("sidebar box is %d rows, want %d", len(box), m.sideH())
	}
}

func wheelAt(x, y int, down bool) tea.MouseMsg {
	b := tea.MouseButtonWheelUp
	if down {
		b = tea.MouseButtonWheelDown
	}
	return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: b}
}

// Wheel over the sidebar scrolls the sidebar, not the chat; wheel over the
// chat leaves the sidebar alone.
func TestSidebarWheelScroll(t *testing.T) {
	m := tallModel(t)
	tm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m = tm.(Model)
	if m.sideVp.YOffset != 0 {
		t.Fatalf("start offset = %d, want 0", m.sideVp.YOffset)
	}
	sx := m.mainW() + 5
	tm, _ = m.Update(wheelAt(sx, 10, true))
	m = tm.(Model)
	if m.sideVp.YOffset <= 0 {
		t.Fatal("wheel over sidebar did not scroll it")
	}
	if m.vp.YOffset != 0 {
		t.Fatal("sidebar wheel must not scroll the chat")
	}
	off := m.sideVp.YOffset
	tm, _ = m.Update(wheelAt(10, 10, true)) // over the chat
	m = tm.(Model)
	if m.sideVp.YOffset != off {
		t.Fatal("chat wheel must not scroll the sidebar")
	}
}
