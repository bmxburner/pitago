package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

var errTest = errors.New("plannotator-tui exited with status 1")

// lastPlanGateResponse returns the decoded payload of the most recent
// extension_ui_response, or "" when nothing was sent.
func lastPlanGateResponse(t *testing.T, lines []string) (PlanGateResult, bool) {
	t.Helper()
	for i := len(lines) - 1; i >= 0; i-- {
		var cmd struct {
			Type  string `json:"type"`
			Value any    `json:"value"`
		}
		if err := json.Unmarshal([]byte(lines[i]), &cmd); err != nil {
			continue
		}
		if cmd.Type != "extension_ui_response" {
			continue
		}
		raw, ok := cmd.Value.(string)
		if !ok || !strings.HasPrefix(raw, "{") {
			continue // a real dialog answer, not ours
		}
		var out PlanGateResult
		if err := json.Unmarshal([]byte(raw), &out); err != nil {
			t.Fatalf("plan gate payload is not valid JSON (%v): %s", err, raw)
		}
		return out, true
	}
	return PlanGateResult{}, false
}
