package pirpc

import (
	"testing"
	"time"
)

func TestRPCNoLLM(t *testing.T) {
	c, err := Spawn(Options{NoSession: true})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer c.Close()

	state, err := c.GetState()
	if err != nil {
		t.Fatalf("get_state: %v", err)
	}
	t.Logf("model=%s provider=%s session=%s", state.Model.ID, state.Model.Provider, state.SessionID)

	resp, err := c.Send(Command{Type: "get_available_models"}, 30*time.Second)
	if err != nil {
		t.Fatalf("get_available_models: %v", err)
	}
	t.Logf("models payload bytes=%d", len(resp.Data))

	resp, err = c.Send(Command{Type: "get_commands"}, 30*time.Second)
	if err != nil {
		t.Fatalf("get_commands: %v", err)
	}
	t.Logf("commands payload bytes=%d", len(resp.Data))

	stats, err := c.GetStats()
	if err != nil {
		t.Fatalf("get_session_stats: %v", err)
	}
	t.Logf("stats session=%s tools=%d", stats.SessionID, stats.ToolCalls)
}
