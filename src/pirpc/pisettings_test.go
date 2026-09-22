package pirpc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPiSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "settings.json")

	if err := SetPiSettingAt(file, "terminal.showImages", false); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := SetPiSettingAt(file, "enableSkillCommands", true); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := ReadPiSettingsAt(file)
	if PiBool(cfg, "terminal.showImages", true) {
		t.Error("showImages should be false")
	}
	if !PiBool(cfg, "enableSkillCommands", false) {
		t.Error("skillCommands should be true")
	}
	// absent keys fall back to pi defaults
	if PiBool(cfg, "images.blockImages", false) {
		t.Error("blockImages default should be false")
	}
	if got := PiInt(cfg, "terminal.imageWidthCells", 60); got != 60 {
		t.Errorf("imageWidth default should be 60, got %d", got)
	}
	if got := PiString(cfg, "transport", "auto"); got != "auto" {
		t.Errorf("transport default should be auto, got %q", got)
	}
	// existing keys survive a second write
	if err := SetPiSettingAt(file, "terminal.imageWidthCells", 120); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg = ReadPiSettingsAt(file)
	if PiBool(cfg, "terminal.showImages", true) {
		t.Error("showImages lost after second write")
	}
	if got := PiInt(cfg, "terminal.imageWidthCells", 60); got != 120 {
		t.Errorf("imageWidth should be 120, got %d", got)
	}
	if fi, err := os.Stat(file); err != nil || fi.Mode().Perm() != 0o644 {
		t.Errorf("new file should be 0644, got %v", fi.Mode())
	}
}
