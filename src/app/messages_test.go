package app

import (
	"strings"
	"testing"
)

const dupEnd = `{"message":{"role":"assistant","content":[{"type":"text","text":"Hey there!"},{"type":"thinking","thinking":"user says hi"}]}}`

func countKinds(m *Model) (asst, think int) {
	for _, b := range m.blocks {
		switch b.Kind {
		case "assistant":
			asst++
		case "thinking":
			think++
		}
	}
	return asst, think
}

// Streaming deltas + message_end must not render the answer twice.
func TestNoDupOnMessageEnd(t *testing.T) {
	m := Model{curAsst: -1, curThink: -1}
	m.tools = make(map[string]int)
	m.applyDelta([]byte(`{"assistantMessageEvent":{"type":"text_start"}}`))
	m.applyDelta([]byte(`{"assistantMessageEvent":{"type":"text_delta","delta":"Hey there!"}}`))
	m.applyDelta([]byte(`{"assistantMessageEvent":{"type":"thinking_start"}}`))
	m.applyDelta([]byte(`{"assistantMessageEvent":{"type":"thinking_delta","delta":"user says hi"}}`))
	m.applyMessageEnd([]byte(dupEnd))
	if asst, think := countKinds(&m); asst != 1 || think != 1 {
		t.Fatalf("streamed: want 1 asst + 1 think, got %d + %d", asst, think)
	}

	// Fallback: no deltas at all → message_end still renders once.
	m2 := Model{curAsst: -1, curThink: -1}
	m2.tools = make(map[string]int)
	m2.applyMessageEnd([]byte(dupEnd))
	if asst, think := countKinds(&m2); asst != 1 || think != 1 {
		t.Fatalf("fallback: want 1 asst + 1 think, got %d + %d", asst, think)
	}
}

// User echoes with vision blocks show a 📷 suffix (TextOf drops images).
func TestUserEchoWithImages(t *testing.T) {
	m := Model{curAsst: -1, curThink: -1}
	m.tools = make(map[string]int)
	m.applyMessageEnd([]byte(`{"message":{"role":"user","content":[{"type":"text","text":"look @shot.png"},{"type":"image"}]}}`))
	if len(m.blocks) != 1 || m.blocks[0].Kind != "user" {
		t.Fatalf("blocks = %+v", m.blocks)
	}
	if !strings.Contains(m.blocks[0].Text, "📷 1 image attached") {
		t.Fatalf("text = %q", m.blocks[0].Text)
	}
	if got := withImages("", 2); got != "📷 2 images attached" {
		t.Fatalf("image-only = %q", got)
	}
	if got := withImages("hi", 0); got != "hi" {
		t.Fatalf("no-image passthrough = %q", got)
	}
}
// Gutter: icon on the first line, blank 2-cell gutter after, separators untouched.
func TestGutter(t *testing.T) {
	got := gutter("●", "head\ncont\n\n")
	want := "● head\n  cont\n\n"
	if got != want {
		t.Fatalf("gutter = %q, want %q", got, want)
	}
	got = gutter("○", "only\n\n")
	if !strings.HasPrefix(got, "○ only\n") {
		t.Fatalf("single line gutter: %q", got)
	}
}
