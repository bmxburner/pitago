package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"pitago/src/pirpc"
)

// feedToolEvent pushes one raw tool event through handleEvent and returns
// the model it produced. Mirrors the live RPC path: pirpc.Event carries the
// decoded Type plus the untouched JSON line.
func feedToolEvent(t *testing.T, m Model, typ, raw string) Model {
	t.Helper()
	um, _ := m.handleEvent(pirpc.Event{Type: typ, Raw: json.RawMessage(raw)})
	return um.(Model)
}

// nestedModel is a Model with the maps handleEvent writes into already
// allocated (a zero Model's nil tools map panics on the first assignment).
func nestedModel() Model {
	return Model{tools: make(map[string]int), blocks: nil}
}

// A parented codemode call must NOT create its own top-level block — that
// ungrouped render is the bug being fixed. The parent's NestedCalls must
// carry the child instead.
func TestNestedToolCallGroupsUnderParent(t *testing.T) {
	m := nestedModel()
	// The parent codemode call, as any ordinary tool call.
	m = feedToolEvent(t, m, "tool_execution_start",
		`{"toolCallId":"cm_1","toolName":"codemode","args":{"language":"javascript","code":"return 1"}}`)
	if len(m.blocks) != 1 {
		t.Fatalf("parent start must make 1 block, got %d", len(m.blocks))
	}

	before := len(m.blocks)
	// The script's own call, carrying parentToolCallId.
	m = feedToolEvent(t, m, "tool_execution_start",
		`{"toolCallId":"cm_1/0","toolName":"bash","args":{"command":"ls"},"parentToolCallId":"cm_1"}`)
	if len(m.blocks) != before {
		t.Fatalf("parented start must not add a block: %d -> %d", before, len(m.blocks))
	}
	if len(m.blocks[0].NestedCalls) != 1 {
		t.Fatalf("parent must hold 1 nested call, got %d", len(m.blocks[0].NestedCalls))
	}
	nc := m.blocks[0].NestedCalls[0]
	if nc.ID != "cm_1/0" || nc.Name != "bash" {
		t.Errorf("nested call id/name wrong: %+v", nc)
	}
	// pi's wire vocabulary is unfinished|ok|error, NOT "running".
	if nc.Status != "unfinished" {
		t.Errorf("live nested call status = %q, want unfinished", nc.Status)
	}
	if nc.Arguments != `{"command":"ls"}` {
		t.Errorf("arguments = %q", nc.Arguments)
	}

	// A second phase of the SAME call updates in place, not a second row.
	m = feedToolEvent(t, m, "tool_execution_end",
		`{"toolCallId":"cm_1/0","toolName":"bash","isError":false,
		  "result":{"content":[{"type":"text","text":"file.go\n"}]},"parentToolCallId":"cm_1"}`)
	if len(m.blocks[0].NestedCalls) != 1 {
		t.Fatalf("end must update in place, got %d rows", len(m.blocks[0].NestedCalls))
	}
	if s := m.blocks[0].NestedCalls[0].Status; s != "ok" {
		t.Errorf("finished nested call status = %q, want ok", s)
	}

	// A second, distinct call appends its own row.
	m = feedToolEvent(t, m, "tool_execution_end",
		`{"toolCallId":"cm_1/1","toolName":"read","isError":false,
		  "result":{"content":[]},"parentToolCallId":"cm_1"}`)
	if len(m.blocks[0].NestedCalls) != 2 {
		t.Fatalf("distinct call must append, got %d rows", len(m.blocks[0].NestedCalls))
	}
}

// A failing nested call records the error text so the row can show it.
func TestNestedToolCallError(t *testing.T) {
	m := nestedModel()
	m = feedToolEvent(t, m, "tool_execution_start", `{"toolCallId":"p","toolName":"codemode"}`)
	m = feedToolEvent(t, m, "tool_execution_end",
		`{"toolCallId":"p/0","toolName":"bash","isError":true,
		  "result":{"content":[{"type":"text","text":"command not found: nope"}]},
		  "parentToolCallId":"p"}`)
	nc := m.blocks[0].NestedCalls[0]
	if nc.Status != "error" {
		t.Errorf("status = %q, want error", nc.Status)
	}
	if !strings.Contains(nc.Error, "command not found") {
		t.Errorf("error text not recorded: %q", nc.Error)
	}
}

