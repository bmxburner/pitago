package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/components/mention"
)

func attachModel(t *testing.T) (Model, string) {
	t.Helper()
	dir := t.TempDir()
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, []byte("fake")...)
	if err := os.WriteFile(filepath.Join(dir, "shot.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	var m Model
	m.cwd = dir
	m.ta = textarea.New()
	m.ta.Focus()
	m.ta.SetWidth(80)
	m.tools = make(map[string]int)
	return m, dir
}

// Dropped absolute path collapses into a chip, text stays clean.
func TestCollectDrops(t *testing.T) {
	m, dir := attachModel(t)
	dropped := strings.ReplaceAll(filepath.Join(dir, "shot.png"), " ", `\ `)
	m.ta.SetValue("look " + dropped + " ok")
	m.collectDrops()
	if len(m.imgAtts) != 1 || m.imgAtts[0].label != 1 {
		t.Fatalf("tray = %+v", m.imgAtts)
	}
	if got := m.ta.Value(); strings.Contains(got, "shot.png") {
		t.Fatalf("path left in input: %q", got)
	}
	if m.chipH() != 1 {
		t.Fatal("chipH must reserve one row")
	}
	row := m.chipRow(80)
	if !strings.Contains(row, "[Image 1]") || !strings.Contains(row, "shot.png") {
		t.Fatalf("chip row = %q", row)
	}
}

// takeImages loads vision, clears the tray, keeps leftover @text working.
func TestTakeImages(t *testing.T) {
	m, _ := attachModel(t)
	m.attachPaths([]string{"shot.png"})
	imgs, notes := m.takeImages("plus @shot.png")
	if len(notes) != 0 {
		t.Fatalf("notes = %q", notes)
	}
	if len(imgs) != 2 {
		t.Fatalf("want tray + @ref = 2 images, got %d", len(imgs))
	}
	if len(m.imgAtts) != 0 || m.chipH() != 0 {
		t.Fatal("tray must clear on send")
	}
}

// Dropping several paths at once chips them all, in order.
func TestCollectDropsMulti(t *testing.T) {
	m, dir := attachModel(t)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	_ = os.WriteFile(filepath.Join(dir, "b.png"), png, 0o644)
	m.ta.SetValue(filepath.Join(dir, "shot.png") + " " + filepath.Join(dir, "b.png"))
	m.collectDrops()
	if len(m.imgAtts) != 2 || m.imgAtts[0].label != 1 || m.imgAtts[1].label != 2 {
		t.Fatalf("tray = %+v", m.imgAtts)
	}
	if got := strings.TrimSpace(m.ta.Value()); got != "" {
		t.Fatalf("input not clean: %q", got)
	}
}

// Tray navigation: ↓ enters, ←→ moves, ⌫ deletes the selected chip,
// Esc restores the input cursor, Enter sends tray-only messages.
func TestTrayNav(t *testing.T) {
	m, _ := attachModel(t)
	m.attachPaths([]string{"shot.png"})
	m.ta.SetValue("hi")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	if !m.trayFocus || m.imgCursor != 0 {
		t.Fatalf("focus=%v cursor=%d", m.trayFocus, m.imgCursor)
	}
	// typing exits focus and lands in the input
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")})
	m = nm.(Model)
	if m.trayFocus || !strings.HasSuffix(m.ta.Value(), "!") {
		t.Fatalf("typing must exit tray: focus=%v value=%q", m.trayFocus, m.ta.Value())
	}
	// re-enter, delete the chip, tray empties → back to input
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = nm.(Model)
	if len(m.imgAtts) != 0 || m.trayFocus {
		t.Fatalf("tray=%+v focus=%v", m.imgAtts, m.trayFocus)
	}
}

// Esc in the tray restores the exact input cursor.
func TestTrayEscRestores(t *testing.T) {
	m, _ := attachModel(t)
	m.attachPaths([]string{"shot.png"})
	m.ta.SetValue("hello")
	m.ta.SetCursor(2)
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = nm.(Model)
	if m.trayFocus {
		t.Fatal("Esc must leave the tray")
	}
	if row, col := m.cursorPos(); row != 0 || col != 2 {
		t.Fatalf("cursor = (%d,%d), want (0,2)", row, col)
	}
}

// Left/right moves between chips; ⌫ removes the middle one.
func TestTrayMoveDeleteMiddle(t *testing.T) {
	m, dir := attachModel(t)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	_ = os.WriteFile(filepath.Join(dir, "b.png"), png, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "c.png"), png, 0o644)
	m.attachPaths([]string{"shot.png", "b.png", "c.png"})
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown}) // selects last (c)
	m = nm.(Model)
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft}) // b
	m = nm.(Model)
	if m.imgCursor != 1 {
		t.Fatalf("cursor=%d want 1", m.imgCursor)
	}
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // delete b
	m = nm.(Model)
	if len(m.imgAtts) != 2 || m.imgAtts[0].name != "shot.png" || m.imgAtts[1].name != "c.png" {
		t.Fatalf("tray=%+v", m.imgAtts)
	}
	if !m.trayFocus {
		t.Fatal("focus must stay while chips remain")
	}
}

