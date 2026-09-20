package pirpc

import (
	"encoding/json"
	"strings"
	"testing"
)

// Prompt images must ride the RPC as pi expects:
// {"type":"prompt","message":...,"images":[{"type":"image",...}]}.
func TestPromptImagesMarshal(t *testing.T) {
	raw, err := json.Marshal(Command{
		Type:    "prompt",
		Message: "look @shot.png",
		Images:  []ImageContent{{Type: "image", Data: "aGk=", MimeType: "image/png"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{`"images"`, `"type":"image"`, `"mimeType":"image/png"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in %s", want, s)
		}
	}
	var back Command
	if err := json.Unmarshal(raw, &back); err != nil || len(back.Images) != 1 {
		t.Fatalf("roundtrip: %v %+v", err, back)
	}
	// no images → field omitted (old pi parity, small lines)
	raw, _ = json.Marshal(Command{Type: "prompt", Message: "hi"})
	if strings.Contains(string(raw), "images") {
		t.Fatalf("images should be omitted: %s", raw)
	}
}

func TestPromptEmptyMessagePresent(t *testing.T) {
	// Regression: pi does command.message.startsWith(...) unguarded — an
	// omitted message crashes the prompt ("Cannot read properties of
	// undefined"). Tray-only sends (images, no text) must keep "message".
	raw, err := json.Marshal(Command{Type: "prompt"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"message":""`) {
		t.Fatalf("empty message must be sent explicitly: %s", raw)
	}
}

func TestImageCount(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"hi"},{"type":"image"},{"type":"image"}]`)
	if n := ImageCount(raw); n != 2 {
		t.Fatalf("count = %d", n)
	}
	if n := ImageCount(json.RawMessage(`"plain"`)); n != 0 {
		t.Fatalf("plain count = %d", n)
	}
}
