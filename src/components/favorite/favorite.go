// Package favorite persists the model picker's starred models.
// Pure file store, no TUI state — same shape as components/recent.
package favorite

import (
	"encoding/json"
	"os"
	"strings"
)

// Fav is one starred model.
type Fav struct {
	Provider string `json:"provider,omitempty"`
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
}

// Key identifies a model across providers.
func Key(provider, id string) string {
	return strings.TrimSpace(provider) + "\x00" + strings.TrimSpace(id)
}

// Set builds the lookup used for sort + star rendering.
func Set(list []Fav) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, f := range list {
		out[Key(f.Provider, f.ID)] = true
	}
	return out
}

// Load reads the persisted list (missing/corrupt → empty, no error).
func Load(path string) []Fav {
	var out []Fav
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(raw, &out)
	return out
}

// Save persists the list best-effort (dir 0700, file 0600).
func Save(path string, list []Fav) {
	if path == "" {
		return
	}
	if err := os.MkdirAll(dirOf(path), 0o700); err != nil {
		return
	}
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, raw, 0o600)
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

// Toggle stars (provider, id) when absent, unstars when present.
// Returns the new list + true when the model is now starred.
func Toggle(list []Fav, provider, id, label string) ([]Fav, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return list, false
	}
	provider = strings.TrimSpace(provider)
	label = strings.TrimSpace(label)
	k := Key(provider, id)
	for i, f := range list {
		if Key(f.Provider, f.ID) == k {
			return append(list[:i:i], list[i+1:]...), false
		}
	}
	return append([]Fav{{Provider: provider, ID: id, Label: label}}, list...), true
}
