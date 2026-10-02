package app

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"pitago/src/pirpc"
)

// Nested tool calls (pi's codemode) are grouped under their parent block
// instead of each becoming its own top-level block. These pin the four
// things that would silently regress and hand the regression back to the
// user as a lost audit trail: the header count, the "args omitted"
// degradation, the tail elision, and the copy-menu entry that is the only
// remaining way to copy what the script actually ran.

func codemodeBlock(n int) Block {
	bl := Block{
		Kind:       "tool",
		ToolName:   "codemode",
		ToolStatus: "done",
		ToolArgs:   "collect the files",
		ToolResult: "Script completed\n",
	}
	for i := 0; i < n; i++ {
		bl.NestedCalls = append(bl.NestedCalls, NestedCall{
			ID:         "call_1/" + string(rune('a'+i%26)) + string(rune('a'+i/26)),
			Name:       "read",
			Status:     "ok",
			DurationMs: 120 + i,
		})
	}
	return bl
}

// The header states how many calls the script made, so the collapsed pill
// and the expanded frame agree on what the block contains. The count LEADS
// in both, because both truncate the head's tail. In the frame that was
// not cosmetic: a codemode block's arguments are a whole JavaScript
// program, so they filled the budget at every realistic width and took the
// count with them — the block showed no sign it had made any calls.
func TestToolHeadCarriesNestedCallCount(t *testing.T) {
	if got := toolHeadWithNested(codemodeBlock(7)); !strings.HasPrefix(got, "· 7 calls") {
		t.Errorf("head must LEAD with the nested-call count, got %q", got)
	}
	if got := toolHeadWithNested(codemodeBlock(1)); !strings.HasPrefix(got, "· 1 call") {
		t.Errorf("singular nested-call count must not take a plural: %q", got)
	}
	if got := toolHeadWithNested(readBlock("done")); got != toolHead(readBlock("done")) {
		t.Errorf("a block with no nested calls must be plain toolHead: %q vs %q", got, toolHead(readBlock("done")))
	}
}

// The count must survive a real header at a real width. This is the
// regression the live test surfaced: the frame truncated the head's tail,
// and a codemode block's args are always long enough to eat the count at
// every width a terminal realistically has.
func TestNestedCountSurvivesHeaderTruncation(t *testing.T) {
	const script = `{"code":"const [s, n] = await Promise.allSettled([\n` +
		`  tools.session_search({query: \"previous work\"}),\n` +
		`  tools.get_session_name({}),\n]);\nreturn n;"}`
	for _, w := range []int{50, 60, 76, 100, 140} {
		bl := codemodeBlock(2)
		bl.ToolArgs = script
		row := stripANSI(toolHeaderRow(bl, toolHeadWithNested(bl), blockInner(w)))
		if !strings.Contains(row, "· 2 calls") {
			t.Errorf("w=%d: nested-call count truncated away: %q", w, row)
		}
	}
}

// pi omits a call's arguments past its 8 KiB budget and sets
// argumentsBytes instead. The row must say so rather than render blank —
// a large `write` would otherwise show an empty row.
func TestNestedRowDegradesWhenArgumentsOmitted(t *testing.T) {
	bl := Block{
		Kind: "tool", ToolName: "codemode", ToolStatus: "done",
		NestedCalls: []NestedCall{
			{ID: "c1", Name: "bash", Status: "ok", ArgumentsBytes: 4200},
		},
	}
	out, _ := (&Model{}).renderOneBlock(bl, 60)
	row := ""
	for _, r := range frameRows(t, out) {
		if strings.Contains(stripANSI(r), "bash") && strings.Contains(stripANSI(r), "omitted") {
			row = stripANSI(r)
		}
	}
	if row == "" {
		t.Fatalf("nested call with dropped arguments rendered blank:\n%s", stripANSI(out))
	}
	if !strings.Contains(row, "4.1KB") {
		t.Errorf("omitted arguments must state their size: %q", row)
	}
}

// A codemode script can fan out to 256 calls; the frame stays one block by
// eliding the tail and stating the remainder as a count.
func TestNestedCallsElidePastTheCap(t *testing.T) {
	out, _ := (&Model{}).renderOneBlock(codemodeBlock(20), 60)
	text := stripANSI(out)
	// "collapsed", not "more calls": this line is the renderer's own doing,
	// and it must not read like calls were lost (nestedTruncationNote says
	// that, and says it differently).
	if !strings.Contains(text, "more collapsed") {
		t.Errorf("past-cap nested calls must render a count line:\n%s", text)
	}
	if got := strings.Count(text, "read"); got != nestedCallsMaxRows {
		t.Errorf("want exactly %d child rows, rendered %d:\n%s", nestedCallsMaxRows, got, text)
	}
	// Every row stays inside the frame's inner column.
	for _, r := range frameRows(t, text) {
		if w := lipgloss.Width(r); w > 60 {
			t.Errorf("row is %d cells over the 60-cell column: %q", w, r)
		}
	}
}

