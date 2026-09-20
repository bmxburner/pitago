// Package recent persists the sidebar's recent-models list (provider may
// be "" for entries learned from a bare label; resolved via GetModels on
// switch). Pure file store, no TUI state.
package recent

import (
	"encoding/json"
	"os"
	"strings"
)

// MaxRecent caps the models kept in the sidebar.
const MaxRecent = 5

// RecentModel is one entry of the sidebar list.
type RecentModel struct {
	Provider string `json:"provider,omitempty"`
	ID       string `json:"id"`
	Label    string `json:"label,omitempty"`
}

// DispLabel is what the sidebar/dialog show.
func (r RecentModel) DispLabel() string {
	if r.Label != "" {
		return r.Label
	}
	return r.ID
}

// Load reads the persisted list (missing/corrupt → empty, no error).
func Load(path string) []RecentModel {
	var out []RecentModel
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(raw, &out)
	if len(out) > MaxRecent {
		out = out[:MaxRecent]
	}
	return out
}

// Save persists the list best-effort (dir 0700, file 0600).
func Save(path string, list []RecentModel) {
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

// Push moves (provider, id) to the front of list, dedupes, caps at MaxRecent.
func Push(list []RecentModel, provider, id, label string) []RecentModel {
	id = strings.TrimSpace(id)
	if id == "" {
		return list
	}
	provider = strings.TrimSpace(provider)
	label = strings.TrimSpace(label)
	out := make([]RecentModel, 0, MaxRecent)
	out = append(out, RecentModel{Provider: provider, ID: id, Label: label})
	for _, r := range list {
		if r.ID == id || (label != "" && (r.Label == label || r.DispLabel() == label)) {
			continue
		}
		out = append(out, r)
		if len(out) >= MaxRecent {
			break
		}
	}
	return out
}
