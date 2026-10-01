package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/pirpc"
)

// MCP manager (issue #7): the settings hub's MCP section as a real panel.
//
// Layout reuses the hub's two-pane dialog: left = configured servers,
// middle = actions for the highlighted server, right = that server's
// properties (the DETAILS column, wide terminals only). Every write goes
// through pirpc.McpDoc, which preserves mcp.json's key order and keeps a
// .bak copy, then tells the user to reload pi — the Claude Code pattern:
// the config file is the source of truth, the process is restarted, no
// hidden in-memory server registry.

const (
	mcpKind     = "mcpedit"     // the two-pane config editor ("mcp" is pi's manager)
	mcpFormKind = "mcpeditForm" // add/edit form (own kind: every key is text)
)

// mcpArmWindow bounds the remove-confirmation gate (same shape as the
// plugin install/remove gate in pmarket.go).
const mcpArmWindow = 10 * time.Second

// mcpFields are the form rows, in tab order. Values are parsed by
// mcpDefFromForm. Args/Env/Headers are "keep unless touched" (the form
// tracks which rows the user actually edited, so a blank row never
// silently means "delete what is there").
var mcpFields = []string{"Name", "Transport", "Command", "URL", "Args", "Env", "Headers", "Enabled"}

// mcpFormVals is a form's field values, keyed by field name. It exists so
// the same field set, validation and save path back BOTH the overlay form
// and the hub's pane 3 — one place to change a field, one rule for
// "touched".
type mcpFormVals map[string]string

// set records a value and marks the field touched under the same rule the
// overlay uses: blanking a field that had content is a change, and so is
// changing real text to real text, but a stray space in a field that
// prefilled blank is neither.
func (f mcpFormVals) set(field, v string, pristine string, touched map[string]bool) {
	f[field] = v
	if touched == nil {
		return
	}
	if touched[field] {
		return // sticky: a later space must not un-clear it
	}
	touched[field] = (v == "" && pristine != "") || (v != pristine && strings.TrimSpace(v) != "")
}

// touched reports whether a field was really edited.
func (f mcpFormVals) touched(field string, touched map[string]bool) bool {
	return touched != nil && touched[field]
}

