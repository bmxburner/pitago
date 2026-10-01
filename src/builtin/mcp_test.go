package builtin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pitago/src/app"
	"pitago/src/pirpc"
)

const mcpFixture = `{
  "mcpServers": {
    "alpha": { "command": "alpha-bin", "args": ["--stdio"] },
    "beta": { "type": "http", "url": "https://b.test/mcp", "disabled": true }
  },
  "imports": ["claude-code"]
}`

func mcpPanelModel(t *testing.T) *app.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(mcpFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := app.New(nil, t.TempDir())
	return &m
}

// mcpRow returns the right-pane row index for an action label.
func mcpRow(t *testing.T, d *app.Dialog, label string) int {
	t.Helper()
	for i, o := range d.Options {
		if o == label {
			return i
		}
	}
	t.Fatalf("row %q not in %v", label, d.Options)
	return -1
}

func hasMcpServer(t *testing.T, name string) bool {
	t.Helper()
	for _, n := range mcpDoc(t).Servers() {
		if n == name {
			return true
		}
	}
	return false
}

func mcpDoc(t *testing.T) *pirpc.McpDoc {
	t.Helper()
	doc, err := pirpc.LoadMcpConfig(filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestConfirmMcpToggleWritesAndKeepsPanel(t *testing.T) {
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	mm, _ := confirmMcpEdit(m, d, mcpRow(t, d, "Toggle enabled"))
	got := mm.(*app.Model)
	if !strings.Contains(got.Status, "disabled") || !strings.Contains(got.Status, "/reload") {
		t.Errorf("status = %q", got.Status)
	}
	if !mcpDoc(t).Get("alpha").Disabled {
		t.Error("toggle did not reach the file")
	}
	if len(got.Dialogs) != 1 {
		t.Errorf("panel should stay open, %d dialogs", len(got.Dialogs))
	}
	// The rows rebuilt in place and the selection survived.
	if got.Dialogs[0].McpSelected() != "alpha" {
		t.Errorf("selection = %q", got.Dialogs[0].McpSelected())
	}
	if !strings.Contains(strings.Join(got.Dialogs[0].Descs, "|"), "disabled · Enter enables") {
		t.Errorf("descs = %v", got.Dialogs[0].Descs)
	}
}

func TestConfirmMcpRemoveNeedsTwoPresses(t *testing.T) {
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	// First press only arms.
	mm, _ := confirmMcpEdit(m, d, mcpRow(t, d, "Remove server…"))
	got := mm.(*app.Model)
	if !strings.Contains(got.Status, "press Enter again") {
		t.Fatalf("status = %q", got.Status)
	}
	if !hasMcpServer(t, "alpha") {
		t.Error("server removed on the first press")
	}
	// Second press removes.
	mm, _ = confirmMcpEdit(got, got.Dialogs[0], mcpRow(t, got.Dialogs[0], "Remove server…"))
	got = mm.(*app.Model)
	if !strings.Contains(got.Status, "removed") {
		t.Errorf("status = %q", got.Status)
	}
	if hasMcpServer(t, "alpha") {
		t.Error("alpha still configured after the second press")
	}
	if got.Dialogs[0].McpSelected() != "beta" {
		t.Errorf("selection should fall back to beta, got %q", got.Dialogs[0].McpSelected())
	}
}

func TestConfirmMcpAddAndEditOpenTheForm(t *testing.T) {
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	mm, _ := confirmMcpEdit(m, d, mcpRow(t, d, "Add server…"))
	got := mm.(*app.Model)
	if len(got.Dialogs) != 2 || got.Dialogs[0].Kind != "mcpeditForm" {
		t.Fatalf("form not on top: %+v", got.Dialogs)
	}
	if got.Dialogs[0].Title != "Add MCP server" {
		t.Errorf("title = %q", got.Dialogs[0].Title)
	}

	d = got.Dialogs[1]
	mm, _ = confirmMcpEdit(got, d, mcpRow(t, d, "Edit server…"))
	got = mm.(*app.Model)
	if got.Dialogs[0].Title != "Edit MCP server — alpha" {
		t.Errorf("edit title = %q", got.Dialogs[0].Title)
	}
}

func TestConfirmMcpOpenIsOffLoop(t *testing.T) {
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	mm, cmd := confirmMcpEdit(m, d, mcpRow(t, d, "Open mcp.json"))
	if cmd == nil {
		t.Fatal("open should return a command")
	}
	_ = mm.(*app.Model)
	// The command reports back as McpPanelMsg; run it and accept either
	// outcome (Finder may or may not exist on the test machine).
	msg := cmd()
	if _, ok := msg.(app.McpPanelMsg); !ok {
		t.Fatalf("msg = %T, want app.McpPanelMsg", msg)
	}
}

func TestConfirmMcpIgnoresUnknownPayload(t *testing.T) {
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	d.Payload[0] = "mcp:unknown"
	mm, cmd := confirmMcpEdit(m, d, 0)
	if cmd != nil {
		t.Error("unknown payload should not schedule work")
	}
	if len(mm.(*app.Model).Dialogs) != 1 {
		t.Error("panel should be untouched")
	}
}

func TestMcpNoticeCountsOnlyWhatNeedsAttention(t *testing.T) {
	// pi exits 1 when ANY server is wrong but still prints every server,
	// so the notice must count the ones that actually need a human.
	err := errors.New("exit status 1")
	servers := []pirpc.McpServerInfo{
		{Name: "a", State: "connected", Enabled: true},
		{Name: "b", State: "failed", Enabled: true, Error: "no SLACK_BOT_TOKEN"},
		{Name: "c", State: "disabled", Enabled: false}, // the user's choice, not broken
	}
	got := mcpNotice(nil, servers, err)
	// The denominator excludes the disabled one: two live servers, one broken.
	if !strings.Contains(got, "1 of 2") {
		t.Errorf("notice = %q, want one of the two live servers flagged", got)
	}
	// "connecting" is not connected, so it must not be called healthy.
	starting := []pirpc.McpServerInfo{
		{Name: "a", State: "connected", Enabled: true},
		{Name: "b", State: "connecting", Enabled: true},
	}
	if got := mcpNotice(nil, starting, err); !strings.Contains(got, "1 of 2") {
		t.Errorf("notice = %q, a connecting server is not a connected one", got)
	}
	// Nothing actually needs attention: say so, keep the status.
	all := []pirpc.McpServerInfo{{Name: "a", State: "connected", Enabled: true}}
	if got := mcpNotice(nil, all, err); !strings.Contains(got, "connected") {
		t.Errorf("notice = %q, want the list reported as healthy", got)
	}
	// A list that produced nothing at all keeps the raw status.
	if got := mcpNotice(nil, nil, err); !strings.Contains(got, "exit status 1") {
		t.Errorf("empty-list notice = %q", got)
	}
	// Config errors still win: they are the actionable part.
	if got := mcpNotice([]string{"mcp.json is not valid JSON"}, servers, nil); !strings.Contains(got, "mcp.json") {
		t.Errorf("notice = %q", got)
	}
}

// mcpRowUnder finds an action row by the server it sits under: the hub's
// MCP section is a tree, so an action label appears once per server.
func mcpRowUnder(t *testing.T, d *app.Dialog, server, label string) int {
	t.Helper()
	start := -1
	for i, o := range d.Options {
		if o == server {
			start = i + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("server row %q not in %v", server, d.Options)
	}
	for i := start; i < len(d.Options); i++ {
		if d.Options[i] == "" || !strings.HasPrefix(d.Options[i], "  ") {
			break
		}
		if strings.TrimSpace(d.Options[i]) == label {
			return i
		}
	}
	t.Fatalf("row %q under %q not in %v", label, server, d.Options)
	return -1
}

func TestHubPiActionRowsRunFromTheServerMenu(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	m := hubModel()
	d := m.Dialogs[0]
	selectPsec(m, d, app.PsecMCP)
	m.SetMcpInfo(app.McpMsg{Servers: []pirpc.McpServerInfo{{
		Name: "needsyou", State: "needs-auth", Enabled: true, Scope: "global", Transport: "https://x.test/mcp",
	}}})
	m.LoadPsecRows(d)
	// Put the cursor on the server so the third column describes it, then
	// find its Sign in row.
	for f, p := range d.FIdx {
		if d.Payload[p] == "@mcpsel:needsyou" {
			d.Cursor = f
		}
	}
	m.LoadPsecRows(d)
	ri := -1
	for i, o := range d.McpAct {
		if o == "Sign in" {
			ri = i
		}
	}
	if ri < 0 {
		t.Fatalf("no Sign in row: %v", d.McpAct)
	}
	if p := d.McpActPayload[ri]; p != "@mcpact:signin@needsyou" {
		t.Fatalf("sign-in payload = %q", p)
	}
	// Enter on that row: focus the column and mark the run, exactly as the
	// hub's key handler does.
	d.McpActFocus, d.McpActCursor, d.McpActRun = true, ri, true
	m.UseBuiltins(All(), Confirmers())
	mm, cmd := confirmPconfig(m, d, ri)
	got := mm.(*app.Model)
	if len(got.Dialogs) != 1 {
		t.Errorf("sign-in must not open a dialog: %+v", got.Dialogs)
	}
	if cmd == nil {
		t.Error("sign-in should schedule the login")
	}
	if !strings.Contains(got.Status, "signing in to needsyou") {
		t.Errorf("status = %q", got.Status)
	}
}

func TestHubActionOnAnUnknownServerRelistsInsteadOfGuessing(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	m := hubModel()
	m.UseBuiltins(All(), Confirmers())
	mm, cmd := confirmMcpHubAction(m, "disable@ghost")
	_ = mm
	if cmd == nil {
		t.Error("the list must be re-read, not guessed at")
	}
}

func TestHubEmptyServerListSpeaksPisWording(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	m := hubModel()
	d := m.Dialogs[0]
	selectPsec(m, d, app.PsecMCP)
	m.MCP = nil
	m.SetMcpInfo(app.McpMsg{})
	m.LoadPsecRows(d)
	if !strings.Contains(d.Message, "No MCP servers configured") ||
		!strings.Contains(d.Message, ".pi/mcp.json") {
		t.Errorf("pi's empty wording = %q", d.Message)
	}
	// Nothing to manage: the hub says so in pi's words, and offers no
	// actions. (Adding a server is the follow-up PR, not this one.)
	if len(d.McpAct) != 0 {
		t.Errorf("no servers means no actions: %v", d.McpAct)
	}
}

// Edit and Remove from the hub's actions column. Edit now happens IN
// pane 3 — one surface, no overlay — and Remove is a two-press gate that
// says so where the user is looking.
func TestHubEditAndRemoveFromTheActionsColumn(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"alpha":{"command":"a"}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := hubModel()
	m.UseBuiltins(All(), Confirmers())
	d := m.Dialogs[0]
	selectPsec(m, d, app.PsecMCP)
	m.SetMcpInfo(app.McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Scope: "global", Source: filepath.Join(dir, "mcp.json")},
	}})
	m.LoadPsecRows(d)

	row := -1
	for i, o := range d.McpAct {
		if o == "Edit server…" {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("no Edit row: %v", d.McpAct)
	}
	d.McpActFocus, d.McpActCursor, d.McpActRun = true, row, true
	mm, _ := confirmPconfig(m, d, 0)
	got := mm.(*app.Model)
	// No dialog was pushed: the hub itself is now editing.
	if len(got.Dialogs) != 1 {
		t.Fatalf("edit opened an overlay: %+v", got.Dialogs)
	}
	if !got.Dialogs[0].McpEdit.Active {
		t.Fatal("pane 3 is not in edit mode")
	}
	if got.Dialogs[0].McpEdit.Orig != "alpha" {
		t.Errorf("editing %q, want alpha", got.Dialogs[0].McpEdit.Orig)
	}
	if got.Dialogs[0].McpEdit.Path != filepath.Join(dir, "mcp.json") {
		t.Errorf("editor pinned to %q, want the file pi reported", got.Dialogs[0].McpEdit.Path)
	}
	// Prefilled from the file, focused on the command.
	if v := got.Dialogs[0].McpEditVal(); v != "a" {
		t.Errorf("prefilled value = %q, want the stored command", v)
	}
	// Remove, from a hub that is not currently editing: behind a gate that
	// says so, and the second press deletes.
	m2 := hubModel()
	d2 := m2.Dialogs[0]
	selectPsec(m2, d2, app.PsecMCP)
	m2.SetMcpInfo(app.McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Scope: "global", Source: filepath.Join(dir, "mcp.json")},
	}})
	m2.LoadPsecRows(d2)
	rm := -1
	for i, o := range d2.McpAct {
		if o == "Remove server…" {
			rm = i
		}
	}
	if rm < 0 {
		t.Fatalf("no Remove row: %v", d2.McpAct)
	}
	d2.McpActFocus, d2.McpActCursor, d2.McpActRun = true, rm, true
	mm, _ = confirmPconfig(m2, d2, 0)
	got = mm.(*app.Model)
	saved, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if !strings.Contains(string(saved), "alpha") {
		t.Errorf("one press must not delete: %s", saved)
	}
	// The gate is announced on the Remove row, next to the action it
	// qualifies — not in the message strip at the top of the panel.
	if strings.Contains(got.Dialogs[0].Message, "Enter again") {
		t.Errorf("the prompt should not sit in the top message strip: %q", got.Dialogs[0].Message)
	}
	if !got.McpRemoveArmed("alpha") {
		t.Error("the first press must leave the gate armed on the Remove row")
	}
	// The second press inside the window removes it.
	d2.McpActRun = true
	mm, _ = confirmPconfig(got, d2, 0)
	got = mm.(*app.Model)
	saved, _ = os.ReadFile(filepath.Join(dir, "mcp.json"))
	if strings.Contains(string(saved), "alpha") {
		t.Errorf("the second press did not remove it: %s", saved)
	}
}
