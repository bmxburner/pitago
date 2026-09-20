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