// mcpDocPath picks the file pitago writes: mcp.json when it exists, else
// .mcp.json when only that exists, else mcp.json (created on first save).
// Same precedence the sidebar snapshot reader uses.
func mcpDocPath(dir string) string {
	paths := pirpc.McpConfigPaths(dir)
	if len(paths) == 0 {
		return ""
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return paths[0]
}

// mcpHasServer reports whether the doc already has an entry under name —
// key presence, not field presence, so a placeholder entry counts.
func mcpHasServer(doc *pirpc.McpDoc, name string) bool {
	for _, n := range doc.Servers() {
		if n == name {
			return true
		}
	}
	return false
}

// mcpTargetPath is the file an action on this panel must write: the one
// the panel is showing, so a form edit and a toggle on the same server can
// never land in two different files.
func mcpTargetPath(d *Dialog) string {
	if d != nil && d.McpPath != "" {
		return d.McpPath
	}
	return mcpDocPath(piAgentDir())
}

// McpEditMode is the hub's inline entry editor: pane 3 turns into the
// same form the overlay shows, so editing a server never leaves settings
// for a different UI. The overlay editor (Kind mcpedit) still exists for
// the standalone panel; this is the one the hub reaches.
type McpEditMode struct {
	Active   bool
	Orig     string   // the entry being edited ("" = add)
	Path     string   // the file it came from
	Focus    int      // focused field (index into McpFields)
	Fields   []string // field names, parallel to Vals
	Vals     []string
	Pristine []string
	Touched  map[string]bool
	Err      string // validation/save error, shown in pane 3
	Armed    string // field armed for the "type twice to discard" gate
	ArmedAt  time.Time
	// Cur is the caret position per field, counted in RUNES (not bytes:
	// a byte index slices a multi-byte character in half and the field
	// turns to mojibake). A missing or short entry means "end of value" —
	// which is where a freshly prefilled field wants it.
	Cur []int
}

// mcpEditRunes is the focused field as runes, the unit every caret
// operation counts in.
func (d *Dialog) mcpEditRunes() []rune {
	return []rune(d.McpEditVal())
}

// McpEditCur is the caret index in the focused field, clamped to the
// value. Exported: the renderer draws the caret, the key handler moves it.
func (d *Dialog) McpEditCur() int {
	n := len(d.mcpEditRunes())
	if d.McpEdit.Focus < 0 || d.McpEdit.Focus >= len(d.McpEdit.Cur) {
		return n
	}
	c := d.McpEdit.Cur[d.McpEdit.Focus]
	if c < 0 {
		return n // never parked: open at the end, not at Home. Returning
		// 0 here put the caret at the start of every field the user had
		// not typed in, so backspace did nothing and typing prepended —
		// silently corrupting an args or env string that still saved.
	}
	if c > n {
		return n
	}
	return c
}

// McpEditSetCur parks the caret in the focused field, clamped.
func (d *Dialog) McpEditSetCur(c int) {
	e := &d.McpEdit
	if e.Focus < 0 {
		return
	}
	// -1 is "never parked", which reads as "end of value" — a plain 0
	// would mean Home for every field the user has not typed in.
	for len(e.Cur) < len(e.Fields) {
		e.Cur = append(e.Cur, -1)
	}
	n := len(d.mcpEditRunes())
	if c < 0 {
		c = 0
	}
	if c > n {
		c = n
	}
	e.Cur[e.Focus] = c
}

// McpEditCaret moves the caret by delta, which may be negative. It does
// not touch the value: ← and → are a position, not an edit, so they
// never dirty the form and never arm the Esc gate.
func (d *Dialog) McpEditCaret(delta int) {
	d.McpEditSetCur(d.McpEditCur() + delta)
}

// McpEditInsert types `s` at the caret rather than at the end — the
// difference between "append to the field" and "type where I am looking".
func (d *Dialog) McpEditInsert(s string) {
	if s == "" {
		return
	}
	r := d.mcpEditRunes()
	c := d.McpEditCur()
	ins := []rune(s)
	out := make([]rune, 0, len(r)+len(ins))
	out = append(out, r[:c]...)
	out = append(out, ins...)
	out = append(out, r[c:]...)
	d.McpEditSet(string(out))
	d.McpEditSetCur(c + len(ins))
}

// McpEditDelete removes one rune: before the caret normally, at the
// caret with a Delete keypress.
func (d *Dialog) McpEditDelete(atCaret bool) {
	r := d.mcpEditRunes()
	c := d.McpEditCur()
	i := c
	if !atCaret {
		i--
	}
	if i < 0 || i >= len(r) {
		return
	}
	d.McpEditSet(string(append(append([]rune{}, r[:i]...), r[i+1:]...)))
	d.McpEditSetCur(i)
}

// McpEdit is the hub's inline editor state. Exported field on Dialog so the
// key handler and the renderer share it.
func (d *Dialog) McpEditVal() string {
	if d.McpEdit.Focus < 0 || d.McpEdit.Focus >= len(d.McpEdit.Vals) {
		return ""
	}
	return d.McpEdit.Vals[d.McpEdit.Focus]
}

func (d *Dialog) McpEditSet(v string) {
	e := &d.McpEdit
	if e.Focus < 0 || e.Focus >= len(e.Vals) {
		return
	}
	e.Vals[e.Focus] = v
	if e.Touched == nil {
		e.Touched = map[string]bool{}
	}
	d.McpEditSetCur(len([]rune(v))) // a wholesale set parks the caret at the end
	pristine := ""
	if e.Focus < len(e.Pristine) {
		pristine = e.Pristine[e.Focus]
	}
	now := strings.TrimSpace(v)
	if !e.Touched[e.Fields[e.Focus]] {
		e.Touched[e.Fields[e.Focus]] = (v == "" && pristine != "") ||
			(v != pristine && now != "")
	}
	e.Armed, e.ArmedAt = "", time.Time{} // typing un-arms the discard gate
}

// mcpDiscardArmed reports whether the inline hub editor's Esc gate is
// waiting on a second Esc. Recomputed at render time, like
// Model.McpRemoveArmed for the remove gate: a prompt stored as text goes
// stale the moment the window lapses, so after it did the pane still
// promised a second Esc that would only re-arm, and the edit survived the
// press the user believed discarded it.
func (d *Dialog) mcpDiscardArmed() bool {
	e := d.McpEdit
	return e.Armed == "discard" && !e.ArmedAt.IsZero() &&
		time.Since(e.ArmedAt) < mcpFormDiscardWindow
}

// mcpFormDiscardArmed reports whether the overlay form's Esc gate is waiting
// on a second Esc. Recomputed at render time, like Dialog.mcpDiscardArmed
// for the inline hub editor: the prompt used to be written into m.Status at
// arm time and never expired, so the status bar still said "press Esc again"
// long after the gate had re-armed, and the press the user read as a discard
// only re-armed the dialog.
func (d *Dialog) mcpFormDiscardArmed() bool {
	return d.FormArmed == "esc" && !d.FormArmedAt.IsZero() &&
		time.Since(d.FormArmedAt) < mcpFormDiscardWindow
}

// dirty reports whether the form differs from its prefill, which is what
// the Esc gate needs.
func (d *Dialog) mcpEditDirty() bool {
	if d.McpEdit.Active {
		for i, v := range d.McpEdit.Vals {
			if i < len(d.McpEdit.Pristine) && v != d.McpEdit.Pristine[i] {
				return true
			}
		}
		return len(d.McpEdit.Touched) > 0
	}
	return mcpFormDirty(d)
}

// OpenMcpEditHub switches the hub's pane 3 into the entry editor for a
// server. It pre-fills from the file (not pi), keeps the path pi reported,
// and leaves pane 2 alone: editing happens where you can see it.
func (m *Model) OpenMcpEditHub(name string) {
	path := ""
	if srv, ok := m.McpInfoFor(name); ok && srv.Source != "" {
		path = srv.Source
	}
	if path == "" {
		path = mcpDocPath(piAgentDir())
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		m.Status = "MCP: " + err.Error()
		m.Refresh()
		return
	}
	var def pirpc.McpDef
	if name != "" {
		def = doc.Get(name)
	}
	vals := mcpFormPrefill(def, name)
	e := McpEditMode{
		Active: true, Orig: name, Path: path,
		Fields: append([]string(nil), mcpFields...),
		Vals:   vals,
	}
	e.Pristine = append([]string(nil), vals...)
	e.Touched = map[string]bool{}
	for _, d := range m.Dialogs {
		if d.Kind == "pconfig" {
			d.McpEdit = e
			d.McpActFocus = false // the actions give way to the form
			d.McpEdit.Focus = indexOfFieldName(mcpFields, "Command")
			if name == "" {
				d.McpEdit.Focus = 0
			}
		}
	}
	m.Status = "editing " + name + " in " + mcpBaseName(path) + " — ↑↓ field · Ctrl+S save · Esc cancel"
	m.Refresh()
}

// mcpFormPrefil is the field values for an entry, in mcpFields order.
func mcpFormPrefill(def pirpc.McpDef, name string) []string {
	transport := def.Type
	if transport == "" {
		transport = "stdio"
	}
	if name == "" && def.Command == "" && def.URL == "" {
		transport = "stdio"
	}
	yesNo := "yes"
	if def.Disabled {
		yesNo = "no"
	}
	fields := []string{
		name,
		transport,
		def.Command,
		def.URL,
		mcpJoinArgs(def.Args),
		mcpEnvJSON(def.Env),
		mcpEnvJSON(def.Headers),
		yesNo,
	}
	if len(fields) < len(mcpFields) {
		fields = append(fields, make([]string, len(mcpFields)-len(fields))...)
	}
	return fields
}

func indexOfFieldName(fields []string, name string) int {
	for i, f := range fields {
		if f == name {
			return i
		}
	}
	return 0
}

// invalidateMcpInfo marks pi's list stale after a write: the file on disk
// changed, so what pi reports about it (state, tools, whether it is even
// valid) is out of date. The next keypress on the MCP section re-lists —
// there is no `pi mcp reconnect`, listing is what connects — and the
// DETAILS column re-reads the file immediately.
func (m *Model) invalidateMcpInfo() {
	m.McpInfoAt = time.Time{}
	m.refreshMcpHubDefs()
}

// mcpTargetPathString is mcpTargetPath for callers holding a path rather
// than a dialog. The path is passed in, not resolved again, because a
// fresh resolve would move the write to a different file than the one on
// screen (the panel pins its file for exactly this reason).
func mcpTargetPathString(path string) string {
	if path == "" {
		return mcpTargetPath(nil)
	}
	return path
}

// saveMcpDoc writes doc under a compare-and-swap guard on the bytes the
// doc was parsed from, snapshotting them as .bak on the way.
//
// Two different races are covered, by two different mechanisms:
//   - The panel can sit open for minutes, and the user may edit the file
//     in another window. Every write path re-reads the file immediately
//     before mutating (LoadMcpConfig → Put → saveMcpDoc), so a mid-session
//     hand edit is merged, not renamed over — that is the re-read, not
//     this guard.
//   - Between that read and the rename, another writer could land. The CAS
//     closes that microsecond window and nothing is written on a mismatch.
//
// The backup is taken only after the comparison passes, so a refused save
// cannot overwrite a good .bak with a stale snapshot. A failed backup
// aborts the write: overwriting the only copy of a hand-written env token
// map to save a convenience is the wrong trade.
func saveMcpDoc(doc *pirpc.McpDoc) error {
	if doc.Path == "" {
		return fmt.Errorf("no MCP config path resolved")
	}
	prev := doc.Read() // exact content this doc was parsed from
	cur, err := os.ReadFile(doc.Path)
	if err != nil && !os.IsNotExist(err) {
		return err // unreadable file: refuse to clobber it
	}
	if !bytes.Equal(cur, prev) {
		return fmt.Errorf("%s changed on disk — reopen the panel and retry", mcpBaseName(doc.Path))
	}
	if len(prev) > 0 {
		if err := pirpc.BackupFile(doc.Path, prev); err != nil {
			return fmt.Errorf("backup failed, nothing written: %w", err)
		}
	}
	return doc.SaveIfUnchanged(doc.Path, prev)
}

// OpenMcpPanel pushes the MCP manager on top of whatever is open.
func (m *Model) OpenMcpPanel() { m.OpenMcpPanelOn("") }

// OpenMcpPanelOn pushes the manager with `name` already highlighted, so
// Enter on a server row in the settings hub lands on that server rather
// than the first one ("" = none in particular).
func (m *Model) OpenMcpPanelOn(name string) {
	m.clearMcpArm() // a fresh editor, a fresh gate
	d := &Dialog{Kind: mcpKind, Title: "MCP servers",
		PsecIDs:   []string{PsecMCP},
		LeftHead:  "SERVERS",
		RightHead: "ACTIONS",
		ProvFocus: true}
	m.LoadMcpRows(d)
	for i, n := range d.McpNames {
		if n == name {
			d.ProvCursor = i
		}
	}
	if d.ProvCursor < len(d.McpNames) {
		m.LoadMcpRows(d) // status dots and actions depend on the selection
	}
	// Prepend: the panel is usually opened from the settings hub, and
	// Dialogs[0] is the dialog that takes keys (see updateDialog).
	m.Dialogs = append([]*Dialog{d}, m.Dialogs...)
	m.Refresh()
}

// McpSelected returns the server name under the left-pane cursor ("" when
// the list is empty).
func (d *Dialog) McpSelected() string {
	if d.ProvCursor < 0 || d.ProvCursor >= len(d.McpNames) {
		return ""
	}
	return d.McpNames[d.ProvCursor]
}

// McpDef returns the definition for the selected server.
func (d *Dialog) McpDef() pirpc.McpDef {
	if d.ProvCursor < 0 || d.ProvCursor >= len(d.McpDefs) {
		return pirpc.McpDef{}
	}
	return d.McpDefs[d.ProvCursor]
}

// mcpStatus returns the live snapshot for a server name.
func (m *Model) mcpStatus(name string) (McpServer, bool) {
	for _, s := range m.MCP {
		if s.Name == name {
			return s, true
		}
	}
	return McpServer{}, false
}

// mcpLabel prefixes a server name with its live status dot.
func (m *Model) mcpLabel(name string, def pirpc.McpDef) string {
	dot := "○" // configured, pi has not connected it
	if s, ok := m.mcpStatus(name); ok && s.Connected {
		dot = "●"
	}
	if def.Disabled {
		dot = "⊘" // disabled wins: pi will not connect it at all
	}
	return dot + " " + name
}

// LoadMcpRows rebuilds the panel: servers on the left, actions in the
// middle. It keeps both cursors — the panel is browsed with ↑↓ on the
// left, and every write rebuilds the rows.
//
// The config path is pinned on the first load: re-resolving per rebuild
// would let the panel silently hop to another file (mcp.json appearing
// mid-session) and swap the server list under the user.
func (m *Model) LoadMcpRows(d *Dialog) {
	path := d.McpPath
	if path == "" {
		path = mcpDocPath(piAgentDir())
	}
	d.McpPath = path // pinned even on failure: "Open mcp.json" must show this one
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		// Show the failure in place rather than pretending there are no
		// servers: an unreadable mcp.json is exactly when the user opened
		// this panel.
		d.Message = "cannot read mcp.json — " + err.Error()
		d.Provs = []string{"— unreadable —"}
		// Clear every source too: a stale McpAll with an emptied McpDefs
		// would panic the moment the user types (the filter indexes both).
		d.McpNames, d.McpDefs, d.McpAll, d.McpAllDefs = nil, nil, nil, nil
		m.clearMcpArm()
		d.Options = []string{"Open mcp.json"}
		d.Descs = []string{"reveal the config in Finder/editor"}
		d.Payload = []string{"mcp:open"}
		d.Reindex()
		return
	}
	d.Message = "↑↓ server · ←→ pane · Enter runs the highlighted action · type filters · Esc closes"
	m.clearMcpArm() // a rebuild invalidates any armed remove
	d.McpAll = doc.Servers()
	d.McpAllDefs = make([]pirpc.McpDef, len(d.McpAll))
	for i, n := range d.McpAll {
		d.McpAllDefs[i] = doc.Get(n)
	}
	m.applyMcpFilter(d)
}

