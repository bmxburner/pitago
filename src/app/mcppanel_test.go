package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/pirpc"
)

const mcpPanelFixture = `{
  "mcpServers": {
    "vault-tools": {
      "command": "/opt/homebrew/bin/bun",
      "args": ["run", "/src/index.ts"],
      "env": { "VAULT_ROOT": "/Volumes/Tars" }
    },
    "remote": {
      "type": "http",
      "url": "https://example.test/mcp",
      "disabled": true
    }
  },
  "imports": ["claude-code"]
}`

// mcpPanelEnv points piAgentDir() at a temp dir holding a fixture mcp.json
// and returns the file path.
func mcpPanelEnv(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(mcpPanelFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	return filepath.Join(dir, "mcp.json")
}

func mcpPanelModel(t *testing.T) *Model {
	t.Helper()
	m := &Model{MCP: []McpServer{{Name: "vault-tools", Direct: 12, Total: 15, Tokens: 1400, Connected: true}}}
	m.winW, m.winH = 140, 40
	return m
}

// ptr takes the address of a Model value (dialog updaters return a value).
func ptr(m Model) *Model { return &m }

// openForm opens the add/edit form on the MCP panel underneath (not on an
// earlier form), like confirmMcp does.
func openForm(m *Model, name string) {
	var d *Dialog
	for _, cur := range m.Dialogs {
		if cur.Kind == mcpKind {
			d = cur
			break
		}
	}
	m.OpenMcpForm(d, name)
}

func payloadOfRow(t *testing.T, d *Dialog, opt string) string {
	t.Helper()
	for i, o := range d.Options {
		if o == opt {
			if i >= len(d.Payload) {
				t.Fatalf("no payload for row %q", opt)
			}
			return d.Payload[i]
		}
	}
	t.Fatalf("row %q not in %v", opt, d.Options)
	return ""
}

func TestMcpPanelListsServersAndActions(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	if n := len(m.Dialogs); n != 1 || m.Dialogs[0].Kind != mcpKind {
		t.Fatalf("panel not pushed: %+v", m.Dialogs)
	}
	d := m.Dialogs[0]
	if got := strings.Join(d.McpNames, ","); got != "vault-tools,remote" {
		t.Errorf("servers = %q, want vault-tools,remote", got)
	}
	// Live status dot for the connected one, disabled marker for the other.
	if !strings.HasPrefix(d.Provs[0], "● ") {
		t.Errorf("connected server should be ●: %q", d.Provs[0])
	}
	if !strings.HasPrefix(d.Provs[1], "⊘ ") {
		t.Errorf("disabled server should be ⊘: %q", d.Provs[1])
	}
	if d.LeftHead != "SERVERS" || d.RightHead != "ACTIONS" {
		t.Errorf("heads = %q / %q", d.LeftHead, d.RightHead)
	}
	for _, row := range []string{"Add server…", "Toggle enabled", "Edit server…", "Remove server…", "Reload pi MCPs", "Open mcp.json"} {
		if p := payloadOfRow(t, d, row); !strings.HasPrefix(p, "mcp:") {
			t.Errorf("row %q payload = %q", row, p)
		}
	}
	if d.Options[0] != "Add server…" {
		t.Errorf("Add should be the first action, got %v", d.Options)
	}
}

func TestMcpPanelServerCursorDrivesRowsAndDetail(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]

	// stdio: the detail column shows the command, args and env.
	lines := stripANSI(strings.Join(m.mcpDetailLines(d, 40), "\n"))
	for _, want := range []string{"vault-tools", "Command", "/opt/homebrew/bin/bun", "run /src/index.ts", "VAULT_ROOT=/Volumes/Tars", "connected", "12/15"} {
		if !strings.Contains(lines, want) {
			t.Errorf("detail missing %q:\n%s", want, lines)
		}
	}
	if strings.Contains(lines, "http://") {
		t.Errorf("stdio server should not show a URL row:\n%s", lines)
	}

	// Move to the http server (↑↓ on the left pane reloads the rows).
	d.ProvCursor = 1
	m.LoadMcpRows(d)
	lines = stripANSI(strings.Join(m.mcpDetailLines(d, 40), "\n"))
	for _, want := range []string{"URL", "https://example.test/mcp", "disabled", "http"} {
		if !strings.Contains(lines, want) {
			t.Errorf("detail missing %q:\n%s", want, lines)
		}
	}
	if got := payloadOfRow(t, d, "Toggle enabled"); got != "mcp:toggle" {
		t.Errorf("toggle payload = %q", got)
	}
	descs := strings.Join(d.Descs, " | ")
	if !strings.Contains(descs, "disabled · Enter enables") {
		t.Errorf("toggle desc should offer enabling: %s", descs)
	}
}

func TestMcpPanelEmptyConfig(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	if len(d.McpNames) != 0 || d.McpSelected() != "" {
		t.Fatalf("expected no servers, got %v", d.McpNames)
	}
	if len(d.Provs) != 1 || !strings.Contains(d.Provs[0], "no servers") {
		t.Errorf("left pane = %v", d.Provs)
	}
	if p := payloadOfRow(t, d, "Add server…"); p != "mcp:add" {
		t.Errorf("add payload = %q", p)
	}
	if lines := stripANSI(strings.Join(m.mcpDetailLines(d, 40), "\n")); !strings.Contains(lines, "no server selected") {
		t.Errorf("detail = %s", lines)
	}
}

func TestMcpPanelUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	if !strings.Contains(d.Message, "cannot read mcp.json") {
		t.Errorf("message = %q", d.Message)
	}
	if p := payloadOfRow(t, d, "Open mcp.json"); p != "mcp:open" {
		t.Errorf("fallback action = %q", p)
	}
}

func TestMcpToggleWritesFileAndBackup(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	msg, err := m.McpToggle("", "vault-tools")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "disabled") {
		t.Errorf("toggle message = %q", msg)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), `"VAULT_ROOT"`) {
		t.Errorf("env lost on toggle:\n%s", saved)
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Get("vault-tools").Disabled {
		t.Errorf("toggle did not persist:\n%s", saved)
	}
	// imports section survived untouched.
	if !strings.Contains(string(saved), `"claude-code"`) {
		t.Errorf("imports lost:\n%s", saved)
	}
	// The pre-write copy is on disk.
	back, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != mcpPanelFixture {
		t.Errorf("backup content = %s", back)
	}
	// Missing server is an error, not a silent write.
	if _, err := m.McpToggle("", "ghost"); err == nil {
		t.Error("toggle of a missing server should error")
	}
}

func TestMcpRemoveArmsTwice(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	if m.ArmMcpRemove("vault-tools") {
		t.Fatal("first press must only arm")
	}
	// The prompt belongs on the Remove row, where the action it qualifies
	// is — not on a status line the user is not looking at.
	if !m.McpRemoveArmed("vault-tools") {
		t.Error("the gate is not armed on vault-tools")
	}
	if !m.ArmMcpRemove("vault-tools") {
		t.Fatal("second press within the window must confirm")
	}
	// A different server re-arms instead of confirming.
	if m.ArmMcpRemove("remote") {
		t.Fatal("another server must re-arm")
	}
	if !m.ArmMcpRemove("remote") {
		t.Fatal("second press for remote must confirm")
	}
	// Opening a panel starts unarmed, so a stale arm can never turn the
	// first Enter into a delete.
	m.OpenMcpPanel()
	if m.ArmMcpRemove("vault-tools") {
		t.Error("a new panel must not inherit an armed remove")
	}
	msg, err := m.McpRemove("", "remote")
	if err != nil || !strings.Contains(msg, "removed") {
		t.Fatalf("remove = %q, %v", msg, err)
	}
	saved, _ := os.ReadFile(path)
	if strings.Contains(string(saved), `"remote"`) {
		t.Errorf("remote still configured:\n%s", saved)
	}
}

func TestMcpFormPrefillAndSave(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "vault-tools")
	if n := len(m.Dialogs); n != 2 || m.Dialogs[0].Kind != mcpFormKind {
		t.Fatalf("form not pushed: %+v", m.Dialogs)
	}
	f := m.Dialogs[0]
	if f.FormPath != path {
		t.Errorf("form path = %q, want %q", f.FormPath, path)
	}
	vals := mcpFormValues(f)
	if vals["Name"] != "vault-tools" || vals["Command"] != "/opt/homebrew/bin/bun" {
		t.Errorf("prefill = %+v", vals)
	}
	if vals["Args"] != "run /src/index.ts" {
		t.Errorf("args prefill = %q", vals["Args"])
	}
	if !strings.Contains(vals["Env"], `"VAULT_ROOT": "/Volumes/Tars"`) {
		t.Errorf("env prefill = %q", vals["Env"])
	}

	// Typing lands in the focused field; ↑↓ move between fields.
	f.FormFocus = 2
	mm, _ := m.updateMcpForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("!")}, f)
	m = ptr(mm.(Model))
	if !strings.HasSuffix(mcpFormValues(f)["Command"], "!") {
		t.Errorf("rune did not reach the focused field: %+v", mcpFormValues(f))
	}
	mm, _ = m.updateMcpForm(tea.KeyMsg{Type: tea.KeyUp}, f)
	m = ptr(mm.(Model))
	if f.FormFocus != 1 {
		t.Errorf("↑ should move up, focus = %d", f.FormFocus)
	}
	mm, _ = m.updateMcpForm(tea.KeyMsg{Type: tea.KeyBackspace}, f)
	m = ptr(mm.(Model))
	if mcpFormValues(f)["Transport"] != "stdi" {
		t.Errorf("backspace did not trim: %q", mcpFormValues(f)["Transport"])
	}
	// Put the transport back (the prefilled "stdio" minus one rune is not
	// a valid value) before saving.
	f.FormVals[1] = "stdio"

	// Save with the appended "!" and confirm it landed.
	f.FormFocus = 5
	mm, _ = m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 {
		t.Fatalf("form should pop, %d dialogs left", len(m.Dialogs))
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Get("vault-tools").Command; got != "/opt/homebrew/bin/bun!" {
		t.Errorf("saved command = %q", got)
	}
	if !strings.Contains(m.Status, "/reload") {
		t.Errorf("status should mention /reload: %q", m.Status)
	}
}

func TestMcpFormAddServer(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "")
	f := m.Dialogs[0]
	if got := mcpFormValues(f)["Name"]; got != "" {
		t.Errorf("add form should start empty, name = %q", got)
	}
	set := func(field, val string) {
		for i, name := range f.Options {
			if name != field {
				continue
			}
			f.FormFocus = i
			f.setFormVal(val) // marks the row touched, like typing does
		}
	}
	set("Name", "fresh")
	set("Command", "/usr/bin/env")
	set("Args", `run "/Volumes/My Docs/svc.js"`)
	set("Env", `{"A":"1"}`)
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := doc.Get("fresh")
	if got.Command != "/usr/bin/env" {
		t.Errorf("new server command = %q", got.Command)
	}
	if len(got.Args) != 2 || got.Args[1] != "/Volumes/My Docs/svc.js" {
		t.Errorf("quoted args not split right: %#v", got.Args)
	}
	if got.Env["A"] != "1" {
		t.Errorf("env = %v", got.Env)
	}
	if !strings.Contains(m.Dialogs[0].McpNames[len(m.Dialogs[0].McpNames)-1], "fresh") {
		t.Errorf("new server not in the panel: %v", m.Dialogs[0].McpNames)
	}
}

