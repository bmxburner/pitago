// Package yank derives clipboard payloads from chat history.
// Drag-select in the terminal always grabs the sidebar too (same rows), so
// the yank picker is the clean way to copy chat-only text.
// Pure (no clipboard, no TUI state) so tests stay hermetic.
package yank

import (
	"fmt"
	"strings"

	"pitago/src/components/chat"
	"pitago/src/components/format"
)

// MaxYank lists how many recent messages the yank picker shows.
const MaxYank = 20

// Entries builds the picker rows from history, most recent first.
// Options previews align with Payload full texts.
func Entries(blocks []chat.Block) (opts, descs, payload []string) {
	for i := len(blocks) - 1; i >= 0 && len(opts) < MaxYank; i-- {
		b := blocks[i]
		if (b.Kind != "user" && b.Kind != "assistant") || strings.TrimSpace(b.Text) == "" {
			continue
		}
		opts = append(opts, b.Kind+": "+format.Short(b.Text, 60))
		descs = append(descs, fmt.Sprintf("%d chars", len([]rune(b.Text))))
		payload = append(payload, b.Text)
	}
	return opts, descs, payload
}

// LastAssistantText returns the latest non-blank assistant answer.
func LastAssistantText(blocks []chat.Block) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Kind == "assistant" && strings.TrimSpace(blocks[i].Text) != "" {
			return blocks[i].Text
		}
	}
	return ""
}