// applyMcpFilter narrows the server list to the typed filter and rebuilds
// the rows. Separate from LoadMcpRows so typing does not re-read the file
// on every keystroke: finding a server by name is the panel's whole job,
// so the filter belongs to the server pane, not only to the actions.
//
// McpNames/McpDefs/Provs end up parallel to each other (the filtered view);
// McpAll/McpAllDefs stay parallel to the whole file.
func (m *Model) applyMcpFilter(d *Dialog) {
	f := strings.ToLower(strings.TrimSpace(d.Filter))
	// typing is navigation: an armed remove must not survive it, or a
	// later Enter deletes with no fresh confirmation.
	m.mcpArmName, m.mcpArmAt = "", time.Time{}
	d.McpNames = d.McpNames[:0]
	d.McpDefs = d.McpDefs[:0]
	d.Provs = d.Provs[:0]
	for i, n := range d.McpAll {
		if i >= len(d.McpAllDefs) {
			break // defensive: the two are assigned together
		}
		if f != "" && !strings.Contains(strings.ToLower(n), f) {
			continue
		}
		d.McpNames = append(d.McpNames, n)
		d.McpDefs = append(d.McpDefs, d.McpAllDefs[i])
		d.Provs = append(d.Provs, m.mcpLabel(n, d.McpAllDefs[i]))
	}
	if len(d.Provs) == 0 {
		d.Provs = append(d.Provs, "— no servers —")
	}
	if d.ProvCursor >= len(d.McpNames) {
		d.ProvCursor = 0
	}
	d.Options, d.Descs, d.Payload = mcpActionRows(d.McpSelected(), d.McpDef())
	d.Reindex()
}

// mcpActionRows builds the middle pane for the selected server. Property
// rows live in the detail column, so this pane holds actions only.
func mcpActionRows(sel string, def pirpc.McpDef) (opts, descs, payload []string) {
	add := func(o, dsc, p string) {
		opts = append(opts, o)
		descs = append(descs, dsc)
		payload = append(payload, p)
	}
	if sel == "" {
		add("Add server…", "create an mcpServers entry", "mcp:add")
		add("Reload pi MCPs", "restart pi so it re-reads mcp.json", "mcp:reload")
		add("Open mcp.json", "reveal the config in Finder/editor", "mcp:open")
		return opts, descs, payload
	}
	state := "enabled · Enter disables"
	if def.Disabled {
		state = "disabled · Enter enables"
	}
	// Add stays on the first row with servers present too: a manager
	// without a way to add is a viewer, not a manager.
	add("Add server…", "create an mcpServers entry", "mcp:add")
	add("Toggle enabled", state, "mcp:toggle")
	add("Edit server…", "command, args, env, transport", "mcp:edit")
	add("Remove server…", "delete the entry (mcp.json.bak kept)", "mcp:remove")
	add("Reload pi MCPs", "restart pi so it re-reads mcp.json", "mcp:reload")
	add("Open mcp.json", "reveal the config in Finder/editor", "mcp:open")
	return opts, descs, payload
}

