package pimark

import (
	"strings"
	"testing"
)

func needBridge(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("pimark bridge unavailable (need node + pi in PATH)")
	}
}

func TestRenderPiStyle(t *testing.T) {
	needBridge(t)
	out, err := Render("# Hello\n\n```go\npackage main\n```\n", 60, Assistant)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Hello") {
		t.Fatalf("render lost heading: %q", out)
	}
	// pi keeps the fence border (gray ```go), unlike Glamour which strips it.
	if !strings.Contains(out, "```go") {
		t.Fatalf("render lost pi fence border: %q", out)
	}
}

func TestHighlightPiFallback(t *testing.T) {
	needBridge(t)
	// unknown lang: no auto-detect, plain mdCodeBlock color, input kept.
	out, err := Highlight("hello world", "nosuchlang123")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello world") {
		t.Fatalf("fallback lost code: %q", out)
	}
}