// pi caps a nested error at 500 chars; so must we, or one huge failure
// blows out the row and the render cache.
func TestNestedErrorCapped(t *testing.T) {
	m := nestedModel()
	m = feedToolEvent(t, m, "tool_execution_start", `{"toolCallId":"p","toolName":"codemode"}`)
	long := strings.Repeat("x", 4000)
	m = feedToolEvent(t, m, "tool_execution_end",
		`{"toolCallId":"p/0","toolName":"bash","isError":true,
		  "result":{"content":[{"type":"text","text":"`+long+`"}]},
		  "parentToolCallId":"p"}`)
	got := m.blocks[0].NestedCalls[0].Error
	if len([]rune(got)) > 501 {
		t.Errorf("error not capped: %d runes", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("cap must mark the cut, got tail %q", got[len(got)-10:])
	}
}

// Trap 3: arguments are NOT always present — pi omits them past its byte
// budget and sets argumentsBytes. The renderer must be able to say so, so
// the bytes field has to survive verbatim.
func TestNestedArgumentsBytesPreserved(t *testing.T) {
	m := nestedModel()
	m = feedToolEvent(t, m, "tool_execution_start", `{"toolCallId":"p","toolName":"codemode"}`)
	m = feedToolEvent(t, m, "tool_execution_start",
		`{"toolCallId":"p/0","toolName":"write","parentToolCallId":"p","argumentsBytes":4192}`)
	nc := m.blocks[0].NestedCalls[0]
	if nc.ArgumentsBytes != 4192 {
		t.Errorf("argumentsBytes = %d, want 4192", nc.ArgumentsBytes)
	}
	if nc.Arguments != "" {
		t.Errorf("arguments should be absent, got %q", nc.Arguments)
	}
}

// A nested event whose parent is unknown must still fall through to the
// ordinary path rather than being silently dropped.
func TestNestedUnknownParentFallsBack(t *testing.T) {
	m := nestedModel()
	before := len(m.blocks)
	m = feedToolEvent(t, m, "tool_execution_start",
		`{"toolCallId":"cm_9/0","toolName":"bash","parentToolCallId":"cm_9"}`)
	if len(m.blocks) != before+1 {
		t.Fatalf("unknown-parent call must still create a block, got %d", len(m.blocks))
	}
	if m.blocks[before].Kind != "tool" || m.blocks[before].ToolCallID != "cm_9/0" {
		t.Errorf("fallback block wrong: %+v", m.blocks[before])
	}
}

// restore() must fill NestedCalls from the persisted parent toolResult —
// pi never persists nested calls as their own transcript messages, so the
// summary on the parent is the ONLY resume source. It must not overwrite a
// list the live event tail already built.
func TestRestoreFillsNestedCalls(t *testing.T) {
	m := nestedModel()
	// A toolResult row carrying the codemode nestedCalls summary.
	msgs := []pirpc.AgentMessage{
		{Role: "assistant", Content: json.RawMessage(
			`[{"type":"toolCall","id":"cm_1","name":"codemode","arguments":{}}]`)},
		{Role: "toolResult", ToolCallID: "cm_1", ToolName: "codemode",
			Content: json.RawMessage(`[{"type":"text","text":"Script completed"}]`),
			NestedCalls: &pirpc.NestedCalls{
				Calls: []pirpc.NestedCall{
					{ID: "cm_1/0", Name: "bash", Status: "ok", DurationMs: 120,
						Arguments: json.RawMessage(`{"command":"ls"}`)},
					{ID: "cm_1/1", Name: "write", Status: "error",
						Error: "boom", ArgumentsBytes: 9000},
				},
			}},
	}
	m.restore(msgs)
	if len(m.blocks) == 0 {
		t.Fatal("restore produced no blocks")
	}
	var parent *Block
	for i := range m.blocks {
		if m.blocks[i].Kind == "tool" && m.blocks[i].ToolCallID == "cm_1" {
			parent = &m.blocks[i]
		}
	}
	if parent == nil {
		t.Fatal("parent codemode block missing after restore")
	}
	if len(parent.NestedCalls) != 2 {
		t.Fatalf("restored nested calls = %d, want 2", len(parent.NestedCalls))
	}
	if parent.NestedCalls[0].DurationMs != 120 || parent.NestedCalls[0].Arguments == "" {
		t.Errorf("duration/arguments lost in restore: %+v", parent.NestedCalls[0])
	}
	if parent.NestedCalls[1].ArgumentsBytes != 9000 || parent.NestedCalls[1].Status != "error" {
		t.Errorf("argumentsBytes/status lost in restore: %+v", parent.NestedCalls[1])
	}
}

// A list already built from the live event tail wins over the restore copy.
func TestRestoreDoesNotOverwriteLiveNestedCalls(t *testing.T) {
	m := nestedModel()
	m = feedToolEvent(t, m, "tool_execution_start", `{"toolCallId":"cm_1","toolName":"codemode"}`)
	m = feedToolEvent(t, m, "tool_execution_end",
		`{"toolCallId":"cm_1/0","toolName":"bash","isError":false,
		  "result":{"content":[]},"parentToolCallId":"cm_1"}`)

	m.restore([]pirpc.AgentMessage{
		{Role: "assistant", Content: json.RawMessage(
			`[{"type":"toolCall","id":"cm_1","name":"codemode","arguments":{}}]`)},
		{Role: "toolResult", ToolCallID: "cm_1", ToolName: "codemode",
			Content: json.RawMessage(`[{"type":"text","text":"ok"}]`),
			NestedCalls: &pirpc.NestedCalls{
				Calls: []pirpc.NestedCall{
					{ID: "cm_1/0", Name: "bash", Status: "ok"},
					{ID: "cm_1/1", Name: "read", Status: "ok"},
				},
			}},
	})
	// The live tail saw exactly one child; restore must not add the second.
	if got := len(m.blocks[0].NestedCalls); got != 1 {
		t.Errorf("restore overwrote the live list: %d rows, want 1", got)
	}
}

// A nested row must never be created against a non-tool block (the parent
// index guard). Falls back to the ordinary path instead of corrupting state.
func TestNestedParentMustBeToolBlock(t *testing.T) {
	m := nestedModel()
	m.AddBlock(Block{Kind: "assistant", Text: "hi"})
	// Register a non-tool block under a call id the way m.tools might get
	// poisoned, then send a parented event against it.
	m.tools["asm_1"] = 0
	before := len(m.blocks)
	m = feedToolEvent(t, m, "tool_execution_start",
		`{"toolCallId":"asm_1/0","toolName":"bash","parentToolCallId":"asm_1"}`)
	if len(m.blocks) != before+1 {
		t.Fatalf("non-tool parent must fall through to a new block, got %d", len(m.blocks))
	}
	if len(m.blocks[0].NestedCalls) != 0 {
		t.Errorf("non-tool block must not collect nested calls")
	}
}

// The live list stops at pi's own record bound. pi drops the overflow and
// marks its persisted record incomplete, so growing without limit would
// both diverge from what pi stored and hash an unbounded number of rows on
// every render frame.
func TestNestedListStopsAtPiRecordBound(t *testing.T) {
	m := nestedModel()
	m = feedToolEvent(t, m, "tool_execution_start",
		`{"toolCallId":"cm_1","toolName":"codemode","args":{"code":"1"}}`)

	over := pirpc.NestedCallsMaxCalls + 25
	for i := 0; i < over; i++ {
		m = feedToolEvent(t, m, "tool_execution_start", fmt.Sprintf(
			`{"toolCallId":"cm_1/%d","toolName":"grep","args":{"p":%d},"parentToolCallId":"cm_1"}`, i, i))
	}
	got := len(m.blocks[0].NestedCalls)
	if got != pirpc.NestedCallsMaxCalls {
		t.Fatalf("nested list = %d rows, want pi's bound %d", got, pirpc.NestedCallsMaxCalls)
	}
	// The call that WAS recorded must still be updateable — the cap drops
	// new rows, it must not freeze the ones already there.
	m = feedToolEvent(t, m, "tool_execution_end",
		`{"toolCallId":"cm_1/0","toolName":"grep","isError":false,"parentToolCallId":"cm_1",
		  "result":{"content":[{"type":"text","text":"hit"}]}}`)
	if got := len(m.blocks[0].NestedCalls); got != pirpc.NestedCallsMaxCalls {
		t.Fatalf("cap changed on update: %d rows", got)
	}
	if s := m.blocks[0].NestedCalls[0].Status; s != "ok" {
		t.Errorf("recorded row stopped updating past the cap: status = %q", s)
	}
}