// mcpDetailLines is the panel's DETAILS column: the highlighted server's
// definition as label/value rows. Same fixed-width contract as the plugin
// and marketplace detail builders.
func (m *Model) mcpDetailLines(d *Dialog, w int) []string {
	if w < 30 {
		w = 30
	}
	sel := d.McpSelected()
	if sel == "" {
		return []string{"  " + toolStyle.Width(w-2).Render("— no server selected —")}
	}
	def := d.McpDef()
	lines := []string{"  " + lipgloss.NewStyle().Bold(true).Foreground(cText).Render(Fit(sel, w-2))}

	status, style := "not connected", statusBarStyle
	if def.Disabled {
		status = "disabled"
	} else if s, ok := m.mcpStatus(sel); ok && s.Connected {
		status = fmt.Sprintf("connected · %d/%d direct · ~%s tok", s.Direct, s.Total, fmtComma(s.Tokens))
		style = okStyle
	}
	target, targetLabel := def.Command, "Command"
	if def.Transport() != "stdio" {
		target, targetLabel = def.URL, "URL"
	}
	rows := [][2]string{
		{"Status", status},
		{"Transport", def.Transport()},
		{targetLabel, target},
		{"Args", mcpJoinArgs(def.Args)},
		{"Env", mcpJoinMap(def.Env)},
		{"File", mcpBaseName(d.McpPath)},
	}
	const lw = 10
	valW := w - 2 - lw - 1
	if valW < 10 {
		valW = 10
	}
	for i, r := range rows {
		valStyle := lipgloss.NewStyle().Foreground(cText)
		switch {
		case i == 0:
			valStyle = style
		case r[1] == "" || r[1] == "—":
			valStyle = statusBarStyle
		}
		lines = append(lines, "  "+toolStyle.Render(Fit(r[0], lw))+
			valStyle.Render(Fit(Short(r[1], valW), valW)))
	}
	return lines
}

// mcpJoinArgs renders args for display ("—" when empty).
func mcpJoinArgs(args []string) string {
	if len(args) == 0 {
		return "—"
	}
	return strings.Join(args, " ")
}

// mcpJoinMap renders an env/header map for display (KEY=VALUE, sorted).
func mcpJoinMap(kv map[string]string) string {
	if len(kv) == 0 {
		return "—"
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+kv[k])
	}
	return strings.Join(parts, " ")
}

// mcpBaseName shortens a path for the detail column.
func mcpBaseName(p string) string {
	if p == "" {
		return "—"
	}
	return filepath.Base(p)
}

// ArmMcpRemove is the remove-confirmation gate (Exported: the Enter
// actions live in src/builtin, in the hub's action group and in the
// editor): first press arms, a second on the same server within the
// window removes. Removal deletes an entry that may hold hand-written
// env tokens.
//
// The arm is on the model so both surfaces share one gate, and is
// cleared whenever the editor opens or the rows rebuild — a fresh
// surface must never inherit a half-authorised delete.
func (m *Model) ArmMcpRemove(name string) bool {
	if m.mcpArmName == name && !m.mcpArmAt.IsZero() && time.Since(m.mcpArmAt) < mcpArmWindow {
		m.clearMcpArm()
		return true
	}
	m.mcpArmName, m.mcpArmAt = name, time.Now()
	// From the settings hub the prompt rides the Remove row itself (see
	// mcpHubPane and the panel's middle column): the instruction belongs
	// on the action it qualifies, and it leaves when the cursor does. The
	// standalone panel is a fallback for its wide three-column layout,
	// which drops per-row descriptions entirely.
	m.Status = "press Enter again to remove MCP server " + name
	m.Refresh()
	return false
}

// McpToggle flips a server's enabled flag in the config file `path`
// names (Exported: the hub and the editor both call it). The path is a
// parameter rather than a fresh resolve because the panel pins the file
// it is showing: resolving again would write a different mcp.json than
// the one on screen and leave the original entry behind. Pass the
// panel's McpPath, or "" to resolve.
func (m *Model) McpToggle(path, name string) (string, error) {
	doc, err := pirpc.LoadMcpConfig(mcpTargetPathString(path))
	if err != nil {
		return "", err
	}
	on, err := doc.ToggleDisabled(name)
	if err != nil {
		return "", err
	}
	if err := saveMcpDoc(doc); err != nil {
		return "", err
	}
	m.invalidateMcpInfo()
	if on {
		return name + " disabled", nil
	}
	return name + " enabled", nil
}

// McpRemove deletes a server from the config file `path` names, same
// path rule as McpToggle.
func (m *Model) McpRemove(path, name string) (string, error) {
	doc, err := pirpc.LoadMcpConfig(mcpTargetPathString(path))
	if err != nil {
		return "", err
	}
	if !doc.Delete(name) {
		return "", fmt.Errorf("no MCP server named %q", name)
	}
	if err := saveMcpDoc(doc); err != nil {
		return "", err
	}
	m.invalidateMcpInfo()
	return name + " removed", nil
}

// McpOpen reveals the config file in Finder/editor (best effort, off the
// event loop — the panel never blocks on it).
func (m *Model) McpOpen(path string) tea.Cmd {
	if path == "" {
		path = mcpDocPath(piAgentDir())
	}
	if path == "" {
		return nil
	}
	return func() tea.Msg {
		return McpPanelMsg{Action: "open", Err: openInDesktop(path)}
	}
}

// openInDesktop hands a file to the desktop opener.
func openInDesktop(path string) error {
	cands := [][]string{{"open", "-R", path}, {"xdg-open", path}, {"cmd", "/c", "start", "", path}}
	for _, c := range cands {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if err := exec.Command(c[0], c[1:]...).Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("no opener available for %s", mcpBaseName(path))
}

// McpPanelMsg reports the outcome of an off-loop panel action.
type McpPanelMsg struct {
	Action string
	Err    error
}

// McpReloadCmd respawns pi so it re-reads mcp.json — the same path as
// /reload (pi has no "reload" RPC, so the process restarts).
func (m *Model) McpReloadCmd() tea.Cmd {
	m.AddBlock(Block{Kind: "notice", Text: "reloading extensions — reconnecting pi…"})
	m.Refresh()
	return m.RespawnPi()
}

// RefreshMcpSnapshot drops the MCP sidebar cache and re-reads it, so a
// panel write shows up in the sidebar and in the hub's MCP section.
func (m *Model) RefreshMcpSnapshot() {
	mcpCacheAt = time.Time{}
	m.MCP = getMcpServers()
}

// refreshHubUnderneath rebuilds the settings hub's rows if one is waiting
// under the dialog that just closed. Its rows are built once at open, so
// without this a change made in a stacked dialog (MCP panel, plugin
// install) only shows up after the user navigates off the section and
// back — the hub would lie about state the panel just changed.
func (m *Model) refreshHubUnderneath() {
	if len(m.Dialogs) == 0 || m.Dialogs[0].Kind != "pconfig" {
		return
	}
	m.reloadHubRows("")
}

// ReloadMcpPanel refreshes the rows after a write, keeping the same
// server highlighted — by NAME, not by index: a rename (or a deletion
// ahead of it) would otherwise slide the cursor onto a neighbour.
func (m *Model) ReloadMcpPanel(d *Dialog) {
	m.RefreshMcpSnapshot()
	name := d.McpSelected()
	m.LoadMcpRows(d)
	for i, n := range d.McpNames {
		if n == name {
			d.ProvCursor = i
			m.LoadMcpRows(d) // labels and the action rows depend on the selection
			break
		}
	}
	m.Refresh()
}

// ---- add / edit form ----

// OpenMcpForm pushes the add/edit form. name=="" opens an empty form for
// a new server; otherwise the existing definition is prefilled.
//
// d is the panel the form sits on (nil when opened without one): both the
// prefill and the eventual save read and write THAT file. Resolving a
// different path here would show values from one config and save them into
// another.
func (m *Model) OpenMcpForm(d *Dialog, name string) {
	path := mcpTargetPath(d)
	def := pirpc.McpDef{Name: name}
	title := "Add MCP server"
	if name != "" {
		doc, err := pirpc.LoadMcpConfig(path)
		if err != nil {
			def = pirpc.McpDef{}
		} else {
			def = doc.Get(name)
		}
		title = "Edit MCP server — " + name
	}
	f := &Dialog{Kind: mcpFormKind, Title: title,
		Options:  append([]string(nil), mcpFields...),
		FormOrig: name, // the entry being edited ("" for add) — a rename deletes it
		FormPath: path, // the file the panel underneath is showing
		FormVals: []string{
			def.Name,
			def.Transport(),
			def.Command,
			def.URL,
			mcpJoinArgs(def.Args),
			mcpEnvJSON(def.Env),
			mcpEnvJSON(def.Headers),
			mcpYesNo(def.Disabled),
		}}
	// "—" is the empty marker for Args (join display); the form wants "".
	if f.FormVals[4] == "—" {
		f.FormVals[4] = ""
	}
	// Pristine copy for the Esc gate (FormVals is edited in place).
	f.FormPristine = append([]string(nil), f.FormVals...)
	m.Dialogs = append([]*Dialog{f}, m.Dialogs...)
	m.Refresh()
}

// mcpYesNo renders a bool as a form value.
func mcpYesNo(b bool) string {
	if b {
		return "no"
	}
	return "yes"
}

// mcpEnvJSON renders an env/headers map as the JSON object the form field
// holds (sorted keys, so the text is stable across opens).
func mcpEnvJSON(kv map[string]string) string {
	if len(kv) == 0 {
		return ""
	}
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		raw, err := json.Marshal(k)
		if err != nil {
			continue
		}
		b.Write(raw)
		b.WriteString(": ")
		raw, err = json.Marshal(kv[k])
		if err != nil {
			continue
		}
		b.Write(raw)
	}
	b.WriteString("}")
	return b.String()
}

