package pirpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Pi agent settings (~/.pi/agent/settings.json) read/write with dotted
// paths ("terminal.showImages"). Mirrors pi's own keys so /settings has
// parity with stock pi: file-backed rows write here, then pitago respawns
// the pi child to pick them up (same as the /login reconnect flow).

// PiAgentDir mirrors pi's getAgentDir: $PI_CODING_AGENT_DIR or ~/.pi/agent.
func PiAgentDir() string {
	if d := os.Getenv("PI_CODING_AGENT_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent")
}

// PiSettingsPath is <agentDir>/settings.json ("" when unresolvable).
func PiSettingsPath() string {
	if d := PiAgentDir(); d != "" {
		return filepath.Join(d, "settings.json")
	}
	return ""
}

// ReadPiSettings loads settings.json (nil when missing/unparseable).
func ReadPiSettings() map[string]any {
	return ReadPiSettingsAt(PiSettingsPath())
}

// ReadPiSettingsAt loads one settings file (nil when missing/unparseable).
func ReadPiSettingsAt(path string) map[string]any {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

// GetPiSetting reads a dotted path ("terminal.showImages") from cfg.
func GetPiSetting(cfg map[string]any, path string) any {
	var cur any = cfg
	for _, k := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur, ok = m[k]
		if !ok {
			return nil
		}
	}
	return cur
}

// SetPiSetting writes one dotted path into settings.json, creating nested
// maps as needed and preserving the file's existing keys and permissions
// (0644 for a new file, like pi).
func SetPiSetting(path string, val any) error {
	return SetPiSettingAt(PiSettingsPath(), path, val)
}

// SetPiSettingAt writes one dotted path into the given settings file.
func SetPiSettingAt(file, path string, val any) error {
	if file == "" {
		return os.ErrNotExist
	}
	cfg := ReadPiSettingsAt(file)
	if cfg == nil {
		cfg = map[string]any{}
	}
	keys := strings.Split(path, ".")
	m := cfg
	for _, k := range keys[:len(keys)-1] {
		next, ok := m[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			m[k] = next
		}
		m = next
	}
	m[keys[len(keys)-1]] = val
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if fi, err := os.Stat(file); err == nil {
		perm = fi.Mode().Perm()
	} else if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	return os.WriteFile(file, append(raw, '\n'), perm)
}

// PiBool reads a bool with pi's default when absent/wrong type.
func PiBool(cfg map[string]any, path string, def bool) bool {
	if v, ok := GetPiSetting(cfg, path).(bool); ok {
		return v
	}
	return def
}

// PiString reads a string with pi's default when absent/wrong type.
func PiString(cfg map[string]any, path string, def string) string {
	if v, ok := GetPiSetting(cfg, path).(string); ok && v != "" {
		return v
	}
	return def
}

// PiInt reads a number (JSON float64) with pi's default when absent.
func PiInt(cfg map[string]any, path string, def int) int {
	switch v := GetPiSetting(cfg, path).(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return def
}