func TestMcpFormValidation(t *testing.T) {
	cases := []struct {
		name  string
		vals  map[string]string
		wants string
	}{
		{"no name", map[string]string{"Command": "x"}, "name is required"},
		{"spaced name", map[string]string{"Name": "a b", "Command": "x"}, "name must not contain"},
		{"stdio without command", map[string]string{"Name": "a"}, "needs a command"},
		{"bad transport", map[string]string{"Name": "a", "Command": "x", "Transport": "grpc"}, "transport must be"},
		{"http without url", map[string]string{"Name": "a", "Transport": "http"}, "needs a URL"},
		{"bad args json", map[string]string{"Name": "a", "Command": "x", "Args": `["a",`}, "args:"},
		{"unclosed quote", map[string]string{"Name": "a", "Command": "x", "Args": `run "a`}, "unclosed"},
		{"bad env", map[string]string{"Name": "a", "Command": "x", "Env": "{oops}"}, "Env:"},
	}
	for _, tc := range cases {
		f := &Dialog{Kind: mcpFormKind, Options: mcpFields,
			FormTouched: map[string]bool{"Args": true, "Env": true, "Headers": true}}
		for _, name := range f.Options {
			f.FormVals = append(f.FormVals, tc.vals[name])
		}
		_, err := mcpDefFromForm(f, pirpc.McpDef{})
		if err == nil {
			t.Errorf("%s: expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.wants)
		}
	}
	// A valid stdio server with JSON array args passes.
	f := &Dialog{Kind: mcpFormKind, Options: mcpFields,
		FormVals:    []string{"a", "", "cmd", "", `["x","y"]`, "", "", "yes"},
		FormTouched: map[string]bool{"Args": true}}
	f.FormPristine = append([]string(nil), f.FormVals...)
	def, err := mcpDefFromForm(f, pirpc.McpDef{})
	if err != nil {
		t.Fatalf("valid form rejected: %v", err)
	}
	if def.Transport() != "stdio" || len(def.Args) != 2 {
		t.Errorf("def = %+v", def)
	}
	// Enabled = no disables the server; anything else (including a row
	// blanked by a stray backspace) is rejected rather than guessed.
	setEnabled := func(v string) {
		f.FormFocus = 7
		f.setFormVal(v)
	}
	setEnabled("no")
	def, err = mcpDefFromForm(f, pirpc.McpDef{})
	if err != nil || !def.Disabled {
		t.Errorf("Enabled=no did not disable: %+v / %v", def, err)
	}
	setEnabled("maybe")
	if _, err := mcpDefFromForm(f, pirpc.McpDef{}); err == nil {
		t.Error("Enabled=maybe should be rejected")
	}
	setEnabled("")
	if _, err := mcpDefFromForm(f, pirpc.McpDef{Disabled: true}); err != nil {
		t.Errorf("blanking Enabled should fall back to the stored state, not error: %v", err)
	}
	def, err = mcpDefFromForm(f, pirpc.McpDef{Disabled: true})
	if err != nil || !def.Disabled {
		t.Errorf("blanking Enabled silently re-enabled: %+v / %v", def, err)
	}
}

func TestMcpFormSaveErrorStaysInForm(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	openForm(m, "")
	f := m.Dialogs[0]
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 {
		t.Fatalf("form should stay open, %d dialogs", len(m.Dialogs))
	}
	if !strings.Contains(f.FormErr, "name is required") {
		t.Errorf("FormErr = %q", f.FormErr)
	}
	// The empty form wrote nothing.
	saved, _ := os.ReadFile(f.FormPath)
	if !strings.Contains(string(saved), `"imports"`) {
		t.Errorf("file rewritten on a failed save:\n%s", saved)
	}
}

func TestMcpPanelRendersTwoPanes(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	out := stripANSI(m.renderDialog())
	for _, want := range []string{"SERVERS", "ACTIONS", "vault-tools", "Toggle enabled", "MCP servers"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	// Narrow terminal: no DETAILS column, descs come back into the rows.
	m.winW = 100
	out = stripANSI(m.renderDialog())
	if strings.Contains(out, "DETAILS") {
		t.Errorf("narrow render should be two panes:\n%s", out)
	}
	if !strings.Contains(out, "restart pi so it re") {
		t.Errorf("narrow render should carry row descs:\n%s", out)
	}
	// The left pane is servers, not sections: no count badge.
	if strings.Contains(out, "● vault-tools      1") {
		t.Errorf("left pane should not carry a section count:\n%s", out)
	}
}

func TestMcpFormEscDiscardsWithAGate(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	openForm(m, "")
	f := m.Dialogs[0]
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	// Untouched form: one Esc closes it.
	mm, _ := m.updateMcpForm(esc, f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 0 {
		t.Fatalf("clean form should close on one Esc, %d left", len(m.Dialogs))
	}

	// Typed into: the first Esc arms, the second discards.
	openForm(m, "")
	f = m.Dialogs[0]
	mm, _ = m.updateMcpForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, f)
	m = ptr(mm.(Model))
	mm, _ = m.updateMcpForm(esc, f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 {
		t.Fatalf("first Esc must only arm, %d dialogs left", len(m.Dialogs))
	}
	// The prompt is derived at render time, not parked in m.Status: a
	// status-bar string never expires, so it outlived the gate that wrote
	// it and promised a second Esc that only re-armed. Assert on the form.
	if form := stripANSI(m.renderMcpForm(f)); !strings.Contains(form, "press Esc again") {
		t.Errorf("the armed gate says nothing in the form:\n%s", form)
	}
	mm, _ = m.updateMcpForm(esc, f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 0 {
		t.Errorf("second Esc should discard, %d left", len(m.Dialogs))
	}
}

func TestMcpFormTransportSwitchDropsStaleField(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	openForm(m, "vault-tools")
	f := m.Dialogs[0]
	set := func(field, val string) {
		for i, name := range f.Options {
			if name != field {
				continue
			}
			f.FormFocus = i
			f.setFormVal(val) // marks the row touched, like typing does
		}
	}
	// stdio → http must not leave the prefilled command in the entry.
	set("Transport", "http")
	set("URL", "https://new.test/mcp")
	def, err := mcpDefFromForm(f, pirpc.McpDef{})
	if err != nil {
		t.Fatal(err)
	}
	if def.Command != "" {
		t.Errorf("command survived a transport switch: %+v", def)
	}
	// …and http → stdio must not leave a URL behind.
	set("Transport", "stdio")
	set("URL", "")
	set("Command", "bin")
	def, err = mcpDefFromForm(f, pirpc.McpDef{})
	if err != nil {
		t.Fatal(err)
	}
	if def.URL != "" {
		t.Errorf("url survived a transport switch: %+v", def)
	}
}

func TestMcpFormSaveReResolvesPath(t *testing.T) {
	dir := t.TempDir() // no mcp.json yet: the form resolves a not-yet-existing path
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel() // panel on top, so the save targets the panel's file
	openForm(m, "")
	f := m.Dialogs[0]
	// The form was opened before the file existed; the save must merge
	// into it rather than overwrite it.
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(mcpPanelFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	set := func(field, val string) {
		for i, name := range f.Options {
			if name != field {
				continue
			}
			f.FormFocus = i
			f.setFormVal(val) // marks the row touched, like typing does
		}
	}
	set("Name", "fresh")
	set("Command", "/usr/bin/env")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 || f.FormErr != "" {
		t.Fatalf("save failed: dialogs=%d err=%q", len(m.Dialogs), f.FormErr)
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(doc.Servers(), ","); got != "vault-tools,remote,fresh" {
		t.Errorf("existing servers lost or not appended: %q", got)
	}
}

func TestSaveIsRefusedWhenTheFileChangedUnderThePanel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(mcpPanelFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	// The doc the panel holds, parsed a moment ago.
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	// A write lands between that parse and the save.
	hand := `{"mcpServers":{"fresh":{"command":"someone-else"}}}`
	if err := os.WriteFile(path, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := doc.ToggleDisabled("vault-tools"); err != nil {
		t.Fatal(err)
	}
	if err := saveMcpDoc(doc); err == nil {
		t.Fatal("a stale doc must be refused, not renamed over the newer file")
	}
	now, _ := os.ReadFile(path)
	if string(now) != hand {
		t.Errorf("file was overwritten:\n%s", now)
	}
	if _, err := os.Stat(path + ".bak"); err == nil {
		t.Error("a refused save must not write a .bak (it would clobber the good one)")
	}
}

func TestFormSaveFailureStaysInTheForm(t *testing.T) {
	dir := t.TempDir()
	// A directory where the config file should be: reads and writes fail.
	if err := os.Mkdir(filepath.Join(dir, "mcp.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	openForm(m, "")
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("x")
	f.FormFocus = 2
	f.setFormVal("bin")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 {
		t.Fatalf("a failed save must keep the form open, %d dialogs", len(m.Dialogs))
	}
	if f.FormErr == "" {
		t.Error("a failed save must show the reason in the form")
	}
}

func TestRenameOntoAnExistingNameIsRefused(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "vault-tools")
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("remote") // a server that already exists
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if !strings.Contains(f.FormErr, "already exists") {
		t.Errorf("collision should be refused, FormErr = %q", f.FormErr)
	}
	if len(m.Dialogs) != 2 {
		t.Errorf("a refused save must keep the form open on the panel, %d dialogs", len(m.Dialogs))
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(doc.Servers(), ","); got != "vault-tools,remote" {
		t.Errorf("a refused rename changed the file: %q", got)
	}
	if doc.Get("remote").URL != "https://example.test/mcp" {
		t.Errorf("the victim server was clobbered: %+v", doc.Get("remote"))
	}
}

func TestAddOntoAnExistingNameIsRefused(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "") // add
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("remote")
	f.FormFocus = 2
	f.setFormVal("something")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if !strings.Contains(f.FormErr, "already exists") {
		t.Errorf("add over an existing name should be refused, FormErr = %q", f.FormErr)
	}
	doc, _ := pirpc.LoadMcpConfig(path)
	if got := doc.Get("remote"); got.URL != "https://example.test/mcp" || got.Command != "" {
		t.Errorf("existing entry was overwritten: %+v", got)
	}
}

func TestAnEditThatOnlyTrimsIsStillAnEdit(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"spaced":{"command":"c","args":["  run","x"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "spaced")
	f := m.Dialogs[0]
	// The prefill keeps the leading space of the stored first arg.
	if got := mcpFormValues(f)["Args"]; got != "  run x" {
		t.Fatalf("args prefill = %q", got)
	}
	// One backspace leaves the trimmed text identical but the raw value
	// changed — a real edit, and it must be applied, not silently dropped.
	f.FormFocus = indexOfField(f, "Args")
	f.setFormVal(" run x")
	if !f.FormTouched["Args"] {
		t.Fatalf("a raw change was not treated as an edit: %v", f.FormTouched)
	}
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if !strings.Contains(string(out), `"run"`) || strings.Contains(string(out), `"  run"`) {
		t.Errorf("edit was discarded:\n%s", out)
	}
}

func TestWhitespaceOnlyRowsCanStillBeCleared(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"spaced":{"command":"c","args":[" "]}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "spaced")
	f := m.Dialogs[0]
	f.FormFocus = indexOfField(f, "Args")
	f.setFormVal("") // blanked a row whose value was whitespace
	if !f.FormTouched["Args"] {
		t.Fatalf("blanking a whitespace-only row should count as an edit: %v", f.FormTouched)
	}
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if strings.Contains(string(out), `"args"`) {
		t.Errorf("whitespace args were not cleared:\n%s", out)
	}
}

func TestCollisionIsCheckedByKeyNotField(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"weird":{"env":{"TOKEN":"secret"},"disabled":true}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "")
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("weird")
	f.FormFocus = 2
	f.setFormVal("my-new-binary")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if !strings.Contains(f.FormErr, "already exists") {
		t.Errorf("an entry with no command/url still exists: FormErr = %q", f.FormErr)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if !strings.Contains(string(saved), `"TOKEN"`) || !strings.Contains(string(saved), `"disabled":true`) {
		t.Errorf("the existing entry was clobbered or re-enabled:\n%s", saved)
	}
}

func TestPanelKeepsItsConfigFile(t *testing.T) {
	dir := t.TempDir()
	dot := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(dot, []byte(mcpPanelFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	if d.McpPath != dot {
		t.Fatalf("panel file = %s, want %s", d.McpPath, dot)
	}
	// A plain mcp.json appears and the panel rebuilds (after a write): it
	// must stay on the file it was showing, not hop to the new one.
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.LoadMcpRows(d)
	if d.McpPath != dot {
		t.Errorf("panel hopped files: %s", d.McpPath)
	}
	if len(d.McpNames) != 2 {
		t.Errorf("servers changed under the user: %v", d.McpNames)
	}
}

func TestFormSaveTargetsThePanelFile(t *testing.T) {
	dir := t.TempDir()
	dot := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(dot, []byte(mcpPanelFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	if d.McpPath != dot {
		t.Fatalf("panel should be showing %s, got %s", dot, d.McpPath)
	}
	// A plain mcp.json appears while the panel is open. The panel is
	// showing .mcp.json, so that is where the edit must land — a fresh
	// resolve would pick the new file and split the configuration.
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	openForm(m, "vault-tools")
	f := m.Dialogs[0]
	f.FormFocus = 2
	f.setFormVal("edited")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	inDot, _ := os.ReadFile(dot)
	if !strings.Contains(string(inDot), `"edited"`) {
		t.Errorf("edit did not land in the panel's file:\n%s", inDot)
	}
	inPlain, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if strings.Contains(string(inPlain), "edited") {
		t.Errorf("edit leaked into the other config file:\n%s", inPlain)
	}
}

func TestHeadersSurviveAToggleAndAreEditable(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{
		"remote":{"type":"http","url":"https://r.test/mcp","headers":{"Authorization":"Bearer t"}},
		"odd":{"type":"http","url":"https://o.test/mcp","headers":{"X-Retry":3}}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	// A header value the panel cannot model (a number) survives a toggle…
	if _, err := m.McpToggle("", "odd"); err != nil {
		t.Fatal(err)
	}
	// …and so does a plain string header.
	if _, err := m.McpToggle("", "remote"); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if !strings.Contains(string(out), `"X-Retry"`) {
		t.Errorf("toggle dropped an unmodelled header:\n%s", out)
	}
	if !strings.Contains(string(out), `"Authorization"`) {
		t.Errorf("toggle dropped a modelled header:\n%s", out)
	}
	// The form can read, edit and clear the ones it can model.
	m.OpenMcpPanel()
	openForm(m, "remote")
	f := m.Dialogs[0]
	if !strings.Contains(mcpFormValues(f)["Headers"], "Authorization") {
		t.Errorf("headers prefill = %q", mcpFormValues(f)["Headers"])
	}
	f.FormFocus = 6
	f.setFormVal("") // touched + blank = clear
	def, err := mcpDefFromForm(f, pirpc.McpDef{})
	if err != nil {
		t.Fatal(err)
	}
	if def.Headers == nil || len(def.Headers) != 0 {
		t.Errorf("cleared headers should be an explicit empty map: %#v", def.Headers)
	}
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	saved, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if strings.Contains(string(saved), `"Authorization"`) {
		t.Errorf("cleared headers still present:\n%s", saved)
	}
	if !strings.Contains(string(saved), `"X-Retry"`) {
		t.Errorf("untouched server's headers were collateral damage:\n%s", saved)
	}
}

func TestFormEditKeepsDisabledAndUntouchedRows(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"odd":{
		"command":"c","disabled":true,
		"args":["--port","1"],"env":{"PORT":"8080","N":3}}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "odd")
	f := m.Dialogs[0]
	// Fix the command only. Args/Env/Enabled are never typed, so the save
	// must carry the stored values, not delete them.
	f.FormFocus = 2
	f.setFormVal("c2")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	for _, want := range []string{`"command": "c2"`, `"--port"`, `"PORT"`, `3`, `"disabled": true`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("form save lost %q:\n%s", want, out)
		}
	}
}

func TestFormCanClearArgsAndEnv(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "vault-tools")
	f := m.Dialogs[0]
	clear := func(field string) {
		for i, name := range f.Options {
			if name == field {
				f.FormFocus = i
				f.setFormVal("") // touched, then blanked
			}
		}
	}
	clear("Args")
	clear("Env")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	got := doc.Get("vault-tools")
	if len(got.Args) != 0 || len(got.Env) != 0 {
		t.Errorf("cleared rows came back: args=%v env=%v", got.Args, got.Env)
	}
	if got.Command == "" {
		t.Error("clearing args/env must not disturb the command")
	}
	saved, _ := os.ReadFile(path)
	if strings.Contains(string(saved), `"args"`) || strings.Contains(string(saved), `"env"`) {
		t.Errorf("keys still in the file:\n%s", saved)
	}
}

func TestMcpToggleAppliesOnTopOfAHandEdit(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel() // parse the file now, like the panel does
	// The user edits mcp.json while the panel sits open. Every write path
	// re-reads before it mutates, so the toggle lands on top of the hand
	// edit rather than on the panel's stale copy.
	hand := `{"mcpServers":{"vault-tools":{"command":"hand-edited"},"remote":{"type":"http","url":"https://example.test/mcp","disabled":true}}}`
	if err := os.WriteFile(path, []byte(hand), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.McpToggle("", "vault-tools"); err != nil {
		t.Fatal(err)
	}
	saved, _ := os.ReadFile(path)
	if !strings.Contains(string(saved), `"hand-edited"`) {
		t.Errorf("hand edit was overwritten:\n%s", saved)
	}
	if !strings.Contains(string(saved), `"disabled": true`) {
		t.Errorf("toggle did not apply:\n%s", saved)
	}
	// The CAS still guards the rename itself: a doc parsed before the
	// hand edit refuses to save over it (see TestSaveRefusesWhenFileChanged).
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	doc.Path = path
	if err := doc.SaveIfUnchanged(path, []byte(hand)); err == nil {
		t.Error("a stale doc should refuse to save")
	}
}

func TestASpaceDoesNotClearStoredValues(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"odd":{"command":"c","args":[1,2],"env":{"N":3}}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "odd")
	f := m.Dialogs[0]
	// args/env prefill blank (unmodellable shape). A stray space must not
	// read as "clear this".
	for _, field := range []string{"Args", "Env"} {
		f.FormFocus = indexOfField(f, field)
		f.setFormVal(f.formVal() + " ")
	}
	if f.FormTouched["Args"] || f.FormTouched["Env"] {
		t.Errorf("whitespace marked a row touched: %v", f.FormTouched)
	}
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if !strings.Contains(string(out), `"args"`) || !strings.Contains(string(out), `"N": 3`) {
		t.Errorf("a stray space cleared stored values:\n%s", out)
	}
}

func TestRenameMovesTheEntry(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "vault-tools")
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("knowledge")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(doc.Servers(), ","); got != "remote,knowledge" {
		t.Errorf("rename left a duplicate or lost an entry: %q", got)
	}
	if doc.Get("knowledge").Command == "" {
		t.Error("renamed entry lost its command")
	}
}

func TestHeadersRejectedOnStdio(t *testing.T) {
	m := mcpPanelModel(t)
	openForm(m, "")
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("x")
	f.FormFocus = 2
	f.setFormVal("bin")
	f.FormFocus = indexOfField(f, "Headers")
	f.setFormVal(`{"A":"1"}`)
	if _, err := mcpDefFromForm(f, pirpc.McpDef{}); err == nil ||
		!strings.Contains(err.Error(), "http/sse") {
		t.Errorf("headers on stdio should be rejected, got %v", err)
	}
}

func TestTypingFiltersTheServerList(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rem")}, d)
	m = ptr(mm.(Model))
	if got := strings.Join(d.McpNames, ","); got != "remote" {
		t.Errorf("typing should narrow the server list, got %q", got)
	}
	if d.McpSelected() != "remote" {
		t.Errorf("selection = %q", d.McpSelected())
	}
	// Backspace widens it again ("re" still matches remote, not vault-tools).
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyBackspace}, d)
	m = ptr(mm.(Model))
	if got := strings.Join(d.McpNames, ","); got != "remote" {
		t.Errorf("backspace should keep matching remote, got %q", got)
	}
	// Clearing the filter restores the whole list.
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyBackspace}, d)
	m = ptr(mm.(Model))
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyBackspace}, d)
	m = ptr(mm.(Model))
	if len(d.McpNames) != 2 {
		t.Errorf("an empty filter should show every server, got %v", d.McpNames)
	}
	// No match: the pane says so and no server is selected.
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("zzz")}, d)
	m = ptr(mm.(Model))
	if len(d.McpNames) != 0 || !strings.Contains(d.Provs[0], "no servers") {
		t.Errorf("unmatched filter = %v / %v", d.McpNames, d.Provs)
	}
}

func TestClearingAStaysClearedAfterASpace(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"s":{"command":"c","args":["a"]}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "s")
	f := m.Dialogs[0]
	f.FormFocus = indexOfField(f, "Args")
	f.setFormVal("")  // cleared
	f.setFormVal(" ") // …and a space afterwards must not put it back
	if !f.FormTouched["Args"] {
		t.Fatalf("the clear was un-touched: %v", f.FormTouched)
	}
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	if f.FormErr != "" {
		t.Fatalf("save failed: %s", f.FormErr)
	}
	out, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if strings.Contains(string(out), `"args"`) {
		t.Errorf("args came back:\n%s", out)
	}
}