// updateMcpForm drives the form: ↑↓ move between fields, every printable
// key edits the focused field, Enter advances and saves on the last field,
// Ctrl+S saves from anywhere, Esc cancels.
// Esc discards a typed-in form: first press arms the discard (like the
// remove gate), a second one within the window closes it.
const mcpFormDiscardWindow = 4 * time.Second

// mcpFormDiscardPrompt is the overlay form's Esc gate prompt, rendered only
// while the gate is armed. The inline hub editor has its own copy at
// mcpDiscardArmed.
const mcpFormDiscardPrompt = "press Esc again to discard the changes"

func (m Model) updateMcpForm(km tea.KeyMsg, d *Dialog) (tea.Model, tea.Cmd) {
	switch km.Type {
	case tea.KeyEsc:
		if mcpFormDirty(d) && !d.mcpFormDiscardArmed() {
			d.FormArmed, d.FormArmedAt = "esc", time.Now()
			m.Refresh()
			return m, nil
		}
		m.Dialogs = m.Dialogs[1:]
		m.refreshHubUnderneath()
		m.Refresh()
		return m, nil
	case tea.KeyUp:
		if d.FormFocus > 0 {
			d.FormFocus--
		}
		return m, nil
	case tea.KeyDown:
		if d.FormFocus < len(d.FormVals)-1 {
			d.FormFocus++
		}
		return m, nil
	case tea.KeyCtrlS:
		return m.saveMcpForm(d)
	case tea.KeyEnter:
		if d.FormFocus < len(d.FormVals)-1 {
			d.FormFocus++
			return m, nil
		}
		return m.saveMcpForm(d)
	case tea.KeyBackspace:
		d.formDelete(false)
		m.applyPopupH() // long values wrap: keep the winH budget
		return m, nil
	case tea.KeyDelete:
		d.formDelete(true)
		m.applyPopupH()
		return m, nil
	case tea.KeyLeft:
		d.setFormCur(d.formCur() - 1)
		return m, nil
	case tea.KeyRight:
		d.setFormCur(d.formCur() + 1)
		return m, nil
	case tea.KeyHome, tea.KeyCtrlA:
		d.setFormCur(0)
		return m, nil
	case tea.KeyEnd, tea.KeyCtrlE:
		d.setFormCur(len([]rune(d.formVal())))
		return m, nil
	case tea.KeySpace:
		d.formInsert(" ")
		m.applyPopupH()
		return m, nil
	}
	if km.Type == tea.KeyRunes {
		d.formInsert(km.String())
		m.applyPopupH() // long values wrap: keep the winH budget
		return m, nil
	}
	return m, nil
}

// formVal is the focused field's text ("" when the index is out of range).
func (d *Dialog) formVal() string {
	if d.FormFocus < 0 || d.FormFocus >= len(d.FormVals) {
		return ""
	}
	return d.FormVals[d.FormFocus]
}

// formCur is the caret index in the focused field, in runes, clamped.
// A missing or short entry means "end of value", which is where a
// prefilled field opens.
func (d *Dialog) formCur() int {
	n := len([]rune(d.formVal()))
	if d.FormFocus < 0 || d.FormFocus >= len(d.FormCur) {
		return n
	}
	c := d.FormCur[d.FormFocus]
	if c < 0 {
		return n // never parked: open at the end, not at Home
	}
	if c > n {
		return n
	}
	return c
}

// setFormCur parks the caret in the focused field, clamped.
func (d *Dialog) setFormCur(c int) {
	if d.FormFocus < 0 {
		return
	}
	// -1 is "no caret set yet", i.e. open at the end of the value. A
	// plain 0 would silently mean Home for every field the user has not
	// typed in, and backspace would do nothing there.
	for len(d.FormCur) < len(d.FormVals) {
		d.FormCur = append(d.FormCur, -1)
	}
	n := len([]rune(d.formVal()))
	if c < 0 {
		c = 0
	}
	if c > n {
		c = n
	}
	d.FormCur[d.FormFocus] = c
}

// formInsert types at the caret rather than at the end of the field.
func (d *Dialog) formInsert(s string) {
	if s == "" {
		return
	}
	r := []rune(d.formVal())
	c := d.formCur()
	ins := []rune(s)
	out := make([]rune, 0, len(r)+len(ins))
	out = append(out, r[:c]...)
	out = append(out, ins...)
	out = append(out, r[c:]...)
	d.setFormVal(string(out))
	d.setFormCur(c + len(ins))
}

// formDelete removes one rune: before the caret normally, at it on
// Delete.
func (d *Dialog) formDelete(atCaret bool) {
	r := []rune(d.formVal())
	c := d.formCur()
	i := c
	if !atCaret {
		i--
	}
	if i < 0 || i >= len(r) {
		return
	}
	d.setFormVal(string(append(append([]rune{}, r[:i]...), r[i+1:]...)))
	d.setFormCur(i)
}

// setFormVal writes the focused field, clamped to the field list.
func (d *Dialog) setFormVal(v string) {
	if d.FormFocus < 0 || d.FormFocus >= len(d.FormVals) {
		return
	}
	d.FormVals[d.FormFocus] = v
	if d.FormTouched == nil {
		d.FormTouched = map[string]bool{}
	}
	if d.FormFocus < len(d.Options) {
		// "Touched" means the value really changed in a way that matters:
		// a stray space in a row that prefills blank (a stored shape the
		// form cannot show) must not read as "clear this", but blanking a
		// row that had content must.
		name := d.Options[d.FormFocus]
		pristine := ""
		if i := indexOfField(d, name); i >= 0 && i < len(d.FormPristine) {
			pristine = d.FormPristine[i]
		}
		now := strings.TrimSpace(v)
		// Cleared = the row went raw-empty (an edit). Otherwise a real
		// change to real text. A stray space in a blank row is neither:
		// it must not read as "clear this". The flag is sticky — once a
		// row has been cleared, a following space must not put the
		// stored value back.
		if !d.FormTouched[name] {
			d.FormTouched[name] = (v == "" && pristine != "") ||
				(v != pristine && now != "")
		}
	}
	d.FormArmed, d.FormArmedAt = "", time.Time{} // typing un-arms the Esc gate
}

