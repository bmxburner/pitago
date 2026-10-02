package app

// nested_calls_e2e_test.go — nested tool calls over the REAL RPC stream.
//
// Everything else about nested calls is unit-tested by feeding synthetic
// JSON into handleEvent. That proves the decoder and the routing, but it
// cannot prove the wire: that pi's actual tool_execution_* frames, as
// pirpc's readLoop passes them through, carry parentToolCallId and reach
// handleEvent still carrying it. Dropping one field in the transport
// (client.go), or a field rename in pi, would leave every synthetic test
// green and the feature broken.
//
// So these tests spawn the real child process (tests/fakepi, the same
// stdlib binary script/test-wire.sh puts on PI_BIN) and drive the whole
// path: child stdout -> pirpc.Event -> handleEvent -> Model.blocks. The
// fake's codemode scenario is transcribed from pi's own contract — see
// codemodeFixtures in tests/fakepi/main.go for the citations.

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"pitago/src/pirpc"
)

// eventSink collects the child's events from the client's reader goroutine.
// The callback and the test read the same slice, so every access is guarded:
// the CI gate runs -race, and an unguarded read here would be a real race
// rather than a theoretical one.
type eventSink struct {
	mu  sync.Mutex
	evs []pirpc.Event
}

func (s *eventSink) add(ev pirpc.Event) {
	s.mu.Lock()
	s.evs = append(s.evs, ev)
	s.mu.Unlock()
}

// snapshot returns the events collected so far, safe to read at any time.
func (s *eventSink) snapshot() []pirpc.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]pirpc.Event(nil), s.evs...)
}

