package pirpc

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func needPi(t *testing.T) {
	t.Helper()
	if bin := os.Getenv("PI_BIN"); bin != "" {
		return
	}
	if _, err := exec.LookPath("pi"); err != nil {
		t.Skip("pi not in PATH (set PI_BIN to run RPC tests)")
	}
}

func TestRPCNoLLM(t *testing.T) {
	needPi(t)
	c, err := Spawn(Options{NoSession: true})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer c.Close()

	state, err := c.GetState()
	if err != nil && strings.Contains(err.Error(), "timed out") {
		state, err = c.GetState() // pi cold start under load: one retry
	}
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

// Intentional Close must not emit pi_exited (reconnects would fake a
// "pi has exited" if the old pi's death notice lands after respawnMsg);
// an un-closed death still reports it.
func TestPiExitedSuppressedOnClose(t *testing.T) {
	fake := t.TempDir() + "/slowexit.sh"
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nsleep 0.5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := make(chan string, 4)
	// control: unexpected death reports pi_exited
	c, err := Spawn(Options{Bin: fake})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	c.OnEvent = func(e Event) { got <- e.Type }
	select {
	case typ := <-got:
		if typ != "pi_exited" {
			t.Fatalf("unexpected event %q", typ)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expected pi_exited for unexpected death")
	}
	// intentional close: no event
	c2, err := Spawn(Options{Bin: fake})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	c2.OnEvent = func(e Event) { got <- e.Type }
	c2.Close()
	select {
	case typ := <-got:
		t.Fatalf("closed client must stay silent, got %q", typ)
	case <-time.After(2 * time.Second):
	}
}