// indexOfField returns a form row's index in Options, or -1.
func indexOfField(d *Dialog, name string) int {
	for i, n := range d.Options {
		if n == name {
			return i
		}
	}
	return -1
}

// mcpFormDirty reports whether the form differs from its prefill, either
// by an edited value or by a row the user touched.
func mcpFormDirty(d *Dialog) bool {
	for name := range d.FormTouched {
		if d.FormTouched[name] {
			return true
		}
	}
	if len(d.FormPristine) != len(d.FormVals) {
		return true
	}
	for i := range d.FormVals {
		if d.FormVals[i] != d.FormPristine[i] {
			return true
		}
	}
	return false
}

// mcpFormValues is FormVals addressed by field name.
func mcpFormValues(d *Dialog) map[string]string {
	out := map[string]string{}
	for i, f := range d.Options {
		if i < len(d.FormVals) {
			out[f] = d.FormVals[i]
		}
	}
	return out
}

// mcpDefFromForm validates the OVERLAY form and builds the definition to
// save.
func mcpDefFromForm(d *Dialog, base pirpc.McpDef) (pirpc.McpDef, error) {
	return mcpDefFromVals(mcpFormValues(d), d.FormTouched, base)
}

// mcpDefFromVals is the one place a form becomes a server definition: the
// overlay form and the hub's inline editor both come through here, so the
// keep-unless-touched rule and every validation message are the same for
// both.
func mcpDefFromVals(v mcpFormVals, touched map[string]bool, base pirpc.McpDef) (pirpc.McpDef, error) {
	def := pirpc.McpDef{
		Name:     strings.TrimSpace(v["Name"]),
		Type:     strings.ToLower(strings.TrimSpace(v["Transport"])),
		Command:  strings.TrimSpace(v["Command"]),
		URL:      strings.TrimSpace(v["URL"]),
		Disabled: base.Disabled,
	}
	if def.Name == "" {
		return def, fmt.Errorf("name is required")
	}
	if strings.ContainsAny(def.Name, " \t/") {
		return def, fmt.Errorf("name must not contain spaces or /")
	}
	// Enabled is keep-unless-touched: an untouched row must not re-enable a
	// disabled server (a reflexive backspace is exactly the gesture that would
	// otherwise do it), and a blanked row falls back to the stored state
	// rather than guessing "yes".
	if v.touched("Enabled", touched) {
		switch strings.ToLower(strings.TrimSpace(v["Enabled"])) {
		case "yes":
			def.Disabled = false
		case "no", "disabled", "false":
			def.Disabled = true
		case "":
			def.Disabled = base.Disabled // blanked: keep what is stored
		default:
			return def, fmt.Errorf("Enabled must be yes or no")
		}
	}
	switch def.Type {
	case "", "stdio":
		def.Type = "" // stdio is the default; the key is omitted
		def.URL = ""  // a stdio entry never carries a URL
	case "http", "sse":
		def.Command = "" // switching transport must not leave a stale command
	default:
		return def, fmt.Errorf("transport must be stdio, http or sse")
	}
	if def.Type == "" {
		if def.Command == "" {
			return def, fmt.Errorf("a stdio server needs a command")
		}
	} else if def.URL == "" {
		return def, fmt.Errorf("a %s server needs a URL", def.Type)
	}
	// Keep-unless-touched: an untouched row leaves the field nil, and Put
	// then preserves whatever is stored (including shapes the form cannot
	// show). A touched row parses, and blank parses to a non-nil empty —
	// that is how a user actually clears one.
	//
	// base.Disabled is a floor: the prefilled Enabled row normally decides,
	// but a hand-built dialog without that row must not silently re-enable
	// a disabled server.
	if v.touched("Args", touched) {
		args, err := mcpParseArgs(v["Args"])
		if err != nil {
			return def, err
		}
		def.Args = args
	}
	if v.touched("Env", touched) {
		env, err := mcpParseJSONMap(v["Env"], "Env")
		if err != nil {
			return def, err
		}
		def.Env = env
	}
	if v.touched("Headers", touched) {
		hdr, err := mcpParseJSONMap(v["Headers"], "Headers")
		if err != nil {
			return def, err
		}
		if def.Type == "" && len(hdr) > 0 {
			return def, fmt.Errorf("headers only apply to http/sse servers")
		}
		def.Headers = hdr
	}
	return def, nil
}

// mcpParseArgs accepts a JSON array (["run","x"]) or shell-ish words
// (run x, single/double quotes supported), so the common case stays short
// to type while paths with spaces still work.
func mcpParseArgs(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return []string{}, nil // touched + blank = clear it
	}
	if strings.HasPrefix(s, "[") {
		var out []string
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return nil, fmt.Errorf("args: %w", err)
		}
		return out, nil
	}
	return mcpSplitWords(s)
}

// mcpSplitWords splits on whitespace, honouring ' and " quoting. A quote
// with no partner is an error rather than a silent wrong value.
func mcpSplitWords(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	inWord := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == ' ' || c == '\t':
			if inWord || cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("args: unclosed %c quote", quote)
	}
	if inWord || cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out, nil
}

// mcpParseJSONMap parses a touched JSON-object field; blank clears it.
func mcpParseJSONMap(s, label string) (map[string]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]string{}, nil // touched + blank = clear it
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return out, nil
}

// saveMcpForm writes the form's definition into mcp.json and returns to
// the panel underneath.
//
// The target file is the one the panel underneath is showing, not a fresh
// resolve: if mcp.json appeared while a form filled from .mcp.json was
// open, writing the resolved path would leave the original entry orphaned
// in the other file. Falls back to a resolve, then to the form's snapshot.
func (m Model) saveMcpForm(d *Dialog) (tea.Model, tea.Cmd) {
	path := d.FormPath
	if n := len(m.Dialogs); n > 1 {
		if pd := m.Dialogs[n-1]; pd.Kind == mcpKind && pd.McpPath != "" {
			path = pd.McpPath
		}
	}
	if path == "" {
		path = mcpTargetPath(nil)
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		d.FormErr = err.Error()
		m.Refresh()
		return m, nil
	}
	// Seed from the stored entry so untouched state survives: a disabled
	// server stays disabled, and args/env/headers the form could not show
	// are preserved rather than deleted. A rename deletes the old key, so
	// editing the name does not leave two live entries behind.
	orig := d.FormOrig // "" for the add form
	def, err := mcpDefFromForm(d, doc.Get(orig))
	if err != nil {
		d.FormErr = err.Error()
		m.Refresh()
		return m, nil
	}
	// Never land on a name that already exists: Put merges, so a rename or
	// a typo'd add would keep that server's collections while overwriting
	// its transport — a silent clobber of someone else's configuration.
	// Key presence, not field presence: an entry with no command and no
	// URL (a placeholder, a shape we do not model) still exists.
	if def.Name != orig && mcpHasServer(doc, def.Name) {
		d.FormErr = "a server named " + def.Name + " already exists"
		m.Refresh()
		return m, nil
	}
	if orig != "" && orig != def.Name {
		doc.Delete(orig)
	}
	if doc.Path == "" {
		doc.Path = path
	}
	doc.Put(def)
	if err := saveMcpDoc(doc); err != nil {
		d.FormErr = err.Error()
		m.Refresh()
		return m, nil
	}
	d.FormErr = ""
	m.invalidateMcpInfo() // the entry changed, so pi's report of it is stale
	m.Dialogs = m.Dialogs[1:]
	m.Status = "saved " + def.Name + " in " + mcpBaseName(path) + " — run /reload so pi picks it up"
	if n := len(m.Dialogs); n > 0 {
		if pd := m.Dialogs[n-1]; pd.Kind == mcpKind {
			for i, name := range pd.McpNames {
				if name == def.Name {
					pd.ProvCursor = i
				}
			}
			m.ReloadMcpPanel(pd)
			return m, nil
		}
	}
	m.RefreshMcpSnapshot()
	m.refreshHubUnderneath()
	m.Refresh()
	return m, nil
}