func TestFormPrefillsFromThePanelFile(t *testing.T) {
	dir := t.TempDir()
	// Two config files with the same server name and different commands.
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"),
		[]byte(`{"mcpServers":{"foo":{"command":"from-dot","args":["a"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"),
		[]byte(`{"mcpServers":{"foo":{"command":"from-plain"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	// The panel pins mcp.json (the precedence winner)…
	if d.McpPath != filepath.Join(dir, "mcp.json") {
		t.Fatalf("panel file = %s", d.McpPath)
	}
	openForm(m, "foo")
	f := m.Dialogs[0]
	if got := mcpFormValues(f)["Command"]; got != "from-plain" {
		t.Errorf("prefill came from the wrong file: %q", got)
	}
	// Now pin the dot-file and confirm the prefill follows the panel.
	m2 := mcpPanelModel(t)
	m2.OpenMcpPanel()
	d2 := m2.Dialogs[0]
	d2.McpPath = filepath.Join(dir, ".mcp.json")
	m2.LoadMcpRows(d2)
	openForm(m2, "foo")
	f2 := m2.Dialogs[0]
	if got := mcpFormValues(f2)["Command"]; got != "from-dot" {
		t.Errorf("prefill ignored the panel's file: %q", got)
	}
	if got := mcpFormValues(f2)["Args"]; got != "a" {
		t.Errorf("args prefill = %q", got)
	}
}

func TestFilterKeepsDetailsAndArmsAligned(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{
		"aaa":{"command":"cmd-aaa","args":["1"]},
		"bbb":{"command":"cmd-bbb","args":["2"],"disabled":true}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	// Arm a remove, then navigate with the filter: the arm must not
	// survive, or a later Enter deletes with no fresh confirmation.
	if m.ArmMcpRemove("aaa") {
		t.Fatal("first press should only arm")
	}
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("bb")}, d)
	m = ptr(mm.(Model))
	if m.mcpArmName != "" {
		t.Errorf("typing must invalidate an armed remove, arm = %q", m.mcpArmName)
	}
	// The filtered view keeps names, defs and labels parallel: the DETAILS
	// column must show bbb, not aaa.
	if got := strings.Join(d.McpNames, ","); got != "bbb" {
		t.Fatalf("filtered names = %q", got)
	}
	if got := d.McpDef().Command; got != "cmd-bbb" {
		t.Errorf("details show the wrong server: %q", got)
	}
	lines := stripANSI(strings.Join(m.mcpDetailLines(d, 40), "\n"))
	if !strings.Contains(lines, "cmd-bbb") || strings.Contains(lines, "cmd-aaa") {
		t.Errorf("detail column mismatched:\n%s", lines)
	}
	if !strings.Contains(strings.Join(d.Descs, "|"), "disabled · Enter enables") {
		t.Errorf("toggle label came from the wrong server: %v", d.Descs)
	}
}

func TestTypingAfterAFailedLoadDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "mcp.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	if len(d.McpAll) != 0 || len(d.McpAllDefs) != 0 {
		t.Fatalf("a failed load must clear its source lists: %v", d.McpAll)
	}
	// The whole point: typing after a failure used to index a stale
	// McpAll against an emptied McpDefs.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, d)
	m = ptr(mm.(Model))
	if d.McpSelected() != "" {
		t.Errorf("no server should be selected, got %q", d.McpSelected())
	}
}

func TestFilteringKeepsTheActionsReachable(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rem")}, d)
	m = ptr(mm.(Model))
	if len(d.FIdx) != len(d.Options) {
		t.Fatalf("filtering a server must not empty the action pane: %d of %d",
			len(d.FIdx), len(d.Options))
	}
	for _, want := range []string{"Toggle enabled", "Edit server…", "Remove server…"} {
		found := false
		for i, o := range d.Options {
			if o == want {
				_, found = d.FIdx[i], true
				break
			}
		}
		if !found {
			t.Errorf("%q is not reachable while filtering: %v", want, d.FIdx)
		}
	}
}

func TestReloadKeepsTheServerSelectedByName(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"aaa":{"command":"a"},"bbb":{"command":"b"},"ccc":{"command":"c"}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	d.ProvCursor = 2 // ccc
	if got := d.McpSelected(); got != "ccc" {
		t.Fatalf("setup: selected %q", got)
	}
	// Deleting a server AHEAD of the selection shifts every later index:
	// only name-based restoration keeps the same server highlighted.
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"),
		[]byte(`{"mcpServers":{"bbb":{"command":"b"},"ccc":{"command":"c"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.ReloadMcpPanel(d)
	if got := d.McpSelected(); got != "ccc" {
		t.Errorf("after deleting a server ahead of it, selection = %q", got)
	}
	// A rename must also leave the renamed server highlighted.
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"),
		[]byte(`{"mcpServers":{"bbb":{"command":"b"},"zzz":{"command":"c"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.ReloadMcpPanel(d)
	if got := d.McpSelected(); got != "zzz" {
		t.Errorf("after a rename the cursor should follow the name, got %q", got)
	}
	// Deleting the selected server clamps to something valid.
	if err := os.Remove(filepath.Join(dir, "mcp.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"),
		[]byte(`{"mcpServers":{"aaa":{"command":"a"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.ReloadMcpPanel(d)
	if got := d.McpSelected(); got != "aaa" {
		t.Errorf("after deleting the selected server, selection = %q", got)
	}
	if len(d.FIdx) == 0 {
		t.Error("actions should still be reachable")
	}
}

func TestStalePanelDoesNotResurrectServersAfterAFailedReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(`{"mcpServers":{"aaa":{"command":"a"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	d := m.Dialogs[0]
	if len(d.McpNames) != 1 {
		t.Fatalf("setup: %v", d.McpNames)
	}
	// The file becomes unreadable and the panel reloads: the stale server
	// must disappear rather than be offered for action.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	m.LoadMcpRows(d)
	if len(d.McpNames) != 0 || d.McpSelected() != "" {
		t.Errorf("stale servers survived a failed reload: %v", d.McpNames)
	}
	for _, p := range d.Payload {
		if strings.HasPrefix(p, "mcp:toggle") || strings.HasPrefix(p, "mcp:remove") {
			t.Errorf("a failed load must not offer destructive actions: %v", d.Payload)
		}
	}
	// Typing on a failed panel stays safe.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}, d)
	m = ptr(mm.(Model))
	if d.McpSelected() != "" {
		t.Errorf("typing on a failed panel selected %q", d.McpSelected())
	}
}

func TestHubShowsPanelChangesOnEsc(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true, Scope: "global",
			Transport: "/opt/homebrew/bin/bun run index.ts", Tools: []string{"vault_search"}},
	}})
	m.OpenHubSection(PsecMCP)
	hub := m.Dialogs[0]
	if !strings.Contains(strings.Join(hub.Descs, "|"), "connected · 1 tool") {
		t.Fatalf("pre-panel state = %v", hub.Descs)
	}
	// The editor is open on top of the hub and the user disables the
	// server there.
	m.OpenMcpPanelOn("vault-tools")
	panel := m.Dialogs[0]
	if _, err := m.McpToggle(panel.McpPath, "vault-tools"); err != nil {
		t.Fatal(err)
	}
	// A write invalidates what pi told us — otherwise the section would
	// keep reporting "connected" for a server that is now off.
	if !m.McpInfoStale() {
		t.Error("the list must be marked stale after a write")
	}
	// Escape back to the hub: its detail column already has the new file.
	mm, _ := m.dismissDialog(panel)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 || m.Dialogs[0] != hub {
		t.Fatalf("hub should be back on top: %+v", m.Dialogs)
	}
	// The list still shows what pi last told us (re-listing is a keypress
	// away, and pi decides the new state), and the hub is back on top.
	if !strings.Contains(strings.Join(hub.Descs, "|"), "connected · 1 tool") {
		t.Errorf("the section lost its rows: %v", hub.Descs)
	}
	if !m.McpInfoStale() {
		t.Error("the list should still be stale, so the next key re-reads it")
	}
	_ = path
}

func TestHubRefreshesAfterClosingTheForm(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenHubSection(PsecMCP)
	m.OpenMcpPanel()
	openForm(m, "") // form on top of the panel on top of the hub
	f := m.Dialogs[0]
	f.FormFocus = 0
	f.setFormVal("filesystem")
	f.FormFocus = 2
	f.setFormVal("npx")
	mm, _ := m.saveMcpForm(f)
	m = ptr(mm.(Model))
	// Esc closes the panel; the hub underneath must know about the new server.
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEsc}, m.Dialogs[0])
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 1 {
		t.Fatalf("hub should be back on top, %d dialogs", len(m.Dialogs))
	}
	hub := m.Dialogs[0]
	if got := strings.Join(hub.Options, "|"); !strings.Contains(got, "filesystem") {
		t.Errorf("hub did not pick up the added server: %v", hub.Options)
	}
}

func TestMcpFormRenders(t *testing.T) {
	m := mcpPanelModel(t)
	openForm(m, "")
	f := m.Dialogs[0]
	f.FormFocus = 0
	out := stripANSI(m.renderDialog())
	for _, want := range []string{"Add MCP server", "Name:", "Transport:", "Env:"} {
		if !strings.Contains(out, want) {
			t.Errorf("form render missing %q:\n%s", want, out)
		}
	}
	f.FormErr = "name is required"
	if out := stripANSI(m.renderDialog()); !strings.Contains(out, "name is required") {
		t.Errorf("form error not rendered:\n%s", out)
	}
}

// rowNamed puts the cursor on a row by its visible label.
func rowNamed(t *testing.T, d *Dialog, label string) int {
	t.Helper()
	for i, o := range d.Options {
		if o == label {
			return i
		}
	}
	t.Fatalf("row %q not in %v", label, d.Options)
	return 0
}

func TestHubMcpSectionIsPiServerListThenServerMenu(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.MCP = nil // the file snapshot must not be what the section shows
	m.SetMcpInfo(McpMsg{
		Servers: []pirpc.McpServerInfo{
			{Name: "vault-tools", State: "connected", Enabled: true, Scope: "global",
				Transport: "/opt/homebrew/bin/bun run index.ts", Tools: []string{"vault_search"}},
			{Name: "slack", State: "failed", Enabled: true, Scope: "global",
				Transport: "https://slack.test/mcp", Error: "Failed to resolve env \"SLACK_BOT_TOKEN\""},
			{Name: "notion", State: "needs-auth", Enabled: true, Scope: "project",
				Transport: "https://notion.test/mcp"},
			{Name: "old", Enabled: false, Scope: "global", Transport: "old"},
		},
	})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]

	// pi's sort: the ones needing the user first (needs-auth, failed), then
	// connected, then disabled last; alphabetical inside a rank.
	wantOrder := []string{"notion", "slack", "vault-tools", "old"}
	for i, o := range d.Options[:len(wantOrder)] {
		if o != wantOrder[i] {
			t.Fatalf("order = %v, want %v (attention first, then name)", d.Options, wantOrder)
		}
	}
	// pi's row description: state · exposure · scope.
	descs := strings.Join(d.Descs, "|")
	for _, want := range []string{
		"needs sign-in · codemode · project",
		`failed: Failed to resolve env "SLACK_BOT_TOKEN" · codemode · global`,
		"connected · 1 tool · codemode · global",
		"disabled · codemode · global",
	} {
		if !strings.Contains(descs, want) {
			t.Errorf("row description missing %q: %v", want, d.Descs)
		}
	}

	// Enter opens that server's menu — pi's second level, and only that
	// server's actions.
	// pi's four, in pi's order, plus the two rows that are ours (the
	// entry edit and the remove gate — pi has no menu for either).
	if got := strings.Join(mcpActionsFor(t, m, d, "notion"), "|"); got !=
		"Sign in|Reconnect|Exposure|Disable|Edit server…|Remove server…" {
		t.Errorf("notion's actions = %q", got)
	}
	for i, o := range d.McpAct {
		if !strings.Contains(d.McpActPayload[i], "@notion") {
			t.Errorf("action %q targets %q, not the highlighted server", o, d.McpActPayload[i])
		}
	}
	// pi's detail block, in pane 3 as label/value rows.
	mcpActionsFor(t, m, d, "notion")
	detail := stripANSI(strings.Join(m.mcpHubDetailLines(d, 44), "\n"))
	for _, want := range []string{"State", "needs sign-in", "Exposure", "codemode", "Scope", "project"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail block missing %q:\n%s", want, detail)
		}
	}
	// It is pane 3, not a banner above the panes: the message line is one
	// line of hints and the block is part of the column.
	if strings.Count(d.Message, "\n") > 0 {
		t.Errorf("the details must not sit above the panes: %q", d.Message)
	}

	// The state the list cannot express: a failed server shows its error.
	mcpActionsFor(t, m, d, "slack")
	if !strings.Contains(strings.Join(m.mcpHubDetailLines(d, 100), "\n"), "SLACK_BOT_TOKEN") {
		t.Errorf("the failure must be visible in pane 3:\n%s", m.mcpHubDetailLines(d, 44))
	}
	// A disabled server offers exactly one action.
	if got := strings.Join(mcpActionsFor(t, m, d, "old"), "|"); got != "Enable|Edit server…|Remove server…" {
		t.Errorf("disabled server's actions = %q", got)
	}
	// A connected stdio server has no Sign out (no stored credentials).
	for _, o := range mcpActionsFor(t, m, d, "vault-tools") {
		if o == "Sign out" {
			t.Error("a stdio server must not offer Sign out")
		}
	}
	// Esc leaves the actions column before it leaves the hub.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRight}, d)
	m = ptr(mm.(Model))
	if !d.McpActFocus {
		t.Fatal("→ did not focus the actions column")
	}
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus {
		t.Error("Esc did not leave the actions column")
	}
	if len(m.Dialogs) != 1 {
		t.Errorf("Esc closed the hub instead of leaving the column: %+v", m.Dialogs)
	}
}

func TestHubMcpSectionTriggersPiListWhenItHasNeverRun(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.UseBuiltins([]Builtin{{Name: BuiltinMcpList, Run: func(_ *Model, _ string) tea.Cmd {
		return func() tea.Msg { return McpMsg{} }
	}}}, nil)
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	if !m.McpInfoStale() {
		t.Fatal("no list yet, so it is stale")
	}
	mm, cmd := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
	_ = mm
	if cmd == nil {
		t.Fatal("sitting on the MCP section must ask pi for its list")
	}
	// Once a list has landed, the section stops re-asking.
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{{Name: "a"}}})
	if cmd := m.mcpListIfStale(d); cmd != nil {
		t.Error("a fresh list must not be re-read on every keystroke")
	}
	// The notice survives to the hub: one bad server is not a total failure.
	m.SetMcpInfo(McpMsg{Notice: "1 of 2 MCP servers need attention — the list itself is complete"})
	if !strings.Contains(m.McpInfoNotice, "1 of 2") {
		t.Errorf("notice = %q", m.McpInfoNotice)
	}
}

