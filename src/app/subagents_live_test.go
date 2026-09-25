package app

import (
	"encoding/json"
	"strings"
	"testing"

	"pitago/src/pirpc"
)

func TestSubagentLiveTracking(t *testing.T) {
	t.Setenv("PI_AGENT_DIR", t.TempDir())
	t.Setenv("PI_CODING_AGENT_DIR", "")
	m := New(nil, t.TempDir())
	feed := func(typ string, payload any) Model {
		t.Helper()
		raw, _ := json.Marshal(payload)
		tm, _ := m.Update(piEventMsg{pirpc.Event{Type: typ, Raw: raw}})
		m = tm.(Model)
		return m
	}
	m = feed("tool_execution_start", map[string]any{
		"toolCallId": "tc1", "toolName": "subagent",
		"args": map[string]any{"name": "scout", "agent": "explore", "task": "map auth", "interactive": false},
	})
	if len(m.Subagents) != 1 || m.Subagents[0].Name != "scout" || m.Subagents[0].Status != SubagentActive {
		t.Fatalf("after start: %+v", m.Subagents)
	}
	if !m.SideVisible(SideSubagents) {
		t.Fatal("section should auto-reveal")
	}
	sec := m.renderSubagentsSection(40)
	if !strings.Contains(sec, "scout") {
		t.Fatalf("sidebar missing row:\n%s", sec)
	}
	m = feed("tool_execution_end", map[string]any{
		"toolCallId": "tc1", "toolName": "subagent",
		"result":  map[string]any{"content": []any{map[string]any{"type": "text", "text": "mapped ok"}}},
		"isError": false,
	})
	if m.Subagents[0].Status != SubagentDone {
		t.Fatalf("after end: %+v", m.Subagents[0])
	}
	// interactive spawn stays active past tool end
	m = feed("tool_execution_start", map[string]any{
		"toolCallId": "tc2", "toolName": "subagent",
		"args": map[string]any{"name": "bg", "interactive": true},
	})
	m = feed("tool_execution_end", map[string]any{
		"toolCallId": "tc2", "toolName": "subagent",
		"result":  map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"id":"ab12"}`}}},
		"isError": false,
	})
	for _, r := range m.Subagents {
		if r.ID == "tc2" && r.Status != SubagentActive {
			t.Fatalf("interactive row should stay active: %+v", r)
		}
	}
}