// renderMcpForm draws the form: one row per field, the focused one with a
// block cursor. Plain frame (no filter line — every key is text here).
// mcpCaretWindow splits `val` at rune index `c` for a row `room` cells
// wide, returning the two halves, each padded to exactly fill its share
// of the room. A value longer than the row is shown as a window that
// follows the caret: an MCP args/env field is routinely longer than a
// terminal cell, and a hard cut left the tail uneditable.
//
// Both halves are clamped to a non-negative width, so no caret
// position can render a malformed row.
func mcpCaretWindow(val string, c, room int) (before, after string) {
	if room < 1 {
		return "", ""
	}
	r := []rune(val)
	if c > len(r) {
		c = len(r)
	}
	// Cell budget to the left of the caret. When there is text after it,
	// only about two thirds goes left: a window that fills up to the
	// caret hides everything the caret is editing toward, which is the
	// half the user is working on.
	half := room - 1
	if len(r) > c {
		if third := half / 3; third > 0 && half-third > 0 {
			half -= third
		}
	}
	before = string(r[:c])
	bw := lipgloss.Width(before)
	if bw > half {
		// Scrolled: drop runes from the left until the text before the
		// caret fits. Elide the left edge so it is obvious there is more.
		keep := string(r[:c])
		for lipgloss.Width(keep) > half-1 && len(keep) > 0 {
			_, size := utf8.DecodeRuneInString(keep[1:])
			keep = keep[1+size:]
		}
		before, bw = "…"+keep, lipgloss.Width("…"+keep)
	}
	// The row is left-aligned: the text before the caret keeps its own
	// width, and whatever is left of the room goes to the right.
	return before, Fit(string(r[c:]), room-1-lipgloss.Width(before))
}

func (m Model) renderMcpForm(d *Dialog) string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(d.Title) + "\n\n")
	boxW := m.winW - 10
	if boxW < 60 {
		boxW = 60
	}
	if boxW > 100 {
		boxW = 100
	}
	const lw = 13
	for i, f := range d.Options {
		val := ""
		if i < len(d.FormVals) {
			val = d.FormVals[i]
		}
		if i == d.FormFocus {
			// The caret splits the value: ← → move it, and typing lands
			// where it sits. Padding comes after the caret, never between
			// the value and it.
			rr := []rune(val)
			c := d.formCur()
			if c > len(rr) {
				c = len(rr)
			}
			before, after := mcpCaretWindow(val, c, boxW-2-lw-1)
			b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cAccent).
				Render("▸ "+Fit(f+":", lw)) +
				cmdHiStyle.Render(before) + "▌" +
				lipgloss.NewStyle().Foreground(cAccent).Render(after) + "\n")
			continue
		}
		line := toolStyle.Render(Fit(f+":", lw))
		if val == "" {
			// Dim placeholder, still one row per field.
			b.WriteString(line + statusBarStyle.Render(Fit(mcpPlaceholder(f), boxW-2-lw)) + "\n")
			continue
		}
		b.WriteString(line + lipgloss.NewStyle().Foreground(cText).
			Render(Fit(Short(val, boxW-2-lw), boxW-2-lw)) + "\n")
	}
	// Separate blocks, never an else-if: FormErr is sticky (only a
	// successful save clears it), so an else-if left the gate armed with
	// nothing on screen to say so — the form looked frozen and the second
	// Esc discarded the edit without ever having shown a confirmation.
	if d.FormErr != "" {
		b.WriteString("\n" + errStyle.Render(Short(d.FormErr, boxW-2)) + "\n")
	}
	if d.mcpFormDiscardArmed() {
		b.WriteString("\n" + errStyle.Render(Short(mcpFormDiscardPrompt, boxW-2)) + "\n")
	}
	b.WriteString("\n" + toolStyle.Render("↑↓ field · Enter next/save · Ctrl+S save · Esc cancel"))
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center,
			dlgStyle.Width(boxW).Render(b.String())))
}

// mcpPlaceholder is the dim hint shown for an empty form field.
// mcpPlaceholder is the dim hint shown for an empty form row. The
// examples are the ones every MCP tutorial ships, so the shape of a real
// entry is obvious before the user knows pi's config format.
func mcpPlaceholder(field string) string {
	switch field {
	case "Name":
		return "server id, e.g. filesystem"
	case "Transport":
		return "stdio"
	case "Command":
		return "executable, e.g. npx"
	case "URL":
		return "https://mcp.example.com/mcp (http/sse only)"
	case "Args":
		return `-y @modelcontextprotocol/server-filesystem /tmp  or  ["-y","@modelcontextprotocol/server-filesystem"]`
	case "Env":
		return `{"GITHUB_TOKEN":"ghp_…"}`
	case "Headers":
		return `{"Authorization":"Bearer …"}`
	case "Enabled":
		return "yes"
	}
	return ""
}

// OpenMcpPanelOnPath opens the editor on a server, pinned to an explicit
// config file. pi tells us where a project-scoped server actually lives,
// and an edit must go there: resolving the agent dir instead would open
// a different server under the same name. An empty path means "resolve".
func (m *Model) OpenMcpPanelOnPath(path, name string) {
	m.clearMcpArm() // a fresh editor, a fresh gate
	d := &Dialog{Kind: mcpKind, Title: "MCP servers",
		Message:   "↑↓ server · ←→ pane · Enter runs the highlighted action · type filters · Esc closes",
		McpPath:   path,
		Filter:    "",
		RightHead: "ACTIONS", LeftHead: "SERVERS"}
	m.LoadMcpRows(d) // pins McpPath even when the file cannot be read
	m.Dialogs = append([]*Dialog{d}, m.Dialogs...)
	if name != "" {
		for i, n := range d.McpNames {
			if n == name {
				d.ProvCursor = i
				d.FIdx = []int{i}
				break
			}
		}
	}
	m.Status = "MCP: add · edit · toggle · remove a server from " + d.McpPath + " · Esc closes"
	m.Refresh()
}