// The review that found these: the hub's action group used to be aimed by
// a row INDEX into the unfiltered server list, so a filter could leave
// Remove pointing at a server that was not on screen. The payload is the
// only authority.

func TestHubRefreshDoesNotPushPiModal(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenHubSection(PsecMCP)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{{Name: "alpha", State: "connected", Enabled: true}}})
	m.openMcpListMsg(McpMsg{
		Options: []string{"alpha"}, Descs: []string{"connected · 1 tool · codemode · global"},
		Servers: []pirpc.McpServerInfo{{Name: "alpha", State: "connected", Enabled: true}},
	})
	kinds := ""
	for _, d := range m.Dialogs {
		kinds += d.Kind + " "
	}
	if strings.Contains(kinds, "mcp ") || strings.Contains(kinds, " mcp") {
		t.Fatalf("a list for the hub must not open pi's manager: %q", kinds)
	}
	// pi's own manager still owns the screen when IT is open.
	m.SetMcpRows(McpMsg{
		Options: []string{"alpha"}, Descs: []string{"connected"},
		Servers: []pirpc.McpServerInfo{{Name: "alpha", State: "connected", Enabled: true}},
	})
	before := len(m.Dialogs)
	m.openMcpListMsg(McpMsg{
		Options: []string{"alpha", "bravo"}, Descs: []string{"connected", "failed"},
		Servers: []pirpc.McpServerInfo{{Name: "alpha", State: "connected", Enabled: true},
			{Name: "bravo", State: "failed", Enabled: true}},
	})
	if len(m.Dialogs) != before {
		t.Fatalf("pi's manager must be updated, not stacked on: %d → %d", before, len(m.Dialogs))
	}
	if !strings.Contains(strings.Join(m.Dialogs[0].Options, ","), "bravo") {
		t.Errorf("pi's manager did not get the new rows: %v", m.Dialogs[0].Options)
	}
}

func TestHubArmedRemoveDoesNotSurviveClosingTheEditor(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenHubSection(PsecMCP)
	m.OpenMcpPanelOn("vault-tools")
	if m.ArmMcpRemove("vault-tools") {
		t.Fatal("first press must only arm")
	}
	// The editor closes; the hub underneath is a different surface.
	mm, _ := m.dismissDialog(m.Dialogs[0])
	m = ptr(mm.(Model))
	if m.ArmMcpRemove("vault-tools") {
		t.Fatal("the gate was armed on another surface — the first Enter must not delete")
	}
}

func TestHubWritesGoToThePinnedFile(t *testing.T) {
	dir := t.TempDir()
	src := `{"mcpServers":{"victim":{"command":"v"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	m := mcpPanelModel(t)
	m.OpenMcpPanelOn("victim")
	panel := m.Dialogs[0]
	if panel.McpPath != filepath.Join(dir, ".mcp.json") {
		t.Fatalf("panel is showing %q", panel.McpPath)
	}
	// A plain mcp.json appears (another tool, a migration, a checkout).
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte(`{"mcpServers":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.McpToggle(panel.McpPath, "victim"); err != nil {
		t.Fatal(err)
	}
	pinned, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(pinned), `"victim"`) {
		t.Errorf("the toggle left the file on screen:\n%s", pinned)
	}
	fresh, _ := os.ReadFile(filepath.Join(dir, "mcp.json"))
	if strings.Contains(string(fresh), `"victim"`) {
		t.Errorf("the toggle wrote to a file the panel never showed:\n%s", fresh)
	}
}

func TestHubMcpSectionShowsPiErrorSlot(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{
		ConfigErrs: []string{"mcp.json: bad is not a valid server name"},
		Servers:    []pirpc.McpServerInfo{{Name: "alpha", State: "connected", Enabled: true}},
	})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	// pi puts config errors in the error slot, prefixed "config:". It does
	// NOT show the list's exit status here: a non-zero exit just means one
	// server is broken, and that server says so in its own row.
	if !strings.Contains(d.Message, "config: mcp.json: bad is not a valid server name") {
		t.Errorf("config errors must be visible in the message slot: %q", d.Message)
	}
	if !strings.Contains(d.Message, "Esc close") {
		t.Errorf("the key hints must survive the errors: %q", d.Message)
	}
	if !strings.Contains(strings.Join(d.Descs, "|"), "connected") {
		t.Errorf("the healthy server still lists: %v", d.Descs)
	}
}

func TestHubMcpBadgeCountsWhatItLists(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.MCP = []McpServer{{Name: "stale"}} // the file snapshot is not what is shown
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "a", Enabled: true}, {Name: "b", Enabled: true}, {Name: "c", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	if got := psecCount(m, PsecMCP); got != 3 {
		t.Errorf("badge = %d, want the 3 servers listed", got)
	}
}