// waitEvents waits until a message_end has arrived — the fake writes it last
// for this scenario — and returns everything collected. Asserting on a
// prefix that happens to pass is the failure mode this avoids.
func waitEvents(t *testing.T, s *eventSink) []pirpc.Event {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var done bool
		for _, ev := range s.snapshot() {
			if ev.Type == "message_end" {
				done = true
			}
		}
		if done {
			// Give the reader one more turn to hand over anything queued
			// behind message_end before the burst is frozen.
			time.Sleep(200 * time.Millisecond)
			return s.snapshot()
		}
		if time.Now().After(deadline) {
			t.Fatalf("no message_end from the fake within the window; got %d events", len(s.snapshot()))
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// hasParentField reports whether a raw tool event really carries
// parentToolCallId on the wire. Asserted rather than assumed: if the
// transport or a pi rename drops it, every synthetic test still passes and
// the feature is simply dead.
func hasParentField(raw json.RawMessage) bool {
	var probe struct {
		ParentToolCallID string `json:"parentToolCallId"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.ParentToolCallID != ""
}

// containsAll reports whether s contains every needle — for asserting a
// rendered row degrades visibly instead of going blank.
func containsAll(s string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(s, n) {
			return false
		}
	}
	return true
}

// The live path, end to end: a codemode script's three nested calls must
// land as children of ONE tool block, not as four top-level blocks.
func TestNestedCallsOverLiveRPCStream(t *testing.T) {
	c, _ := spawnProbeFakePi(t, map[string]string{"FAKEPI_SCENARIO": "codemode"})
	sink := &eventSink{}
	c.SetOnEvent(sink.add)
	if _, err := c.Prompt("count the files"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	events := waitEvents(t, sink)

	m := nestedModel()
	sawParent := false
	for _, ev := range events {
		if ev.Type != "tool_execution_start" &&
			ev.Type != "tool_execution_update" &&
			ev.Type != "tool_execution_end" {
			continue
		}
		// The parentToolCallId must survive the real transport. Without it
		// the events below are indistinguishable from model-issued calls.
		if ev.Type == "tool_execution_start" {
			if hasParentField(ev.Raw) {
				sawParent = true
			}
		}
		um, _ := m.handleEvent(ev)
		m = um.(Model)
	}
	if !sawParent {
		t.Fatal("no event carried parentToolCallId: the wire shape changed")
	}

	if len(m.blocks) != 1 {
		var names []string
		for _, b := range m.blocks {
			names = append(names, b.ToolName)
		}
		t.Fatalf("nested calls must not become top-level blocks: got %d %v", len(m.blocks), names)
	}
	bl := m.blocks[0]
	if bl.ToolName != "codemode" {
		t.Fatalf("block is %q, want codemode", bl.ToolName)
	}
	if len(bl.NestedCalls) != 3 {
		t.Fatalf("want 3 nested calls, got %d: %+v", len(bl.NestedCalls), bl.NestedCalls)
	}

	// Every terminal status pi can report, from the live path.
	want := []struct {
		name, status, errText string
	}{
		{"read", "ok", ""},
		{"bash", "error", "FAIL: cannot find module 'x'"},
		{"write", "unfinished", ""}, // still running when the script returned
	}
	for i, w := range want {
		got := bl.NestedCalls[i]
		if got.Name != w.name || got.Status != w.status {
			t.Errorf("nested[%d] = %s/%s, want %s/%s", i, got.Name, got.Status, w.name, w.status)
		}
		if w.errText != "" && got.Error == "" {
			t.Errorf("nested[%d] (%s) lost its error text: %+v", i, w.name, got)
		}
	}

	// The nested ids must never enter m.tools: that map also drives restore
	// matching, subagent tracking, the LSP and side panels, and the render
	// cache. A nested id in there would make all five chase a block that
	// does not exist.
	for id := range m.tools {
		for _, nc := range bl.NestedCalls {
			if id == nc.ID {
				t.Errorf("nested id %q leaked into m.tools", id)
			}
		}
	}

	// A live event never carries durationMs or argumentsBytes (both are
	// persisted-only, NestedToolCallRecord), so neither may be invented.
	for _, nc := range bl.NestedCalls {
		if nc.DurationMs != 0 || nc.ArgumentsBytes != 0 {
			t.Errorf("live row invented persisted-only fields: %+v", nc)
		}
		if nc.Arguments == "" {
			t.Errorf("live row lost its args: %+v", nc)
		}
	}
}

// Resume, end to end: get_messages returns the parent's nestedCalls record
// and restore() must attach it to the parent's block. This is the path the
// unit test covers only by construction, not by transport.
func TestNestedCallsRestoreOverLiveRPC(t *testing.T) {
	c, _ := spawnProbeFakePi(t, map[string]string{"FAKEPI_SCENARIO": "codemode"})
	msgs, err := c.GetMessages()
	if err != nil {
		t.Fatalf("get_messages: %v", err)
	}

	m := nestedModel()
	m.restore(msgs)

	var parent *Block
	for i := range m.blocks {
		if m.blocks[i].ToolName == "codemode" {
			parent = &m.blocks[i]
		}
	}
	if parent == nil {
		t.Fatalf("no codemode block restored: %d blocks", len(m.blocks))
	}
	if len(parent.NestedCalls) != 3 {
		t.Fatalf("want 3 restored nested calls, got %d: %+v", len(parent.NestedCalls), parent.NestedCalls)
	}

	// durationMs and argumentsBytes exist ONLY on the persisted record —
	// this is the one path where they must survive the wire.
	byName := map[string]NestedCall{}
	for _, nc := range parent.NestedCalls {
		byName[nc.Name] = nc
	}
	if d := byName["read"].DurationMs; d != 12 {
		t.Errorf("read durationMs = %d, want 12", d)
	}
	if b := byName["read"].Arguments; b == "" {
		t.Error("read lost its arguments")
	}
	if e := byName["bash"].Error; e == "" {
		t.Error("bash lost its error text")
	}
	if s := byName["write"].Status; s != "unfinished" {
		t.Errorf("write status = %q, want unfinished", s)
	}

	// THE degradation this whole fixture exists for: pi omits arguments
	// past its 8 KiB per-call budget and sets argumentsBytes instead. If the
	// decoder drops that, the row renders blank — a silent hole in the audit
	// trail, which is the one thing grouping must never lose.
	omitted := byName["bash"]
	if omitted.Arguments != "" {
		t.Errorf("bash should have NO arguments (pi omitted them), got %q", omitted.Arguments)
	}
	if omitted.ArgumentsBytes != 4192 {
		t.Errorf("bash argumentsBytes = %d, want 4192", omitted.ArgumentsBytes)
	}
	// And the row must actually say so when rendered.
	if r := renderNestedCalls(*parent, 60); r == "" {
		t.Error("nested rows rendered nothing for a restored record")
	} else if !containsAll(r, "omitted", "4.1") {
		t.Errorf("omitted-arguments row does not degrade visibly:\n%s", r)
	}
}