// mcpEditFormLines renders the inline editor into pane 3: the same fields
// the overlay form has, in the same order, with the same placeholder hints.
func mcpEditFormLines(d *Dialog, w int) []string {
	e := d.McpEdit
	title := "EDITING " + e.Orig
	if e.Orig == "" {
		title = "NEW SERVER"
	}
	lines := []string{"  " + lipgloss.NewStyle().Bold(true).Foreground(cText).Render(Fit(title, w-2))}
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(cMuted).Render(Fit("in "+mcpBaseName(e.Path), w-2)))
	lines = append(lines, "")
	for i, f := range e.Fields {
		v := ""
		if i < len(e.Vals) {
			v = e.Vals[i]
		}
		mark := "  "
		labelStyle := lipgloss.NewStyle().Foreground(cMuted)
		valStyle := lipgloss.NewStyle().Foreground(cText)
		focused := i == e.Focus
		if focused {
			mark = "▸ "
			// The focused row reads as the active one on its own terms:
			// a bright label plus a block cursor at the caret, which
			// ← → move. A caret alone was not enough — a value of the
			// same colour as the rest of the form looked unselected.
			labelStyle = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
			valStyle = lipgloss.NewStyle().Foreground(cAccent)
		}
		caret := ""
		before, after := v, ""
		if focused {
			caret = "▌" // the insertion point: keystrokes land here
			r := []rune(v)
			c := d.McpEditCur()
			if c > len(r) {
				c = len(r)
			}
			before, after = string(r[:c]), string(r[c:])
		}
		if v == "" {
			if focused {
				v = mcpPlaceholder(f) // placeholder, with the caret before it
				before, after = v, ""
			} else {
				v = "—"
				valStyle = toolStyle
			}
		}
		const lw = 9
		// Every row of this pane is w-2 wide: mark(2) + label + 2 + value
		// + caret. Sizing the value against w instead of w-2 pushed the
		// caret one column past the pane's right edge, where it was
		// clipped — the focused row looked plain.
		room := w - 4 - lw - 2
		if room < 8 {
			room = 8
		}
		// mcpCaretWindow keeps the caret in view for a value longer than
		// the row and pads both halves, so the row is exactly w-2 wide
		// whatever the caret is doing.
		wBefore, wAfter := "", ""
		if focused {
			wBefore, wAfter = mcpCaretWindow(before+after, d.McpEditCur(), room-lipgloss.Width(caret))
		} else {
			wBefore, wAfter = Fit(Short(v, room), room), ""
		}
		body := mark + labelStyle.Render(Fit(f, lw)) + "  " +
			valStyle.Render(wBefore) + cmdHiStyle.Render(caret) +
			valStyle.Render(wAfter)
		lines = append(lines, body+strings.Repeat(" ", max(0, w-2-lipgloss.Width(body))))
	}
	// The discard prompt is derived, not stored: it exists only while the
	// gate is genuinely armed. A validation or save error outranks it —
	// "a server named X already exists" is the highest-value thing this
	// line can say, and gate chrome must not stand over it.
	discardPrompt := e.Err
	if discardPrompt == "" && d.mcpDiscardArmed() {
		discardPrompt = "press Esc again to discard the changes"
	}
	if discardPrompt != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+errStyle.Render(Fit(discardPrompt, w-2)))
	}
	lines = append(lines, "")
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(cMuted).Render("Ctrl+S save · Esc cancel"))
	return lines
}

// updateMcpEditHub drives the inline editor: runes edit the focused field,
// ↑↓ move between fields, Ctrl+S saves, Esc cancels (with a two-press gate
// once something has been typed).
func (m Model) updateMcpEditHub(km tea.KeyMsg, d *Dialog) (tea.Model, tea.Cmd) {
	e := &d.McpEdit
	switch km.Type {
	case tea.KeyUp:
		if e.Focus > 0 {
			e.Focus--
		}
		m.Refresh()
		return m, nil
	case tea.KeyDown:
		if e.Focus < len(e.Fields)-1 {
			e.Focus++
		}
		m.Refresh()
		return m, nil
	case tea.KeyCtrlS:
		return m.saveMcpEditHub(d)
	case tea.KeyEsc:
		if d.mcpEditDirty() {
			// The gate: a typed form is not thrown away by one reflex Esc.
			if d.mcpDiscardArmed() {
				return m.cancelMcpEditHub(d)
			}
			e.Armed, e.ArmedAt = "discard", time.Now()
			m.Refresh()
			return m, nil
		}
		return m.cancelMcpEditHub(d)
	case tea.KeyBackspace:
		d.McpEditDelete(false)
		m.Refresh()
		return m, nil
	case tea.KeyDelete:
		d.McpEditDelete(true)
		m.Refresh()
		return m, nil
	case tea.KeyLeft:
		d.McpEditCaret(-1)
		m.Refresh()
		return m, nil
	case tea.KeyRight:
		d.McpEditCaret(1)
		m.Refresh()
		return m, nil
	case tea.KeyHome, tea.KeyCtrlA:
		d.McpEditSetCur(0)
		m.Refresh()
		return m, nil
	case tea.KeyEnd, tea.KeyCtrlE:
		d.McpEditSetCur(len(d.mcpEditRunes()))
		m.Refresh()
		return m, nil
	}
	if km.Type == tea.KeyRunes {
		d.McpEditInsert(km.String())
		m.Refresh()
		return m, nil
	}
	return m, nil
}

// cancelMcpEditHub closes the editor and puts the actions back.
func (m Model) cancelMcpEditHub(d *Dialog) (tea.Model, tea.Cmd) {
	d.McpEdit = McpEditMode{}
	d.McpActFocus = true
	d.McpActCursor = 0
	m.refreshMcpActions(d)
	m.Status = "edit cancelled"
	m.Refresh()
	return m, nil
}

// saveMcpEditHub writes the entry through the same path the overlay form
// uses: the file pi reported, a CAS against the bytes we read, a .bak, and
// the same name-collision guard.
func (m Model) saveMcpEditHub(d *Dialog) (tea.Model, tea.Cmd) {
	e := &d.McpEdit
	// Saving is a decision, not a reflex: it disarms the Esc gate, so a
	// rejected save reports its own error instead of the gate's prompt
	// standing over it.
	e.Armed, e.ArmedAt = "", time.Time{}
	path := e.Path
	if path == "" {
		path = mcpTargetPath(nil)
	}
	doc, err := pirpc.LoadMcpConfig(path)
	if err != nil {
		e.Err = err.Error()
		m.Refresh()
		return m, nil
	}
	// Seed from the stored entry so untouched state survives (a disabled
	// server stays disabled; args/env/headers the form cannot show are
	// preserved rather than deleted).
	def, err := mcpDefFromVals(e.vals(), e.Touched, doc.Get(e.Orig))
	if err != nil {
		e.Err = err.Error()
		m.Refresh()
		return m, nil
	}
	// Put MERGES, so a rename or a typo would silently take over an existing
	// entry's transport. Key presence, not field presence.
	if def.Name != e.Orig && mcpHasServer(doc, def.Name) {
		e.Err = "a server named " + def.Name + " already exists"
		m.Refresh()
		return m, nil
	}
	if e.Orig != "" && e.Orig != def.Name {
		doc.Delete(e.Orig)
	}
	if doc.Path == "" {
		doc.Path = path
	}
	doc.Put(def)
	if err := saveMcpDoc(doc); err != nil {
		e.Err = err.Error()
		m.Refresh()
		return m, nil
	}
	d.McpEdit = McpEditMode{}
	m.invalidateMcpInfo() // the entry changed, so pi's report is stale
	m.RefreshMcpSnapshot()
	m.Status = "saved " + def.Name + " in " + mcpBaseName(path) + " — run /reload so pi picks it up"
	// Re-read pi's list so the rows and pane 3 show what pi now thinks
	// rather than what the file said a moment ago.
	return m, m.RunBuiltin(BuiltinMcpList, "")
}

// vals is the inline editor's values as the shape the def builder wants.
func (e *McpEditMode) vals() mcpFormVals {
	out := mcpFormVals{}
	for i, f := range e.Fields {
		if i < len(e.Vals) {
			out[f] = e.Vals[i]
		}
	}
	return out
}