// Enter on tray-only input submits (images ride the RPC).
func TestTrayEnterSubmits(t *testing.T) {
	m, _ := attachModel(t)
	m.attachPaths([]string{"shot.png"})
	m.ta.SetValue("")
	if cmd := m.submitInput(); cmd == nil {
		t.Fatal("tray-only Enter must submit")
	}
}

// Pasted image data (screenshot in clipboard) becomes a chip.
func TestPasteImageData(t *testing.T) {
	old := clipRead
	clipRead = func() (string, error) { return "", nil }
	defer func() { clipRead = old }()
	m, dir := attachModel(t)
	oldImg := clipImage
	clipImage = func() (string, error) { return filepath.Join(dir, "shot.png"), nil }
	defer func() { clipImage = oldImg }()

	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if len(m.imgAtts) != 1 {
		t.Fatalf("tray=%+v", m.imgAtts)
	}
}
func TestBackspacePopsChip(t *testing.T) {
	m, _ := attachModel(t)
	m.attachPaths([]string{"shot.png"})
	m.ta.SetValue("")
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = nm.(Model)
	if len(m.imgAtts) != 0 {
		t.Fatalf("tray = %+v", m.imgAtts)
	}
	m.ta.SetValue("hi")
	nm, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = nm.(Model)
	if got := m.ta.Value(); got != "h" {
		t.Fatalf("textarea backspace broken: %q", got)
	}
}

// Tab-completing @image chips it and strips the @token (no double-send).
func TestCompleteAtChipsImage(t *testing.T) {
	m, _ := attachModel(t)
	m.ta.SetValue("@shot.png")
	m.atOpen = true
	m.atRow, m.atStart = 0, 0
	m.atItems = []mention.Item{{Value: "@shot.png", Label: "shot.png"}}
	m.completeAt()
	if len(m.imgAtts) != 1 {
		t.Fatalf("tray = %+v", m.imgAtts)
	}
	if got := strings.TrimSpace(m.ta.Value()); strings.Contains(got, "shot.png") {
		t.Fatalf("@token left in input: %q", got)
	}
}
func TestAttachDedupeCap(t *testing.T) {
	m, dir := attachModel(t)
	png := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	for _, n := range []string{"a.png", "b.png", "c.png", "d.png", "e.png", "f.png"} {
		_ = os.WriteFile(filepath.Join(dir, n), png, 0o644)
	}
	m.attachPaths([]string{"shot.png", "shot.png", "a.png", "b.png", "c.png", "d.png", "e.png", "f.png"})
	if len(m.imgAtts) != 5 {
		t.Fatalf("tray capped at 5, got %+v", m.imgAtts)
	}
	if len(m.blocks) == 0 {
		t.Fatal("cap overflow must leave a notice")
	}
}
