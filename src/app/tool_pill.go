package app

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"pitago/src/components/format"
)

// Tidy-mode tool pills — the Warp-style collapsed tool header.
//
// Before this, Tidy mode collapsed a tool call to its header INSIDE the
// same rounded frame it uses when expanded: `● edit src/app/view.go`
// wrapped in a border, with the border carrying no information. A
// transcript of forty tool calls read as forty small boxes.
//
// A pill drops the frame entirely and states the same fact in one line:
// the tool name as a solid accent chip, the pretty args after it. The
// fill comes from the same toolFill() the frame uses, so a failed call's
// pill is red-tinted exactly like its expanded frame was — the state
// survives the collapse.
//
// Expanded rendering is untouched; ctrl+g flips between the two.

// toolPillRow is one collapsed tool call: the status bullet, the pill
// chip, and the pretty args. The pill is the block's title, so it takes
// the tool's kind accent as its foreground over the status fill.
func toolPillRow(bl Block, w int, hint string) string {
	name := strings.TrimSpace(bl.ToolName)
	if name == "" {
		name = "tool"
	}
	class := format.ToolStatusClass(bl.ToolStatus)
	row := toolBullet(bl) + " " + pillBadge(name, class)

	head := strings.TrimSpace(toolHead(bl))
	if head == "" {
		// A call with no args yet (still streaming, or a bare status
		// refresh): the chip IS the block. Return without the separator
		// space so the row carries no trailing whitespace.
		return row
	}
	// Budget from the visible width of what came before: the badge's ANSI
	// must not eat into the args allowance. The hint is reserved up front
	// rather than appended afterwards, or a long path pushes the row past
	// its column and wraps the pill onto a second line.
	avail := w - lipgloss.Width(row) - 3 - lipgloss.Width(hint)
	if avail < 12 {
		avail = 12
	}
	return row + "  " + toolStyle.Render(Short(head, avail))
}

// pillBadge is the solid chip: the tool name, uppercased, in the kind's
// accent over the execution-state fill. Padding comes from the borderless
// lipgloss style rather than hand-written spaces so the background cannot
// be reset early by a trailing ANSI sequence — the failure mode the
// chrome prototype had to patch around by hand.
func pillBadge(name string, class string) string {
	label := strings.ToUpper(name)
	if len(label) > 12 {
		label = label[:12]
	}
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(toolAccent(name)).
		Background(toolFill(class)).
		Render(" " + label + " ")
}

// toolPillHasHidden reports whether collapsing to a pill would hide
// something the expanded block shows. The chip absorbs the header's
// detail line ("running…", "no output") on its own — that text restates
// what the status bullet and the chip already carry — so the only real
// loss is a result or diff body. A call still in flight has none yet, so
// it shows no hint; the pill re-renders with one the moment it finishes.
func toolPillHasHidden(bl Block) bool {
	return strings.TrimSpace(bl.ToolResult) != "" || strings.TrimSpace(bl.ToolDiff) != ""
}

// toolPillBlock renders one collapsed tool call plus its expand hint.
// Returns the block body only; the caller appends the block separator,
// exactly as it does for the framed renderers.
func toolPillBlock(bl Block, w int) string {
	hint := ""
	if toolPillHasHidden(bl) {
		hint = " (" + expandHint + ")"
	}
	row := toolPillRow(bl, w, hint)
	if hint == "" {
		return row
	}
	return row + toolStyle.Render(hint)
}