// Tidy mode collapses a tool call to one chip. Nested calls are children
// the pill cannot restate at all, so a codemode block with any must not
// report itself as having nothing hidden.
func TestCodemodePillDoesNotHideNestedCalls(t *testing.T) {
	if !toolPillHasHidden(codemodeBlock(1)) {
		t.Error("tidy mode would drop the nested-call record entirely")
	}
	if toolPillHasHidden(Block{Kind: "tool", ToolName: "cd", ToolStatus: "done"}) {
		t.Error("a tool block with no result, diff or nested calls must still collapse")
	}
}

// Grouping removed the per-call top-level blocks the nested calls used to
// be copyable as. The menu must offer a sibling entry that carries that
// audit trail, while the common "I want the output" entry stays first and
// a plain tool block's menu is unchanged.
func TestCopyMenuOffersNestedCalls(t *testing.T) {
	opts, payload := buildBlockOptions(collectBlockContent(codemodeBlock(3)))
	last := opts[len(opts)-1]
	if payload[len(payload)-1] != "nested" || !strings.Contains(last, "nested calls") {
		t.Errorf("want a trailing nested-copy entry, got %q / %q", opts, payload)
	}
	if strings.Contains(opts[0], "nested") {
		t.Errorf("the default output copy must stay first, got %q", opts[0])
	}

	plain, plainPayload := buildBlockOptions(collectBlockContent(readBlock("done")))
	for _, p := range plainPayload {
		if p == "nested" {
			t.Errorf("a tool block with no nested calls must not gain a nested-copy entry: %q", plain)
		}
	}
}

// bash is one of the tools that can drive ctx.executeTool(), so a shell
// block can be a codemode parent too. renderShellBlock returns early for
// shell tools, which would drop the nested rows and the header count
// entirely — the block would read as if the script made no calls.
func TestShellParentRendersNestedCalls(t *testing.T) {
	bl := codemodeBlock(3)
	bl.ToolName = "bash"
	bl.ToolArgs = "npm test"
	bl.ToolResult = "ok\n"
	text := stripANSI(mustRender(t, Model{}, bl, 60))
	if !strings.Contains(text, "· 3 calls") {
		t.Errorf("shell parent dropped the nested-call count:\n%s", text)
	}
	if got := strings.Count(text, "read"); got != 3 {
		t.Errorf("want 3 nested rows under the shell parent, got %d:\n%s", got, text)
	}
}

// A record that lost calls must say so, and must not say it the same way
// our own row-collapse does. The two lines sit adjacent in the frame, so a
// single "more calls" wording for both is how a truncated record passes
// for a complete one.
func TestNestedTruncationIsDistinguishedFromOurCollapse(t *testing.T) {
	complete := Block{Kind: "tool", ToolName: "codemode", ToolStatus: "done",
		ToolResult: "ok", NestedCalls: manyNestedCalls(12, "ok")}
	out := renderNestedCalls(complete, 72)
	if !containsAll(out, "more collapsed") {
		t.Errorf("our own elision must read as a collapse:\n%s", out)
	}
	if containsAll(out, "!") {
		t.Errorf("a complete record must not warn:\n%s", out)
	}
	if containsAll(out, "more calls") {
		t.Errorf("old ambiguous wording still present:\n%s", out)
	}

	// At pi's bound: calls may have been dropped beyond what we hold.
	full := Block{Kind: "tool", ToolName: "codemode", ToolStatus: "done",
		ToolResult: "ok", NestedCalls: manyNestedCalls(pirpc.NestedCallsMaxCalls, "ok")}
	out = renderNestedCalls(full, 72)
	if !containsAll(out, "! record incomplete") {
		t.Errorf("a record at pi's bound must warn:\n%s", tail(out))
	}

	// A call the script never waited for: the row's neutral glyph alone
	// does not tell the user it will never finish.
	unfinished := Block{Kind: "tool", ToolName: "codemode", ToolStatus: "done",
		ToolResult: "ok", NestedCalls: []NestedCall{
			{ID: "c/1", Name: "read", Status: "ok", DurationMs: 9, Arguments: `{"path":"a"}`},
			{ID: "c/2", Name: "write", Status: "unfinished", Arguments: `{"path":"b"}`},
		}}
	out = renderNestedCalls(unfinished, 72)
	if !containsAll(out, "! 1 call still running when the script returned") {
		t.Errorf("an unfinished nested call must be called out:\n%s", out)
	}
}

// manyNestedCalls builds n distinct completed rows.
func manyNestedCalls(n int, status string) []NestedCall {
	out := make([]NestedCall, n)
	for i := range out {
		out[i] = NestedCall{
			ID: "c/" + strconv.Itoa(i), Name: "grep", Status: status,
			DurationMs: 10 + i, Arguments: `{"pattern":"x"}`,
		}
	}
	return out
}

// tail is the last few lines of a render, for failure output that is not
// the whole frame.
func tail(s string) string {
	lines := strings.Split(s, "\n")
	if len(lines) > 4 {
		lines = lines[len(lines)-4:]
	}
	return strings.Join(lines, "\n")
}
