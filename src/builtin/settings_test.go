package builtin

import (
	"strings"
	"testing"

	"pitago/src/app"
)

// Pi parity: 7 live rows + 14 file/local rows (image block first, like the
// stock pi screenshot: skill commands, show images, image width, ...).
func TestSettingsOptionsPiParity(t *testing.T) {
	st := app.SettingsState{
		Model: "m", Thinking: "high", Steering: "all", FollowUp: "all",
		AutoCompact: true, AutoRetry: true, Theme: "default",
		Vals: map[string]string{
			"enableSkillCommands": "on", "terminal.showImages": "on",
			"terminal.imageWidthCells": "60", "images.autoResize": "on",
			"images.blockImages": "off", "transport": "auto",
			"httpIdleTimeoutMs": "5 min", "cacheWarming": "streaming",
			"hideThinkingBlock": "off", "showCacheMissNotices": "off",
			"defaultProjectTrust": "Ask", "quietStartup": "off",
			"enableInstallTelemetry": "off",
		},
		HideThinking: false, AutocompleteMax: 10,
	}
	opts, descs := settingsOptions(st)
	if len(opts) != 7+len(fileSettings) {
		t.Fatalf("expected %d rows, got %d", 7+len(fileSettings), len(opts))
	}
	for i, want := range []string{"Skill commands: on", "Show images: on", "Image width: 60",
		"Auto-resize images: on", "Block images: off"} {
		if opts[7+i] != want {
			t.Errorf("row %d: expected %q, got %q", 7+i, want, opts[7+i])
		}
	}
	if !strings.Contains(descs[7], "reconnects pi") {
		t.Errorf("file rows should warn about reconnect, got %q", descs[7])
	}
}

func TestNextValWraps(t *testing.T) {
	if nextVal([]string{"60", "80", "120"}, "120") != "60" {
		t.Error("should wrap to first")
	}
	if nextVal([]string{"on", "off"}, "bogus") != "on" {
		t.Error("unknown current should restart at first")
	}
}

func TestFileSettingValsDefaults(t *testing.T) {
	vals := fileSettingVals(nil) // missing file → pi defaults
	for path, want := range map[string]string{
		"enableSkillCommands": "on", "terminal.showImages": "on",
		"terminal.imageWidthCells": "60", "images.autoResize": "on",
		"images.blockImages": "off", "transport": "auto",
		"httpIdleTimeoutMs": "5 min", "cacheWarming": "streaming",
		"defaultProjectTrust": "Ask",
	} {
		if vals[path] != want {
			t.Errorf("%s: expected pi default %q, got %q", path, want, vals[path])
		}
	}
}
