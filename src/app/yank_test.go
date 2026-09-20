package app

import (
	"testing"

	"pitago/src/components/yank"
)

func TestLastAssistantText(t *testing.T) {
	blocks := []Block{
		{Kind: "user", Text: "hi"},
		{Kind: "assistant", Text: "first"},
		{Kind: "tool", ToolName: "bash"},
		{Kind: "assistant", Text: "  "}, // blank skipped
		{Kind: "assistant", Text: "latest"},
		{Kind: "notice", Text: "yanked…"},
	}
	if got := yank.LastAssistantText(blocks); got != "latest" {
		t.Errorf("got %q, want latest", got)
	}
	if got := yank.LastAssistantText(nil); got != "" {
		t.Errorf("empty blocks should yield empty, got %q", got)
	}
}

func TestToggleSideFlipsAndReflows(t *testing.T) {
	m := Model{winW: 120, winH: 30, ready: false}
	if !m.showSide() {
		t.Fatal("sidebar should show at 120 cols by default")
	}
	m.ToggleSide()
	if m.showSide() {
		t.Error("sidebar should hide after toggle")
	}
	if mw := m.mainW(); mw != 118 {
		t.Errorf("hidden sidebar should give full width, got %d", mw)
	}
	m.ToggleSide()
	if !m.showSide() {
		t.Error("sidebar should show after second toggle")
	}
}

func TestYankEntriesRecentFirst(t *testing.T) {
	blocks := []Block{
		{Kind: "user", Text: "hi"},
		{Kind: "assistant", Text: "first answer"},
		{Kind: "tool", ToolName: "bash", ToolResult: "out"},
		{Kind: "assistant", Text: "  "},
		{Kind: "user", Text: "thanks"},
	}
	opts, descs, payload := yank.Entries(blocks)
	if len(opts) != 3 || len(descs) != 3 || len(payload) != 3 {
		t.Fatalf("want 3 copyable entries, got %d/%d/%d", len(opts), len(descs), len(payload))
	}
	if payload[0] != "thanks" || payload[1] != "first answer" || payload[2] != "hi" {
		t.Errorf("wrong order/payload: %q", payload)
	}
}

func TestOpenYankWiresDialog(t *testing.T) {
	m := Model{}
	m.blocks = []Block{{Kind: "assistant", Text: "hello"}}
	m.OpenYank()
	if len(m.Dialogs) != 1 || m.Dialogs[0].Kind != "yank" {
		t.Fatalf("want one yank dialog, got %+v", m.Dialogs)
	}
	if len(m.Dialogs[0].Payload) != 1 || m.Dialogs[0].Payload[0] != "hello" {
		t.Errorf("payload misaligned: %+v", m.Dialogs[0].Payload)
	}
	empty := Model{}
	empty.OpenYank()
	if len(empty.Dialogs) != 0 || len(empty.blocks) != 1 {
		t.Error("empty chat should notice, not open dialog")
	}
}