// intInSlice is a tiny membership helper for the filter tests.
func intInSlice(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// F4: a server pi found in ANOTHER file must not be edited in the agent
// dir's copy.
func TestHubEntryEditsAimAtPiSourceFile(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project.json")
	if err := os.WriteFile(project, []byte(`{"mcpServers":{"proj":{"command":"p"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir()) // agent dir is EMPTY
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "proj", State: "connected", Enabled: true, Scope: "project", Source: project},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	// The row says which file the edits go to.
	for i, o := range d.Options {
		if o == "Edit server…" && !strings.Contains(d.Descs[i], project) {
			t.Errorf("edit row must name the file it writes: %q", d.Descs[i])
		}
	}
}

// visibleRows lists the row indices a filter left on screen.
func visibleRows(d *Dialog) []int {
	out := []int{}
	if len(d.FIdx) == 0 {
		for i := range d.Options {
			out = append(out, i)
		}
		return out
	}
	for _, i := range d.FIdx {
		if i < len(d.Options) {
			out = append(out, i)
		}
	}
	return out
}

// The two ways the second pane stopped navigating. Both are the kind of
// bug that reads as "the pane is dead" rather than "the pane is wrong".
func TestHubArrowKeysAreNeverSwallowedByTheListFetch(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Transport: "a"},
		{Name: "bravo", State: "connected", Enabled: true, Transport: "b"},
	}})
	// Age it: an in-flight refresh is the case that broke navigation, and
	// a list that is merely absent would take a different path.
	m.McpInfoAt = time.Now().Add(-2 * mcpInfoTTL)
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	if len(d.FIdx) < 2 {
		t.Fatalf("need at least two rows to navigate: %v", d.Options)
	}
	if !m.McpInfoStale() {
		t.Fatal("an aged list is stale, so the fetch would be requested")
	}
	// Pi's list is slow (90s timeout) or missing entirely: the pane must
	// still navigate while the request is in flight.
	before := d.Cursor
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
	m = ptr(mm.(Model))
	if d.Cursor == before {
		t.Fatal("↓ was swallowed by the MCP list fetch")
	}
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
	m = ptr(mm.(Model))
	if d.Cursor == before+1 {
		t.Fatal("↓ was swallowed again — one keystroke, one list request")
	}
	// A second fetch must not be scheduled while one is in flight.
	if cmd := m.mcpListIfStale(d); cmd != nil {
		t.Error("the list must not be re-requested on every keystroke")
	}
	// The response still lands and is recorded.
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{{Name: "alpha", Enabled: true}}})
	if len(m.McpInfo) != 1 {
		t.Error("the list did not land")
	}
}

func TestHubActionsColumnIsAPane(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.MCP = nil
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "notion", State: "needs-auth", Enabled: true, Transport: "https://notion.test/mcp"},
		{Name: "vault-tools", State: "connected", Enabled: true, Transport: "bun run index.ts",
			Tools: []string{"vault_search"}},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]

	// The column shows the highlighted server's actions, and they follow
	// the cursor — no second menu, no per-server repetition.
	if got := strings.Join(mcpActionsFor(t, m, d, "notion"), "|"); got !=
		"Sign in|Reconnect|Exposure|Disable|Edit server…|Remove server…" {
		t.Errorf("notion's column = %q", got)
	}
	if got := strings.Join(mcpActionsFor(t, m, d, "vault-tools"), "|"); got != "Tools|Reconnect|Exposure|Disable|Edit server…|Remove server…" {
		t.Errorf("vault-tools' column = %q", got)
	}

	// → takes focus; ↑↓ then move the ACTION cursor, not the server.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRight}, d)
	m = ptr(mm.(Model))
	if !d.McpActFocus {
		t.Fatal("→ did not focus the actions column")
	}
	// Focusing must not move the server cursor.
	server := d.Cursor
	before := d.McpActCursor
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
	m = ptr(mm.(Model))
	if d.McpActCursor == before {
		t.Error("↓ in the column did not move the action cursor")
	}
	if d.Cursor != server {
		t.Errorf("↓ in the column moved the server cursor: %d → %d", server, d.Cursor)
	}
	// Every action in the column is reachable (re-select the server first:
	// the ↑↓ above walked the column, and the server cursor is on it).
	mcpActionsFor(t, m, d, "notion")
	seen := map[string]bool{}
	for n := 0; n < len(d.McpAct)+2; n++ {
		seen[d.McpAct[d.McpActCursor]] = true
		mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
		m = ptr(mm.(Model))
		if d.McpActCursor < 0 || d.McpActCursor >= len(d.McpAct) {
			t.Fatalf("action cursor left the column: %d/%d", d.McpActCursor, len(d.McpAct))
		}
	}
	for _, want := range []string{"Sign in", "Reconnect", "Exposure", "Disable"} {
		if !seen[want] {
			t.Errorf("%q unreachable in the column: %v", want, d.McpAct)
		}
	}
	// ← steps back out; Esc then closes the hub.
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyLeft}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus {
		t.Error("← did not leave the column")
	}
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != 0 {
		t.Errorf("Esc did not close the hub: %+v", m.Dialogs)
	}
}

func TestHubEnterOnAServerFocusesItsActions(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "notion", State: "needs-auth", Enabled: true, Transport: "https://notion.test/mcp"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	// Enter on a server is the move pi makes (open its actions) — here
	// the actions are already on screen, so it just takes the focus.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEnter}, d)
	m = ptr(mm.(Model))
	if !d.McpActFocus {
		t.Fatal("Enter on a server did not focus its actions")
	}
	// It must NOT have run anything: no dialog, no pending action.
	if len(m.Dialogs) != 1 {
		t.Errorf("Enter on a server opened something: %+v", m.Dialogs)
	}
	if _, ok := McpHubAction(d); ok {
		t.Error("Enter on a server queued an action to run")
	}
}

func TestHubArrowKeysNavigateWithAFilterActive(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.MCP = nil
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Transport: "http", Tools: []string{"t1"}},
		{Name: "bravo", State: "connected", Enabled: true, Transport: "stdio"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	// Filter to one server, then arrow: with a sparse FIdx, a row index
	// mistaken for a cursor index lands on the wrong row.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("alpha")}, d)
	m = ptr(mm.(Model))
	visible := 0
	for i := range d.FIdx {
		if i < len(d.Options) && strings.Contains(d.Descs[i], "alpha") ||
			(i < len(d.Options) && strings.Contains(d.Payload[i], "alpha")) {
			visible++
		}
	}
	if visible == 0 {
		t.Fatalf("filter removed everything: FIdx=%v rows=%v", d.FIdx, d.Options)
	}
	for n := 0; n < 6; n++ {
		mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
		m = ptr(mm.(Model))
		i := psecCursor(d)
		if i < 0 || i >= len(d.Options) {
			t.Fatalf("cursor left the pane while filtering: %d, FIdx=%v", i, d.FIdx)
		}
		if d.Cursor >= len(d.FIdx) {
			t.Errorf("cursor %d is past FIdx %v", d.Cursor, d.FIdx)
		}
	}
}

// mcpRowUnder finds an action row by the server it sits under. The hub's
// MCP section is a tree, so a bare label ("Tools") appears once per
// server and a lookup has to say which one it means.
func mcpRowUnder(t *testing.T, d *Dialog, server, label string) int {
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
	return 0
}

// payloadUnder is mcpRowUnder's payload, in one step.
func payloadUnder(t *testing.T, d *Dialog, server, label string) string {
	t.Helper()
	i := mcpRowUnder(t, d, server, label)
	if i >= len(d.Payload) {
		t.Fatalf("no payload for %q under %q", label, server)
	}
	return d.Payload[i]
}

// Pane 3: pi's detail block plus the entry itself. Pi prints only the
// transport in its menu and sends you to /mcp to change anything; the
// settings hub is where the entry belongs.
func TestHubServerMenuShowsTheEntry(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true, Scope: "global",
			Source: filepath.Join(piAgentDir(), "mcp.json"), Transport: "bun run index.ts"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	mcpActionsFor(t, m, d, "vault-tools")
	block := strings.Join(m.mcpHubDetailLines(d, 44), "\n")
	for _, want := range []string{"State", "connected", "bun", "VAULT_ROOT", "mcp.json"} {
		if !strings.Contains(block, want) {
			t.Errorf("pane 3 missing %q:\n%s", want, block)
		}
	}
	// An edit made in the editor must reach this block: it reads the
	// snapshot, and a write refreshes the snapshot.
	doc, err := pirpc.LoadMcpConfig(filepath.Join(piAgentDir(), "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	def := doc.Get("vault-tools")
	def.Command = "TOTALLY-DIFFERENT"
	doc.Put(def)
	if err := doc.Save(filepath.Join(piAgentDir(), "mcp.json")); err != nil {
		t.Fatal(err)
	}
	m.refreshMcpHubDefs()
	mcpActionsFor(t, m, d, "vault-tools")
	if !strings.Contains(strings.Join(m.mcpHubDetailLines(d, 44), "\n"), "TOTALLY-DIFFERENT") {
		t.Errorf("the entry block is stale after an edit:\n%s", m.mcpHubDetailLines(d, 44))
	}
}

// mcpActionsFor puts the cursor on a server and returns its action rows —
// the third column is derived from the highlighted server, so this is how a
// test "opens" a server now.
func mcpActionsFor(t *testing.T, m *Model, d *Dialog, name string) []string {
	t.Helper()
	want := "@mcpsel:" + name
	m.loadRows(d)
	for f, ri := range d.FIdx {
		if payloadOf(d, ri) != want {
			continue
		}
		// The message is built from the cursor as it is when the rows are
		// built; the column from the cursor after they are assigned. Put
		// the cursor on the server for both, in that order.
		d.Cursor, d.McpActCursor = f, 0
		m.loadRows(d)
		d.Cursor = f
		m.refreshMcpActions(d)
		return d.McpAct
	}
	t.Fatalf("server %q not in %v", name, d.Options)
	return nil
}

// Enter on an action row must REACH the dispatcher. The flag alone is not
// enough: it is what tells the confirm handler to read the column, and
// without the dispatch after it every action was a no-op.
func TestHubEnterOnAnActionRunsIt(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "notion", State: "needs-auth", Enabled: true, Transport: "https://notion.test/mcp"},
	}})
	ran := ""
	var gotRow int
	m.UseBuiltins([]Builtin{{Name: "probe"}}, map[string]ConfirmFunc{
		"pconfig": func(m *Model, d *Dialog, ri int) (tea.Model, tea.Cmd) {
			if p, ok := McpHubAction(d); ok {
				ran = p
			}
			gotRow = ri
			return m, nil
		},
	})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	mcpActionsFor(t, m, d, "notion")
	// Focus the column, then put the action cursor on Sign in.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRight}, d)
	_ = mm
	for i, o := range d.McpAct {
		if o == "Sign in" {
			d.McpActCursor = i
		}
	}
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEnter}, d)
	_ = mm
	if ran != "@mcpact:signin@notion" {
		t.Errorf("Enter on an action ran %q, want the sign-in row", ran)
	}
	if gotRow < 0 {
		t.Error("the confirm handler was never reached")
	}
	// The flag is consumed, so a later Enter on the servers does not re-run it.
	ran = ""
	d.McpActFocus = false
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEnter}, d)
	_ = mm
	if ran != "" {
		t.Errorf("a stale action ran on a later Enter: %q", ran)
	}
}

// Pane 2 is names only: the state is in pane 3, and saying it twice in the
// row is noise.
func TestHubServerRowsAreNamesOnly(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.MCP = nil
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true, Scope: "global",
			Transport: "bun run index.ts", Tools: []string{"vault_search"}},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.winW = 160
	m.winH = 34
	row := func() string {
		i := psecCursor(d)
		if i < 0 {
			return "<none>"
		}
		return d.Options[i]
	}
	if row() != "vault-tools" {
		t.Fatalf("row = %q", row())
	}
	out := stripANSI(m.renderPconfigDialog(d))
	// The desc stays on the row (the filter matches on it) but is not
	// drawn: pane 2 is the name list, pane 3 has the details. So check
	// the middle pane's band of the rendered line, not the data.
	for _, l := range strings.Split(out, "\n") {
		i := strings.Index(l, "vault-tools")
		if i < 0 {
			continue
		}
		rest := l[i:]
		if j := strings.Index(rest, "│"); j > 0 {
			rest = rest[:j]
		}
		if strings.Contains(rest, "connected") || strings.Contains(rest, "codemode") {
			t.Errorf("pane 2 repeats the details: %q", rest)
		}
	}
	if !strings.Contains(out, "vault-tools") {
		t.Error("the server name must show")
	}
	// The state is in pane 3, once.
	if !strings.Contains(out, "connected · 1 tool") {
		t.Error("pane 3 must carry the state")
	}
	if n := strings.Count(out, "connected · 1 tool"); n != 1 {
		t.Errorf("the state appears %d times: it belongs in pane 3 only", n)
	}
}

// Four things the live pass turned up.
func TestHubDisableKeepsTheServerSelectedAndTheDetailCurrent(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.MCP = nil
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Scope: "global", Transport: "a"},
		{Name: "bravo", State: "connected", Enabled: true, Scope: "global", Transport: "b"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	// Sitting on bravo, in its actions.
	mcpActionsFor(t, m, d, "bravo")
	if len(d.Options) < 2 || d.Options[0] != "alpha" {
		t.Fatalf("rows = %v", d.Options)
	}
	// bravo is disabled: pi moves it to the BOTTOM of the list (disabled
	// ranks last), and reports it disabled. The list may reorder — the
	// subject must not change.
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Scope: "global", Transport: "a"},
		{Name: "bravo", State: "disabled", Enabled: false, Scope: "global", Transport: "b"},
	}})
	if d.Options[0] != "alpha" || d.Options[1] != "bravo" {
		t.Fatalf("the list should have reordered: %v", d.Options)
	}
	if psecCursor(d) != 1 {
		t.Fatalf("focus moved off bravo: cursor=%d rows=%v", d.Cursor, d.Options)
	}
	// Pane 3 is headed with bravo and describes its new state.
	if head := m.detailHead(d); !strings.Contains(head, "bravo") {
		t.Errorf("pane 3 lost the subject: %q", head)
	}
	block := strings.Join(m.mcpHubDetailLines(d, 44), "\n")
	if !strings.Contains(block, "disabled") {
		t.Errorf("pane 3 did not update to the new state:\n%s", block)
	}
	if got := strings.Join(d.McpAct, "|"); got != "Enable|Edit server…|Remove server…" {
		t.Errorf("the actions did not follow the new state: %q", got)
	}
	if d.McpActCursor != 0 {
		t.Errorf("the action cursor should be on the first row, got %d", d.McpActCursor)
	}
}

func TestHubActionsColumnIsVisuallySeparatedFromTheDetail(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Transport: "a"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	lines := m.mcpHubPane(d, 44)
	actionsAt, blankBefore := -1, false
	for i, l := range lines {
		if strings.TrimSpace(l) == "ACTIONS" {
			actionsAt = i
		}
	}
	if actionsAt < 1 {
		t.Fatalf("no ACTIONS header: %q", lines)
	}
	blankBefore = strings.TrimSpace(lines[actionsAt-1]) == ""
	if !blankBefore {
		t.Errorf("no blank line between the detail and ACTIONS: %q", lines[actionsAt-2:actionsAt])
	}
}

func TestHubConnectedIsGreen(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "alpha", State: "connected", Enabled: true, Transport: "a", Tools: []string{"t"}},
		{Name: "bravo", State: "failed", Enabled: true, Transport: "b",
			Error: `Failed to resolve env "SLACK_BOT_TOKEN"`},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	// Green is decided by mcpHealthy, which the pane passes to the row
	// builder. (The test profile renders no colour, so asserting on the
	// escapes would pass either way.)
	if !mcpHealthy(pirpc.McpServerInfo{State: "connected", Enabled: true}) {
		t.Error("a connected, enabled server is healthy")
	}
	for _, s := range []pirpc.McpServerInfo{
		{State: "failed", Enabled: true},
		{State: "needs-auth", Enabled: true},
		{State: "disconnected", Enabled: true},
		{State: "connected", Enabled: false}, // disabled is the user's choice
		{State: "connecting", Enabled: true},
	} {
		if mcpHealthy(s) {
			t.Errorf("%+v must not read as healthy", s)
		}
	}
	// And the pane really is built from that decision.
	mcpActionsFor(t, m, d, "alpha")
	block := strings.Join(m.mcpHubDetailLines(d, 44), "\n")
	if !strings.Contains(block, "connected · 1 tool") {
		t.Errorf("the healthy server's block is wrong:\n%s", block)
	}
}

// The inline editor: pane 3 becomes the form, so editing never leaves
// settings for a different UI.
func TestHubInlineEditorSavesInPlace(t *testing.T) {
	path := mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true, Scope: "global",
			Source: path, Transport: "bun run index.ts"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")
	if !d.McpEdit.Active {
		t.Fatal("the editor did not open in pane 3")
	}
	if len(m.Dialogs) != 1 {
		t.Fatalf("an overlay was pushed: %+v", m.Dialogs)
	}
	// Prefilled from the file, focused on the command.
	d.McpEdit.Focus = 2
	if got := d.McpEditVal(); got != "/opt/homebrew/bin/bun" {
		t.Fatalf("command prefill = %q", got)
	}
	// Type: appends to the focused field.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")}, d)
	m = ptr(mm.(Model))
	if !strings.HasSuffix(d.McpEditVal(), "X") {
		t.Errorf("rune did not land: %q", d.McpEditVal())
	}
	// ↑↓ move between fields, they do not edit the old one.
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyDown}, d)
	m = ptr(mm.(Model))
	if d.McpEdit.Focus != 3 {
		t.Errorf("↓ did not move the field: focus=%d", d.McpEdit.Focus)
	}
	// Ctrl+S writes it.
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyCtrlS}, d)
	_ = mm
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "/opt/homebrew/bin/bunX") {
		t.Errorf("the edit was not written:\n%s", saved)
	}
	// The untouched rows survived: env and args are still there (the file
	// pretty-prints args across lines), and the other server is untouched.
	for _, want := range []string{`"VAULT_ROOT"`, `"run"`, "/src/index.ts", `"remote"`} {
		if !strings.Contains(string(saved), want) {
			t.Errorf("an untouched row was lost (%q):\n%s", want, saved)
		}
	}
	if d.McpEdit.Active {
		t.Error("the editor should be closed after a save")
	}
}

func TestHubInlineEditorRefusesAClobberAndGatesDiscard(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")

	// Esc on an untouched form closes it immediately.
	mm, _ := m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if d.McpEdit.Active {
		t.Fatal("Esc on an untouched form should close it")
	}

	// Type something: Esc must then ask once.
	m.OpenMcpEditHub("vault-tools")
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")}, d)
	m = ptr(mm.(Model))
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if !d.McpEdit.Active {
		t.Fatal("one Esc must not discard a typed form")
	}
	// The prompt is derived at render time, not stored in e.Err — a stored
	// one outlives the arm window and then promises a second Esc that only
	// re-arms. Assert on what the pane shows.
	if pane := stripANSI(strings.Join(mcpEditFormLines(d, 120), "\n")); !strings.Contains(pane, "Esc again") {
		t.Errorf("the gate must say what to do:\n%s", pane)
	}
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if d.McpEdit.Active {
		t.Fatal("the second Esc should discard")
	}

	// Renaming onto an existing entry is refused, not merged into it.
	m.OpenMcpEditHub("vault-tools")
	d.McpEdit.Focus = 0
	d.McpEditSet("remote") // the name row
	mm, _ = m.updatePconfigDialog(tea.KeyMsg{Type: tea.KeyCtrlS}, d)
	_ = mm
	if !d.McpEdit.Active {
		t.Fatal("a refused save must keep the editor open")
	}
	if !strings.Contains(d.McpEdit.Err, "already exists") {
		t.Errorf("error = %q", d.McpEdit.Err)
	}
	// ...and it must reach the pane. An error held only in the field is
	// an error the user never sees.
	if pane := stripANSI(strings.Join(mcpEditFormLines(d, 120), "\n")); !strings.Contains(pane, "already exists") {
		t.Errorf("the refusal never reaches the pane:\n%s", pane)
	}
	// A rejected save is a decision, not a reflex: it must not leave the
	// Esc gate armed, or the next Esc discards an edit whose error the
	// user just asked for.
	mm, _ = m.updateMcpEditHub(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if pane := stripANSI(strings.Join(mcpEditFormLines(d, 120), "\n")); strings.Contains(pane, "Esc again") {
		t.Errorf("gate chrome stands over the save error:\n%s", pane)
	}
	// A refused save disarms the gate, so the next Esc asks rather than
	// throws away: the user is mid-decision, not mid-reflex.
	if !d.McpEdit.Active {
		t.Fatal("one Esc after a refused save must not discard the edit")
	}
	saved, _ := os.ReadFile(mcpDocPath(piAgentDir()))
	if !strings.Contains(string(saved), `"vault-tools"`) {
		t.Errorf("the refused save still wrote:\n%s", saved)
	}

	// Same thing, but with the gate already armed when the save is
	// refused: the save must disarm it. Otherwise the very next Esc lands
	// inside the still-live window and discards an edit the user was in
	// the middle of rescuing.
	m.OpenMcpEditHub("vault-tools")
	d = m.Dialogs[0]
	d.McpEdit.Focus = 0
	d.McpEditSet("remote") // collides again
	mm, _ = m.updateMcpEditHub(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if !d.McpEdit.Active {
		t.Fatal("setup: the Esc must only arm")
	}
	mm, _ = m.updateMcpEditHub(tea.KeyMsg{Type: tea.KeyCtrlS}, d)
	m = ptr(mm.(Model))
	if !d.McpEdit.Active || !strings.Contains(d.McpEdit.Err, "already exists") {
		t.Fatalf("setup: want a refused save in an open editor, got active=%v err=%q", d.McpEdit.Active, d.McpEdit.Err)
	}
	mm, _ = m.updateMcpEditHub(tea.KeyMsg{Type: tea.KeyEsc}, d)
	m = ptr(mm.(Model))
	if !d.McpEdit.Active {
		t.Fatal("a refused save must disarm the gate: one Esc discarded the edit")
	}
}

func TestHubPane3RendersTheInlineEditor(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")
	lines := stripANSI(strings.Join(mcpEditFormLines(d, 60), "\n"))
	for _, want := range []string{"EDITING vault-tools", "Command", "/opt/homebrew/bin/bun",
		"Args", "Env", "Enabled", "Ctrl+S save", "Esc cancel"} {
		if !strings.Contains(lines, want) {
			t.Errorf("pane 3 missing %q:\n%s", want, lines)
		}
	}
	// And it replaces the detail + actions, not stacks on them.
	pane := strings.Join(m.mcpHubPane(d, 60), "\n")
	if strings.Contains(pane, "ACTIONS") || strings.Contains(pane, "Exposure") {
		t.Errorf("the form did not replace the actions column:\n%s", pane)
	}
}

// The armed remove must be announced on the Remove row itself: the
// instruction belongs to the action, and a line of text at the top of
// the panel is where nobody looks.
func TestHubArmedRemoveShowsOnTheRemoveRow(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	rm := -1
	for i, o := range d.McpAct {
		if o == "Remove server…" {
			rm = i
		}
	}
	if rm < 0 {
		t.Fatalf("no Remove row: %v", d.McpAct)
	}
	d.McpActFocus, d.McpActCursor = true, rm

	before := stripANSI(strings.Join(m.mcpHubPane(d, 120), "\n"))
	if strings.Contains(before, "Enter again") {
		t.Errorf("nothing is armed yet:\n%s", before)
	}
	if m.ArmMcpRemove("vault-tools") {
		t.Fatal("the first press must only arm")
	}
	after := stripANSI(strings.Join(m.mcpHubPane(d, 120), "\n"))
	if !strings.Contains(after, "press Enter again") {
		t.Errorf("the armed gate is not on the Remove row:\n%s", after)
	}
	if strings.Contains(d.Message, "Enter again") {
		t.Errorf("the prompt must not sit in the top message strip: %q", d.Message)
	}
	// Moving off the row takes the prompt with it.
	d.McpActCursor = 0
	if pane := stripANSI(strings.Join(m.mcpHubPane(d, 120), "\n")); strings.Contains(pane, "Enter again") {
		m.clearMcpArm()
		t.Errorf("the prompt outlived the row it was on:\n%s", pane)
	}
}

// The inline editor must say which field keystrokes land in: a bright
// label plus a block cursor after the value.
func TestHubInlineEditorShowsACursorOnTheFocusedField(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")

	lines := stripANSI(strings.Join(mcpEditFormLines(d, 70), "\n"))
	if !strings.Contains(lines, "bun▌") {
		t.Errorf("no cursor on the focused Command value:\n%s", lines)
	}
	if n := strings.Count(lines, "▌"); n != 1 {
		t.Errorf("exactly one field is being edited, got %d carets:\n%s", n, lines)
	}
	// The caret must land INSIDE the pane. Rows are 2 + (w-2) wide; one
	// column over and the right edge (the caret) is clipped away.
	for _, l := range strings.Split(stripANSI(strings.Join(mcpEditFormLines(d, 70), "\n")), "\n") {
		if got := lipgloss.Width(l); got > 70 {
			t.Errorf("row is %d wide, pane is 70 — the caret is clipped:\n%q", got, l)
		}
	}

	// It follows the focus, not the first field.
	d.McpEdit.Focus = indexOfFieldName(mcpFields, "Args")
	lines = stripANSI(strings.Join(mcpEditFormLines(d, 70), "\n"))
	if !strings.Contains(lines, "run /src/index.ts▌") {
		t.Errorf("the cursor did not follow the focus:\n%s", lines)
	}
}

// ← → must move the caret, and typing must land where the caret is —
// not always at the end of the field.
func TestHubInlineEditorCaretMovesAndTypesThere(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")

	if got := d.McpEditCur(); got != len("/opt/homebrew/bin/bun") {
		t.Errorf("a prefilled field opens with the caret at the end, got %d", got)
	}
	// Moving is not editing: the Esc gate must not arm on arrows alone.
	d.McpEditSetCur(0)
	d.McpEditCaret(-1)
	if d.mcpEditDirty() {
		t.Error("moving the caret dirtied the form")
	}
	// ←×8, then type: the rune lands inside the value, not at the end.
	orig := d.McpEditVal()
	d.McpEditSetCur(len(orig))
	d.McpEditCaret(-8)
	c := d.McpEditCur()
	d.McpEditInsert("/x")
	r := []rune(orig)
	if got, want := d.McpEditVal(), string(r[:c])+"/x"+string(r[c:]); got != want {
		t.Errorf("typed at the end, not at the caret: %q, want %q", got, want)
	}
	if got := d.McpEditCur(); got != c+2 {
		t.Errorf("caret = %d, want %d", got, c+2)
	}
	// Backspace takes the rune before the caret (the "x" of the inserted
	// "/x"); Delete takes the one at it (the "/"). Both leave the caret
	// where the deleted rune was.
	d.McpEditDelete(false)
	if got := d.McpEditVal(); got != string(r[:c])+"/"+string(r[c:]) {
		t.Errorf("backspace = %q, want %q", got, string(r[:c])+"/"+string(r[c:]))
	}
	if got := d.McpEditCur(); got != c+1 {
		t.Errorf("backspace moved the caret to %d, want %d", got, c+1)
	}
	d.McpEditInsert("/x") // the "/x" is back, caret just after it
	before, cc := d.McpEditVal(), d.McpEditCur()
	rb := []rune(before)
	d.McpEditDelete(true)
	if got, want := d.McpEditVal(), string(rb[:cc])+string(rb[cc+1:]); got != want {
		t.Errorf("delete = %q, want %q", got, want)
	}

	// And the drawn caret is where the model says it is.
	d.McpEditSetCur(0)
	line := stripANSI(strings.Join(mcpEditFormLines(d, 70), "\n"))
	if !strings.Contains(line, "▌"+d.McpEditVal()) {
		t.Errorf("caret not drawn at position 0:\n%s", line)
	}
	// ← at the start is a no-op, not a wrap.
	d.McpEditCaret(-1)
	if got := d.McpEditCur(); got != 0 {
		t.Errorf("caret went below zero: %d", got)
	}
}

// A caret index is a rune index: moving through a multi-byte character
// must not slice it in half.
func TestHubInlineEditorCaretIsRuneIndexed(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")
	d.McpEditSetCur(0)
	d.McpEditInsert("héllo→")
	d.McpEditSetCur(2)
	d.McpEditDelete(false)
	if got := d.McpEditVal(); got != "hllo→/opt/homebrew/bin/bun" {
		t.Errorf("a byte-indexed caret would leave a mojibake fragment: %q", got)
	}
}

// The overlay form gets the same caret: ← → move it, typing lands there,
// and an untouched field opens at the end (backspace trims, it does
// nothing).
func TestMcpFormCaretMovesAndTypesThere(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.OpenMcpPanel()
	openForm(m, "vault-tools")
	f := m.Dialogs[0]
	f.FormFocus = 2 // Command
	if got := f.formCur(); got != len(f.formVal()) {
		t.Errorf("a prefilled field opens with the caret at the end, got %d", got)
	}
	// An untouched field is not parked at Home: backspace trims.
	f.FormFocus = 1
	mm, _ := m.updateMcpForm(tea.KeyMsg{Type: tea.KeyBackspace}, f)
	_ = ptr(mm.(Model))
	if got := f.formVal(); got != "stdi" {
		t.Errorf("backspace on an untouched field = %q, want %q", got, "stdi")
	}
	f.FormFocus = 2
	for i := 0; i < 4; i++ {
		mm, _ = m.updateMcpForm(tea.KeyMsg{Type: tea.KeyLeft}, f)
		_ = ptr(mm.(Model))
	}
	mm, _ = m.updateMcpForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")}, f)
	_ = ptr(mm.(Model))
	if got, want := f.formVal(), "/opt/homebrew/binX/bun"; got != want {
		t.Errorf("typed at the end, not at the caret: %q, want %q", got, want)
	}
	// And the drawn caret is at the model position.
	line := stripANSI(m.renderMcpForm(f))
	if !strings.Contains(line, "/opt/homebrew/binX▌/bun") {
		t.Errorf("caret not drawn after the inserted rune:\n%s", line)
	}
}

// A value longer than the row is shown as a window that follows the
// caret: MCP args/env fields routinely exceed a pane, and a hard cut
// left the tail uneditable.
func TestMcpCaretWindowFollowsTheCaret(t *testing.T) {
	const room = 20
	long := strings.Repeat("abcdefghij", 6) // 60 cells, three times the room

	// Caret at the start: the head is visible, the tail is not.
	before, after := mcpCaretWindow(long, 0, room)
	if strings.TrimSpace(before) != "" {
		t.Errorf("at position 0 the text is all to the right, got %q", before)
	}
	if !strings.HasPrefix(after, "abcdefghij") {
		t.Errorf("the head must be visible: %q", after)
	}

	// Caret in the middle: the window scrolls, eliding the left edge.
	before, after = mcpCaretWindow(long, 30, room)
	if !strings.HasPrefix(before, "…") {
		t.Errorf("a scrolled window must elide its left edge: %q", before)
	}
	// The right side shows the text at the caret (elided at the far end),
	// and is not swallowed whole by the left half.
	if !strings.HasPrefix(after, long[30:34]) {
		t.Errorf("the text at the caret is not visible: %q", after)
	}
	if !strings.Contains(before, long[26:30]) {
		t.Errorf("the window lost the text just before the caret: %q", before)
	}

	// The two halves always add up to the room: the row cannot grow.
	for _, c := range []int{0, 1, 19, 30, 59, 60} {
		before, after = mcpCaretWindow(long, c, room)
		if got := lipgloss.Width(before) + lipgloss.Width(after); got != room-1 {
			t.Errorf("caret %d: halves are %d wide, want %d", c, got, room-1)
		}
	}
	// A degenerate room must not panic or go negative.
	if before, after = mcpCaretWindow(long, 5, 0); before != "" || after != "" {
		t.Errorf("no room, no row: %q %q", before, after)
	}
	// Short values are untouched apart from padding.
	before, after = mcpCaretWindow("abc", 1, room)
	if before != "a" || strings.TrimRight(after, " ") != "bc" {
		t.Errorf("split = %q %q, want %q %q", before, after, "a", "bc")
	}
}

// The rendered prompt must not outlive the window the gate enforces: a
// prompt that says "press Enter again" after the arm expired promises a
// second Enter that re-arms instead of deleting.
func TestHubArmedRemovePromptExpiresWithTheGate(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	rm := -1
	for i, p := range d.McpActPayload {
		if strings.Contains(p, "remove@") {
			rm = i
		}
	}
	if rm < 0 {
		t.Fatalf("no remove row: %v", d.McpActPayload)
	}
	d.McpActFocus, d.McpActCursor = true, rm
	if m.ArmMcpRemove("vault-tools") {
		t.Fatal("first press must only arm")
	}
	if !m.McpRemoveArmed("vault-tools") {
		t.Fatal("the gate is armed")
	}
	if pane := stripANSI(strings.Join(m.mcpHubPane(d, 120), "\n")); !strings.Contains(pane, "press Enter again") {
		t.Errorf("armed, but no prompt on the row:\n%s", pane)
	}
	// Age the arm past the window: the gate re-arms, so the prompt must go.
	m.mcpArmAt = time.Now().Add(-2 * mcpArmWindow)
	if m.McpRemoveArmed("vault-tools") {
		t.Fatal("the window has expired")
	}
	if pane := stripANSI(strings.Join(m.mcpHubPane(d, 120), "\n")); strings.Contains(pane, "press Enter again") {
		t.Errorf("an expired arm still promises a second Enter:\n%s", pane)
	}
	if m.ArmMcpRemove("vault-tools") {
		t.Fatal("an expired arm must re-arm, not confirm")
	}
}

// Every field opens with the caret at the END of its value, including
// fields the user has not typed in: a caret at Home made backspace a
// no-op and prepended what was typed, which silently corrupted an args
// or env string that still saved.
func TestMcpEditUnvisitedFieldsOpenAtTheEnd(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")

	// Touch the Command field first: that is what populates the caret
	// slice for every other field.
	d.McpEdit.Focus = indexOfFieldName(mcpFields, "Command")
	d.McpEditInsert("X")
	d.McpEdit.Focus = indexOfFieldName(mcpFields, "Args")
	args := d.McpEditVal()
	if got := d.McpEditCur(); got != len([]rune(args)) {
		t.Fatalf("Args caret = %d, want %d (end of %q)", got, len([]rune(args)), args)
	}
	// Backspace trims rather than doing nothing.
	mm, _ := m.updateMcpEditHub(tea.KeyMsg{Type: tea.KeyBackspace}, d)
	_ = ptr(mm.(Model))
	if got, want := d.McpEditVal(), args[:len(args)-1]; got != want {
		t.Errorf("backspace on an unvisited field = %q, want %q", got, want)
	}
	// And typing appends, never prepends.
	d.McpEditInsert("Z")
	if got, want := d.McpEditVal(), args[:len(args)-1]+"Z"; got != want {
		t.Errorf("typed rune = %q, want %q (appended at the end, not the start)", got, want)
	}
}

// A server name may contain "@" (a scoped id). The payload parser and
// the runner in src/builtin must agree on where the name starts, or the
// action runs against a server that does not exist.
func TestMcpPayloadTargetKeepsAnAtInTheName(t *testing.T) {
	for _, name := range []string{"vault-tools", "foo@bar", "@scope/pkg", "a@b@c"} {
		payload := psecActMCPAct + "remove@" + name
		if got := mcpTargetOf(payload); got != name {
			t.Errorf("mcpTargetOf(%q) = %q, want %q", payload, got, name)
		}
	}
	// The selection rows carry no separator, so they must not be cut.
	if got := mcpTargetOf(psecActMCPSel + "foo@bar"); got != "foo@bar" {
		t.Errorf("selection target = %q, want %q", got, "foo@bar")
	}
}

// The inline editor's Esc-discard prompt must not outlive the window the
// gate enforces. It used to be stored in e.Err and rendered
// unconditionally, so after the arm expired the pane still promised a
// second Esc that would only re-arm — and the press the user believed
// discarded their edit did not. Asserted on rendered output and on what a
// second Esc actually does, never on the predicate, so the test also fails
// against the pre-fix code rather than merely failing to build.
func TestHubEscDiscardPromptExpiresWithTheGate(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")

	// Dirty the form, so the Esc gate is live at all.
	d.McpEdit.Focus = indexOfFieldName(mcpFields, "Args")
	d.McpEditInsert("--new")
	open := len(m.Dialogs)
	const prompt = "press Esc again to discard"

	pressEsc := func() *Model {
		mm, _ := m.updateMcpEditHub(tea.KeyMsg{Type: tea.KeyEsc}, d)
		return ptr(mm.(Model))
	}
	pane := func() string {
		return stripANSI(strings.Join(mcpEditFormLines(d, 120), "\n"))
	}

	m = pressEsc()
	if !strings.Contains(pane(), prompt) {
		t.Fatalf("the first Esc must arm and say so:\n%s", pane())
	}
	// A second Esc inside the window really does discard: the editor closes
	// and the hub stays open under it.
	m = pressEsc()
	if d.McpEdit.Active {
		t.Fatal("a second Esc inside the window must discard the edit")
	}
	if len(m.Dialogs) != open {
		t.Fatalf("discard must leave the hub open, %d dialogs", len(m.Dialogs))
	}

	// Arm again, then age the arm past the window: the gate re-arms, so
	// the prompt must not still be on screen.
	m.OpenMcpEditHub("vault-tools")
	d = m.Dialogs[0]
	d.McpEdit.Focus = indexOfFieldName(mcpFields, "Args")
	d.McpEditInsert("--new")
	open = len(m.Dialogs)
	m = pressEsc()
	if !strings.Contains(pane(), prompt) {
		t.Fatalf("armed again:\n%s", pane())
	}
	d.McpEdit.ArmedAt = time.Now().Add(-2 * mcpFormDiscardWindow)
	if strings.Contains(pane(), prompt) {
		t.Errorf("an expired arm still promises a second Esc:\n%s", pane())
	}
	m = pressEsc()
	if !d.McpEdit.Active {
		t.Fatal("an expired arm discarded the edit instead of re-arming")
	}
	if len(m.Dialogs) != open {
		t.Fatalf("re-arming must leave the hub open, %d dialogs", len(m.Dialogs))
	}
	if !strings.Contains(pane(), prompt) {
		t.Errorf("the re-armed gate says nothing:\n%s", pane())
	}

	// Typing un-arms the gate, so the prompt goes with it.
	d.McpEditInsert("x")
	if strings.Contains(pane(), prompt) {
		t.Errorf("prompt outlived a disarm by typing:\n%s", pane())
	}
}

// The overlay form's Esc prompt must expire with the gate that wrote it.
// It used to live in m.Status, which never expires: the status bar still
// said "press Esc again" after the arm lapsed, and the press the user read
// as a discard only re-armed the dialog.
func TestMcpFormDiscardPromptExpiresWithTheGate(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	openForm(m, "")
	d := m.Dialogs[0]
	open := len(m.Dialogs)

	form := func() string { return stripANSI(m.renderMcpForm(d)) }
	esc := tea.KeyMsg{Type: tea.KeyEsc}

	mm, _ := m.updateMcpForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}, d)
	m = ptr(mm.(Model))
	if mcpFormDirty(d) != true {
		t.Fatal("setup: the form should be dirty")
	}
	mm, _ = m.updateMcpForm(esc, d)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != open {
		t.Fatalf("the first Esc must only arm, %d dialogs left", len(m.Dialogs))
	}
	if !strings.Contains(form(), "press Esc again") {
		t.Fatalf("armed, but no prompt in the form:\n%s", form())
	}

	// Age the arm past the window: the gate re-arms, so the prompt must go.
	d.FormArmedAt = time.Now().Add(-2 * mcpFormDiscardWindow)
	if strings.Contains(form(), "press Esc again") {
		t.Errorf("an expired arm still promises a second Esc:\n%s", form())
	}
	if strings.Contains(m.Status, "press Esc again") {
		t.Errorf("the prompt is parked in m.Status, which never expires: %q", m.Status)
	}
	mm, _ = m.updateMcpForm(esc, d)
	m = ptr(mm.(Model))
	if len(m.Dialogs) != open {
		t.Fatal("an expired arm must re-arm, not discard")
	}
	if !strings.Contains(form(), "press Esc again") {
		t.Errorf("the re-armed gate says nothing:\n%s", form())
	}

	// Typing un-arms it, so the prompt goes with it.
	mm, _ = m.updateMcpForm(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")}, d)
	m = ptr(mm.(Model))
	if strings.Contains(form(), "press Esc again") {
		t.Errorf("prompt outlived a disarm by typing:\n%s", form())
	}

	// A save error is sticky — only a successful save clears it — so it is
	// on screen for the rest of the form's life. The gate prompt must
	// still appear alongside it: an armed gate with nothing to show for it
	// looks like a frozen form, and the second Esc discards the edit.
	d.FormErr = "a server named gh already exists"
	mm, _ = m.updateMcpForm(esc, d)
	m = ptr(mm.(Model))
	f := form()
	if !strings.Contains(f, "already exists") {
		t.Errorf("the save error vanished:\n%s", f)
	}
	if !strings.Contains(f, "press Esc again") {
		t.Errorf("an armed gate with no visible prompt — the form looks frozen:\n%s", f)
	}
}

// Below mcpHubDetailW the hub's MCP section has no third column, so the
// middle pane must keep the desc text — and, crucially, the actions column
// must be neither enterable nor runnable. It used to be reachable anyway:
// the desc was suppressed unconditionally, so at 100 columns you saw a bare
// server name and could still -> ↓ Enter a state-changing action on it.
func TestHubMcpActionsAreUnreachableWhenTheColumnIsNotDrawn(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t) // winW 140: the column IS drawn here
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true, Scope: "user", Exposure: "tools"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]

	// Narrow: hubBoxW(100) == 90 < 110.
	m.winW = 100
	m.loadRows(d) // the row build caches the header hint, so rebuild it
	if m.mcpActColumnDrawn(d) {
		t.Fatalf("winW=100 should not draw the actions column")
	}

	// The row still says what it is, and the header no longer promises a
	// column that is not there. The whole hub is rendered, not just the
	// detail pane — this is about the row the user is looking at.
	pane := stripANSI(m.renderPconfigDialog(d))
	if !strings.Contains(pane, "vault-tools") {
		t.Fatalf("the server row vanished:\n%s", pane)
	}
	if !strings.Contains(pane, "connected") {
		t.Errorf("narrow, the row lost its state · exposure · scope:\n%s", pane)
	}
	if strings.Contains(pane, "→ its actions") {
		t.Errorf("the header advertises a column that is not drawn:\n%s", pane)
	}

	// -> must not take focus on it.
	mm, _ := m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyRight}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus {
		t.Fatal("-> focused an undrawn column")
	}
	// Tab likewise.
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyTab}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus {
		t.Fatal("Tab focused an undrawn column")
	}
	// Enter must not run one either.
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyEnter}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus || d.McpActRun {
		t.Fatal("Enter ran an action on an undrawn column")
	}

	// Same keys, same model, wide: the column is drawn and reachable, or
	// the guards above would be passing for the wrong reason.
	m.winW = 140
	if !m.mcpActColumnDrawn(d) {
		t.Fatal("winW=140 should draw the actions column")
	}
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyRight}, d)
	m = ptr(mm.(Model))
	if !d.McpActFocus {
		t.Fatal("-> should focus the drawn column")
	}
}

// A terminal narrowed while the actions column has focus must not leave
// Enter able to run an action from a column that is no longer on screen.
func TestHubMcpFocusIsDroppedWhenTheColumnStopsBeingDrawn(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true, Scope: "user", Exposure: "tools"},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]

	mm, _ := m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyRight}, d)
	m = ptr(mm.(Model))
	if !d.McpActFocus {
		t.Fatal("setup: -> should focus the column at 140")
	}

	// The window shrinks under it.
	m.winW = 100
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyTab}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus {
		t.Fatal("Tab must drop a focus the terminal can no longer show")
	}
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyEnter}, d)
	m = ptr(mm.(Model))
	if d.McpActRun {
		t.Fatal("Enter must not run an action on a column that is gone")
	}

	// And from a clean narrow state, Tab must not focus it either: it
	// toggles, so without the guard the second Tab would put the focus on
	// a column that is not there.
	d.McpActFocus = false
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyTab}, d)
	m = ptr(mm.(Model))
	if d.McpActFocus {
		t.Fatal("Tab focused a column that is not drawn")
	}
}

// Every line of a rendered dialog must be the same width. A row that grows
// a phantom line — or one whose right border sits a cell or two inside the
// others — wraps inside the box and shifts the frame. This is the check
// the whole suite was missing: the desc case once fired on a wide Plugins
// row with a -3 budget, so every plugin rendered "— …" and the borders
// went ragged, with every existing test still green.
func TestHubRowsNeverWrapInsideTheBox(t *testing.T) {
	mcpPanelEnv(t)
	for _, tc := range []struct {
		name    string
		sec     string
		winW    int
		wantRow string
	}{
		{"plugins wide", PsecPlugin, 140, "pi-fake-noexist"},
		{"mcp wide", PsecMCP, 140, "vault-tools"},
		{"mcp narrow", PsecMCP, 100, "vault-tools"},
		{"plugins narrow", PsecPlugin, 100, "pi-fake-noexist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := mcpPanelModel(t)
			m.winW = tc.winW
			m.Plugins = []Plugin{{Spec: "npm:pi-fake-noexist", Name: "pi-fake-noexist"}}
			m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
				{Name: "vault-tools", State: "connected", Enabled: true, Scope: "user", Exposure: "tools"},
			}})
			m.OpenHubSection(tc.sec)
			d := m.Dialogs[0]
			m.loadRows(d)
			m.syncMcpActDrawn(d)

			out := stripANSI(m.renderPconfigDialog(d))
			if !strings.Contains(out, tc.wantRow) {
				t.Fatalf("%q is missing from the render:\n%s", tc.wantRow, out)
			}
			// A phantom "— …" on a row with no room for it is the symptom.
			if strings.Contains(out, "— …") {
				t.Errorf("a row rendered a phantom \"— …\" desc:\n%s", out)
			}
			// Every line the box draws must be exactly as wide as its
			// peers, or a row has wrapped and the right border sits inside
			// the others. The rounded corners are legitimately narrower, so
			// the comparison is over the body lines only.
			ref := -1
			for i, ln := range strings.Split(out, "\n") {
				if !strings.Contains(ln, "│") {
					continue
				}
				w := lipgloss.Width(ln)
				if ref == -1 {
					ref = w
					continue
				}
				if w != ref {
					t.Errorf("line %d is %d wide, %d for every other body line — a row wrapped:\n%q",
						i+1, w, ref, ln)
				}
			}
		})
	}
}

// A resize is the common way to lose the actions column, and it happens
// with no keypress: WindowSizeMsg updates winW and rebuilds nothing, so a
// hint cached in d.Message would keep promising a column that just went.
func TestHubMcpHintIsTrueAfterAResizeWithNoKeypress(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]

	wide := stripANSI(m.renderPconfigDialog(d))
	if !strings.Contains(wide, "its actions") {
		t.Fatalf("wide, the hint should mention the actions:\n%s", wide)
	}

	// The window shrinks. No key, no loadRows — just the resize.
	m.winW = 100
	narrow := stripANSI(m.renderPconfigDialog(d))
	if strings.Contains(narrow, "its actions") {
		t.Errorf("after a resize the hint still promises the column:\n%s", narrow)
	}
	if !strings.Contains(narrow, "vault-tools") {
		t.Errorf("and the section vanished:\n%s", narrow)
	}
}

// A focus that outlives its column must be dropped wherever it comes from,
// not only by the key handlers that happen to notice. Cancelling the inline
// editor sets the focus unconditionally, so at a narrow width it used to
// leave the hub parked on a column that was never on screen: ↑↓ moved an
// invisible cursor and ← was swallowed.
func TestHubStaleActionFocusIsDroppedOnRender(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.OpenMcpEditHub("vault-tools")

	m.winW = 100 // the editor's cancel will set the focus unconditionally
	mm, _ := m.cancelMcpEditHub(d)
	m = ptr(mm.(Model))
	if !d.McpActFocus {
		t.Skip("cancel no longer parks the focus on the actions column")
	}
	// Rendering is enough to notice: no keypress required.
	_ = m.renderPconfigDialog(d)
	if d.McpActFocus {
		t.Fatal("a focus on an undrawn column survived the render")
	}
	// And the keys that were being swallowed work again.
	mm, _ = m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyLeft}, d)
	m = ptr(mm.(Model))
	if m.Status != "" {
		t.Logf("status after ←: %q", m.Status)
	}
}

// The preconditions above are asserted against the production predicate,
// which would follow a moved threshold silently. Pin the widths instead.
func TestHubMcpDetailThresholdIs110(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]

	for _, tc := range []struct {
		winW  int
		drawn bool
	}{{119, false}, {120, true}} {
		m.winW = tc.winW
		if got := m.mcpActColumnDrawn(d); got != tc.drawn {
			t.Errorf("winW=%d: mcpActColumnDrawn = %v, want %v (hubBoxW=%d)",
				tc.winW, got, tc.drawn, hubBoxW(m))
		}
	}
}

// Defence in depth, pinned: even with a stale McpActRun, an undrawn column
// yields no payload. The key handlers already refuse, but this is the only
// function that can return one, so it asks too.
func TestMcpHubActionRefusesAnUndrawnColumn(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	m.loadRows(d)

	m.winW = 100
	m.syncMcpActDrawn(d)
	if d.McpActDrawn {
		t.Fatal("setup: at 100 the column is not drawn")
	}
	// A stale run flag, as if one had survived a resize.
	d.McpActRun, d.McpActCursor, d.McpActFocus = true, 0, true
	if payload, ok := McpHubAction(d); ok {
		t.Fatalf("an undrawn column returned a payload: %q", payload)
	}

	// And the same state with the column drawn does return one.
	m.winW = 140
	m.syncMcpActDrawn(d)
	d.McpActRun = true
	if _, ok := McpHubAction(d); !ok {
		t.Fatal("a drawn column refused its action — the guard is too broad")
	}
}

// A project-scoped server's entry lives in the project's mcp.json, not in
// the agent dir's. The hub used to read only the agent dir, so every such
// server missed the cache and the DETAILS column said "not in mcp.json" —
// no command, no args, no env, nothing to edit, on a server that plainly
// exists.
func TestHubDetShowsProjectScopedEntries(t *testing.T) {
	mcpPanelEnv(t)
	dir := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", dir)
	proj := t.TempDir()
	projFile := filepath.Join(proj, ".pi", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(projFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projFile,
		[]byte(`{"mcpServers":{"proj-one":{"command":"proj-bin","args":["--stdio"],"env":{"K":"V"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	m := &Model{MCP: []McpServer{{Name: "proj-one"}}}
	m.winW, m.winH = 140, 40
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "proj-one", Scope: "project", Source: projFile, Enabled: true},
	}})
	m.refreshMcpHubDefs()

	d, ok := m.mcpHubDef("proj-one")
	if !ok {
		t.Fatal("a project-scoped server missed the cache: its entry is in the project's mcp.json")
	}
	if d.Command != "proj-bin" {
		t.Errorf("command = %q, want %q", d.Command, "proj-bin")
	}

	// And the column that renders it must actually show the entry.
	m.OpenHubSection(PsecMCP)
	d2 := m.Dialogs[0]
	m.syncMcpActDrawn(d2)
	out := stripANSI(strings.Join(m.mcpHubPane(d2, 60), "\n"))
	if strings.Contains(out, "not in") {
		t.Errorf("the details column says the entry is missing:\n%s", out)
	}
	if !strings.Contains(out, "proj-bin") {
		t.Errorf("the command is not on screen:\n%s", out)
	}
}

// Typing in the hub's MCP section must narrow the server list. It used to
// call applyMcpFilter and then loadRows, and the hub's row builder never
// read the filter — so every rebuild after a keystroke restored the full
// list and the filter box looked live while nothing filtered.
func TestHubMcpFilterNarrowsTheServerList(t *testing.T) {
	mcpPanelEnv(t)
	m := mcpPanelModel(t)
	m.SetMcpInfo(McpMsg{Servers: []pirpc.McpServerInfo{
		{Name: "vault-tools", State: "connected", Enabled: true},
		{Name: "slack", State: "connected", Enabled: true},
		{Name: "notion", State: "connected", Enabled: true},
	}})
	m.OpenHubSection(PsecMCP)
	d := m.Dialogs[0]
	typeIn := func(s string) {
		for _, r := range s {
			mm, _ := m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}, d)
			m = ptr(mm.(Model))
		}
	}
	typeIn("sl")
	if d.Filter != "sl" {
		t.Fatalf("filter = %q", d.Filter)
	}
	if strings.Contains(strings.Join(d.Options, ","), "notion") ||
		strings.Contains(strings.Join(d.Options, ","), "vault-tools") {
		t.Errorf("the filter did not narrow the list: %v", d.Options)
	}
	if len(d.Options) != 1 || d.Options[0] != "slack" {
		t.Fatalf("rows = %v, want just [slack]", d.Options)
	}
	// A filter that matches nothing says so rather than showing everything.
	typeIn("zz")
	if !strings.Contains(strings.Join(d.Options, " "), "no server matches") {
		t.Errorf("an empty match should say so: %v", d.Options)
	}
	// Backspace widens it again: "slzz" -> "slz" -> "sl" matches slack.
	for _, want := range []string{"slz", "sl"} {
		mm, _ := m.updatePconfigDialogKey(tea.KeyMsg{Type: tea.KeyBackspace}, d)
		m = ptr(mm.(Model))
		if d.Filter != want {
			t.Fatalf("after backspace filter = %q, want %q", d.Filter, want)
		}
	}
	if len(d.Options) != 1 || d.Options[0] != "slack" {
		t.Errorf("backspace did not widen the list back to slack: %v", d.Options)
	}
}
