package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/components/theme"
	"pitago/src/pirpc"
)

// Pitago settings hub (/pitago-setting): two-pane dialog like the /model
// picker — left = sections, right = the selected section's rows. All rows
// come from Model state (no RPC), so the hub opens instantly.
// Keys: ↑↓ move in the focused pane, ←/→/Tab switch pane, typing filters
// the right pane, Enter runs/opens, Esc closes.

// Hub section ids (Dialog.PsecIDs parallels the left pane).
const (
	PsecAgent  = "agent"
	PsecCmd    = "command"
	PsecKeys   = "shortcut"
	PsecSkill  = "skill"
	PsecPrompt = "prompt"
	PsecExt    = "extension"
	PsecPlugin = "plugin"
	PsecMarket = "market"
	PsecMCP    = "mcp"
	PsecTool   = "tool"
	PsecTasks  = "tasks"
	PsecSide   = "side"
	PsecTheme  = "theme"
	PsecLogin  = "login"
)

// Payload markers for non-runnable right rows: "@<section>" opens the
// classic single dialog, "" is info-only (toast hint on Enter). Runnables
// that mutate in place use their own prefix ("side:", "tasks:", "theme:") —
// see confirmPconfig in src/builtin.
const (
	psecActAgent = "@agent"
	psecActLogin = "@login"
	psecActMCP   = "@mcp"
	// psecActMCPEdit+"@<server>" opens the editor on that server;
	// psecActMCPCLI fills pi's own /mcp command.
	psecActMCPEdit = "@mcpedit:"
	// psecActMCPAdd is the editor row at the bottom of the server list.
	// It is the one row pitago adds to pi's menu.
	psecActMCPAdd = "@mcpedit"
	psecActMCPCLI = "@mcpcli"
	// psecActMCPSel+"<server>" is a server row in the hub's MCP section;
	// psecActMCPAct+"<kind>@<server>" is an action on that server.
	psecActMCPSel = "@mcpsel:"
	psecActMCPAct = "@mcpact:"
)

// CountCmds tallies extension catalog commands per source (skill/prompt/
// extension) for the section labels.
func CountCmds(cmds []pirpc.RepoCommand) (skill, prompt, ext int) {
	for _, c := range cmds {
		switch c.Source {
		case "skill":
			skill++
		case "prompt":
			prompt++
		case "extension":
			ext++
		}
	}
	return skill, prompt, ext
}

// psecCount returns the left-pane badge for a section (-1 = no badge).
func psecCount(m *Model, id string) int {
	sk, pr, ex := CountCmds(m.Cmds)
	switch id {
	case PsecCmd:
		return len(m.builtinCmdRows())
	case PsecKeys:
		return len(m.shortcutRows())
	case PsecSkill:
		return sk
	case PsecPrompt:
		return pr
	case PsecExt:
		return ex
	case PsecPlugin:
		return len(m.Plugins)
	case PsecMarket:
		if m.Market == nil && m.MarketErr == "" {
			return -1 // not loaded yet: no badge
		}
		return len(m.Market)
	case PsecMCP:
		if len(m.McpInfo) > 0 {
			return len(m.McpInfo) // the section lists pi's rows, so count those
		}
		return len(m.MCP)
	case PsecTool:
		return m.Stats.ToolCalls
	}
	return -1
}

// OpenPconfig pushes the two-pane settings hub (focus starts on sections).
func (m *Model) OpenPconfig() {
	d := &Dialog{Kind: "pconfig", Title: "Pitago settings",
		Provs:     []string{"Agent", "Commands", "Shortcuts", "Skills", "Prompts", "Extensions", "Plugins", "Marketplace", "MCP", "Tools", "Tasks", "Sidebar", "Theme", "Login"},
		PsecIDs:   []string{PsecAgent, PsecCmd, PsecKeys, PsecSkill, PsecPrompt, PsecExt, PsecPlugin, PsecMarket, PsecMCP, PsecTool, PsecTasks, PsecSide, PsecTheme, PsecLogin},
		ProvFocus: true}
	m.LoadPsecRows(d)
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
}

// tasksSettingsJump reports the /tasks-menu Settings row.
func tasksSettingsJump(d *Dialog, choice int) bool {
	return d != nil && d.Kind == "ui" && d.Title == "Tasks" &&
		choice >= 0 && choice < len(d.Options) && d.Options[choice] == "Settings"
}

// openTasksSettings opens the hub focused on the native Tasks tab
// (right pane focused, ready to cycle values).
func (m *Model) openTasksSettings() {
	m.OpenHubSection(PsecTasks)
}

// OpenKeysSettings opens the settings hub on the Shortcuts section. The
// standalone shortcuts dialog is gone — /shortcuts lands here.
func (m *Model) OpenKeysSettings() {
	m.OpenHubSection(PsecKeys)
}

// OpenHubSection opens the hub with the right pane already focused on
// one section (/shortcuts → Shortcuts, /tasks → Settings).
func (m *Model) OpenHubSection(id string) {
	m.OpenPconfig()
	if n := len(m.Dialogs); n > 0 {
		if hd := m.Dialogs[n-1]; hd.Kind == "pconfig" {
			for i, sid := range hd.PsecIDs {
				if sid == id {
					hd.ProvCursor = i
				}
			}
			hd.ProvFocus = false
			m.LoadPsecRows(hd)
		}
	}
}

// reloadHubRows rebuilds the open settings hub's right pane in place
// (section=="" keeps the current section): async results (market fetch,
// install/remove) refresh the rows without closing the hub or losing
// the cursor.
// refreshHubSection rebuilds one section's rows in place, but ONLY when
// the hub is already showing that section. Background work (a registry
// page landing, stars, a sort) must never yank the hub to another
// section: reloadHubRows(PsecMarket) moves the left-pane cursor, so a
// result that lands while the user browsed elsewhere steals the focus.
// The selection follows its package, not its old row index.
func (m *Model) refreshHubSection(id string) {
	d := m.hubDialog()
	if d == nil || d.CurPsec() != id {
		return
	}
	sel := ""
	if ri := psecCursor(d); ri >= 0 && id == PsecMarket && ri < len(m.Market) {
		sel = m.Market[ri].Name
	}
	m.reloadHubRows("")
	if sel != "" {
		if d := m.hubDialog(); d != nil {
			for fi, ri := range d.FIdx {
				if ri < len(m.Market) && m.Market[ri].Name == sel {
					d.Cursor = fi
					break
				}
			}
		}
	}
}

func (m *Model) reloadHubRows(section string) {
	if len(m.Dialogs) == 0 || m.Dialogs[0].Kind != "pconfig" {
		return
	}
	d := m.Dialogs[0]
	if section != "" {
		for i, id := range d.PsecIDs {
			if id == section {
				d.ProvCursor = i
			}
		}
	}
	cur := d.Cursor
	m.LoadPsecRows(d)
	if cur < len(d.FIdx) {
		d.Cursor = cur
	}
}

// hubDialog returns the open settings hub, or nil when none is up.
func (m *Model) hubDialog() *Dialog {
	if len(m.Dialogs) == 0 {
		return nil
	}
	if d := m.Dialogs[0]; d.Kind == "pconfig" {
		return d
	}
	return nil
}

// hubWindow is the hub's fixed row window (both panes): the render clamps
// it, and the star hydration reads the same window off the model so the
// background fetch only ever covers what is on screen.
func hubWindow(m *Model) int {
	win := m.winH - 14
	if win < 12 {
		win = 12
	}
	if win > 24 {
		win = 24
	}
	return win
}

// mcpHubDetailW is the box width at or above which the hub's MCP section
// spends its middle pane on names and draws the actions in a third column.
const mcpHubDetailW = 110

// syncMcpActDrawn keeps the column's reachability in one place. A focus
// can outlive the column — a resize, moving off the MCP section, cancelling
// the inline editor — and a focus on an undrawn column is worse than none:
// ↑↓ move an invisible cursor and ← is swallowed. So the drawn-ness is
// stamped on the dialog for the package-level helpers to consult, and a
// focus that no longer has a column under it is dropped.
func (m Model) syncMcpActDrawn(d *Dialog) {
	d.McpActDrawn = m.mcpActColumnDrawn(d)
	if !d.McpActDrawn {
		d.McpActFocus, d.McpActRun = false, false
	}
}

// SyncMcpHubActions stamps the column's reachability onto the dialog.
// Exported because anything that drives the hub's actions outside its own
// key handling — src/builtin's confirm runner, and its tests — has to
// establish the same invariant before asking for an action, exactly as a
// keypress or a render does.
func (m *Model) SyncMcpHubActions(d *Dialog) { m.syncMcpActDrawn(d) }

// mcpActColumnDrawn reports whether that third column is actually on
// screen. The middle pane drops the desc text whenever the column exists,
// so on a narrower terminal there is no desc AND no column — and an
// undrawn column must not be enterable or runnable, or the user runs a
// state-changing action (Disable) against a server whose state, exposure
// and scope are nowhere on screen.
func (m Model) mcpActColumnDrawn(d *Dialog) bool {
	// The MCP manager panel lays its own actions column out, so it is drawn
	// whatever the width. Only the hub's section has to trade the middle
	// pane's width against the third one.
	if d.Kind == mcpKind {
		return true
	}
	return d.Kind == "pconfig" && d.CurPsec() == PsecMCP &&
		hubBoxW(&m) >= mcpHubDetailW
}

// hubBoxW is the hub dialog's outer width. One source of truth: the
// renderer lays the box out at this width, and psecRows clamps its
// section message to it so a long line cannot wrap and stretch the box.
func hubBoxW(m *Model) int {
	w := m.winW - 10
	if w < 70 {
		w = 70
	}
	if w > 150 {
		w = 150
	}
	return w
}

// loadRows rebuilds the focused dialog's right pane. The MCP manager is
// the only other two-pane dialog and brings its own rows.
func (m Model) loadRows(d *Dialog) {
	if d.Kind == mcpKind {
		m.LoadMcpRows(d)
		return
	}
	m.LoadPsecRows(d)
}

// detailLines builds the optional third (DETAILS) column. Plugins and
// Marketplace own their specs; the MCP editor panel shows the selected
// server. (The hub's MCP section has no third column: pi's row already
// carries state · exposure · scope, and squeezing it into a narrow
// column would be a worse trade than one less column.)
func (m Model) detailLines(d *Dialog, w int) []string {
	if d.Kind == mcpKind {
		return m.mcpDetailLines(d, w)
	}
	if d.CurPsec() == PsecMarket {
		return marketDetailLines(&m, d, w)
	}
	return pluginDetailLines(&m, d, w)
}

// detailHead titles the third column. The MCP section's carries the
// server's name, since its column is about one server.
func (m Model) detailHead(d *Dialog) string {
	if d.Kind != "pconfig" || d.CurPsec() != PsecMCP {
		return "DETAILS"
	}
	if sel := m.mcpHubTarget(d); sel != "" {
		return sel
	}
	return "MCP"
}

// mcpHubPane is the third column for the MCP section, in the same shape as
// the Plugins column: a label/value block of what the server is, and then —
// under it, in this column — the actions you can run on it. The block is
// what tells you why a server is failing; the actions are what fix it.
//
// Only the action rows are navigable (→, ↑↓, Enter); the block is read,
// like every other details block in the hub.
func (m Model) mcpHubPane(d *Dialog, w int) []string {
	if w < 30 {
		w = 30
	}
	if d.McpEdit.Active {
		return mcpEditFormLines(d, w)
	}
	lines := m.mcpHubDetailLines(d, w)
	if len(d.McpAct) == 0 {
		return lines
	}
	// A blank line: the rows above are read (a description), the rows
	// below run (Enter), and the gap is what says so.
	lines = append(lines, "")
	lines = append(lines, "  "+lipgloss.NewStyle().Foreground(cMuted).Render("ACTIONS"))
	for i, o := range d.McpAct {
		mark := "  "
		style := statusBarStyle
		if i == d.McpActCursor {
			mark = "▸ "
			style = rowHiStyle
			if !d.McpActFocus {
				style = lipgloss.NewStyle().Foreground(cText)
			}
		}
		label := Short(o, w-4)
		row := label
		// An armed remove announces itself on its own row, not in the
		// message strip at the top: the prompt is about this action, sits
		// next to it, and leaves when the cursor does. Reading "press
		// Enter again" against an unrelated top-of-panel line was the
		// whole reason it was easy to miss. (The per-row descs stay
		// undrawn on purpose — this column sits beside the detail block,
		// which already says what each action does.)
		if m.mcpArmedFor(d, i) && i == d.McpActCursor {
			const prompt = "press Enter again to remove · any other key cancels"
			// Room is measured from the label, not from an already-padded
			// row: padding first left it negative and the prompt never fit.
			if room := w - 4 - lipgloss.Width(label) - 2; room > 8 {
				row = label + "  " + Fit(Short(prompt, room), room)
			} else {
				// Too narrow for the row: say it on the pane's own line
				// rather than drop the only warning the user gets.
				lines = append(lines, "  "+errStyle.Render(Fit(prompt, w-2)))
			}
		}
		lines = append(lines, mark+style.Width(w-2).Render(Fit(row, w-4)))
	}
	return lines
}

// mcpArmedFor reports whether the half-armed remove gate is waiting on
// the action row at `i` of the highlighted server.
//
// The predicate is the SAME one ArmMcpRemove enforces, window included:
// a prompt that outlived mcpArmWindow would promise a second Enter that
// re-arms instead of deleting. Scoped to both server and row: an arm on
// one server must not light up another's row, and it lights only the
// remove row, matched on the action KIND rather than on the label.
func (m Model) mcpArmedFor(d *Dialog, i int) bool {
	if i < 0 || i >= len(d.McpActPayload) {
		return false
	}
	if !strings.HasSuffix(d.McpActPayload[i], psecActMCPAct+"remove@"+m.mcpHubTarget(d)) {
		return false
	}
	return m.McpRemoveArmed(m.mcpHubTarget(d))
}

// mcpHubDetailLines is pi's detail block for the highlighted server, as
// label/value rows like the rest of the hub's third column: what pi
// reports, then the entry it comes from. The entry comes from the snapshot
// taken when the rows were built — a render must never touch the disk.
func (m Model) mcpHubDetailLines(d *Dialog, w int) []string {
	if w < 30 {
		w = 30
	}
	sel := m.mcpHubTarget(d)
	if sel == "" {
		return []string{"  " + toolStyle.Width(w-2).Render("— no server selected —")}
	}
	srv, known := m.McpInfoFor(sel)
	if known {
		return append(mcpHubServerRows(srv, w), mcpHubEntryRows(sel, &m, w)...)
	}
	// pi has not listed it (or not at all): say so, and show the entry that
	// is on disk, which is still worth reading.
	rows := []string{mcpDetailRow("State", "not listed yet — Reload pi MCPs", w)}
	return append(rows, mcpHubEntryRows(sel, &m, w)...)
}

// mcpHubServerRows is pi's own report of the server: state, exposure,
// which file declared it, and its error if it has one.
func mcpHubServerRows(srv pirpc.McpServerInfo, w int) []string {
	// A healthy server is green, the way the Marketplace column greens
	// "installed": the state is the one value in this pane that is good or
	// bad at a glance.
	rows := []string{
		mcpStyledDetailRow("State", mcpStateOf(srv, true), mcpHealthy(srv), w),
		mcpDetailRow("Tools", strings.Join(srv.Tools, ", "), w),
		mcpDetailRow("Exposure", mcpExposureName(srv), w),
		mcpDetailRow("Scope", mcpScopeLabel(srv), w),
	}
	if src := srv.Source; src != "" {
		rows = append(rows, mcpDetailRow("File", mcpTailPath(src, w-12), w))
	}
	if e := mcpServerError(srv); e != "" {
		rows = append(rows, mcpDetailRow("Error", e, w))
	}
	return rows
}

// mcpHubEntryRows is the entry itself: what pi is running, read from
// mcp.json's snapshot.
func mcpHubEntryRows(sel string, m *Model, w int) []string {
	def, have := m.mcpHubDef(sel)
	if !have {
		return []string{mcpDetailRow("Entry", "not in the agent dir's mcp.json", w)}
	}
	label, target := "Command", def.Command
	if def.Transport() != "stdio" {
		label, target = "URL", def.URL
	}
	return []string{
		mcpDetailRow(label, target, w),
		mcpDetailRow("Args", mcpJoinArgs(def.Args), w),
		mcpDetailRow("Env", mcpJoinMap(def.Env), w),
	}
}

// mcpHealthy is the one question the colour answers: is this server up?
func mcpHealthy(s pirpc.McpServerInfo) bool {
	return s.Enabled && s.State == "connected"
}

// mcpTailPath shortens a path from the LEFT, so the file name (which is
// the part that says which config this is) always survives.
func mcpTailPath(path string, w int) string {
	if w < 12 || len(path) <= w {
		return path
	}
	return "…" + path[len(path)-w+1:]
}

// mcpDetailRow is one "Label   value" line, styled like the other third
// columns.
func mcpDetailRow(label, value string, w int) string {
	return mcpStyledDetailRow(label, value, false, w)
}

// mcpStyledDetailRow is mcpDetailRow with a style for the value, for the
// ones that carry meaning in their colour (a green "connected").
func mcpStyledDetailRow(label, value string, ok bool, w int) string {
	if value == "" {
		value = "—"
	}
	const lw = 8
	valW := w - 2 - lw - 1
	if valW < 10 {
		valW = 10
	}
	val := lipgloss.NewStyle().Foreground(cText)
	if ok {
		val = okStyle
	}
	return "  " + lipgloss.NewStyle().Foreground(cMuted).Render(Fit(label, lw)) + " " +
		val.Render(Fit(value, valW))
}

// mcpStateOf is pi's describeState for a row outside the menu.
func mcpStateOf(s pirpc.McpServerInfo, withError bool) string {
	var m Model
	return m.mcpState(s, withError)
}

// twoPaneHeads returns the left/middle column headers ("SECTIONS" and the
// section name for the hub; "SERVERS"/"ACTIONS" for the MCP panel).
func (d *Dialog) twoPaneHeads() (left, mid string) {
	left, mid = d.LeftHead, d.RightHead
	if left == "" {
		left = "SECTIONS"
	}
	if mid == "" {
		mid = d.CurPsec()
		if d.ProvCursor >= 0 && d.ProvCursor < len(d.Provs) {
			mid = d.Provs[d.ProvCursor]
		}
	}
	return left, mid
}

// CurPsec is the selected section id (left pane cursor).
func (d *Dialog) CurPsec() string {
	if len(d.PsecIDs) == 0 {
		return ""
	}
	if d.ProvCursor < 0 || d.ProvCursor >= len(d.PsecIDs) {
		return d.PsecIDs[0]
	}
	return d.PsecIDs[d.ProvCursor]
}

// LoadPsecRows rebuilds the right pane for the selected section.
func (m *Model) LoadPsecRows(d *Dialog) {
	if d.CurPsec() == PsecMCP {
		// The section's DETAILS column renders from this cache, and a
		// render must not read the disk — so the read happens here, on
		// the row build. It also means the column is populated the first
		// time the section is opened, before pi has ever been listed.
		m.refreshMcpHubDefs()
	}
	opts, descs, payload, msg := psecRows(m, d)
	d.Options, d.Descs, d.Payload = opts, descs, payload
	d.Message = msg
	d.Cursor = 0
	d.Reindex()
	// The MCP section's third column is derived from the rows that were
	// JUST assigned: built inside psecRows it would read the previous
	// row set (the first build has no payload at all).
	if d.CurPsec() == PsecMCP {
		m.refreshMcpActions(d)
	}
}

// curatedPlugins are the pi packages pitago is built around. They ride the
// Plugins section as ★ rows: installed or not, the star marks the row as a
// suggestion. name is the npm name — pi install takes "npm:<name>".
var curatedPlugins = []pluginSuggestion{
	{"pi-subagents", "delegate work to background teams"},
	{"pi-ask-user", "handshake before risky decisions"},
	{"@dietrichgebert/ponytail", "lazy-senior mode: less code, fewer deps"},
}

// pluginSuggestion is one ★ package in the Plugins section.
type pluginSuggestion struct{ name, why string }

// suggestions returns every suggested package: the curated list plus the
// user's own picks (prefs.json, added with Ctrl+F), curated first and
// deduped. Static per row build — the model mirrors prefs.
func (m *Model) suggestions() []pluginSuggestion {
	out := make([]pluginSuggestion, 0, len(curatedPlugins)+len(m.SuggestPlugins))
	seen := map[string]bool{}
	for _, c := range curatedPlugins {
		if c.name != "" && !seen[c.name] {
			seen[c.name] = true
			out = append(out, c)
		}
	}
	for _, n := range m.SuggestPlugins {
		if n = pluginName(strings.TrimSpace(n)); n != "" && !seen[n] {
			seen[n] = true
			out = append(out, pluginSuggestion{name: n, why: "your pick"})
		}
	}
	return out
}

// suggestAddKind is the free-text prompt Ctrl+F opens in the Plugins tab.
const suggestAddKind = "pluginadd"

// openSuggestAdd pushes the "add a suggested plugin" prompt over the hub
// (same stacking as the shortcut capture, so Esc returns to the tab).
func (m *Model) openSuggestAdd() {
	d := &Dialog{Kind: suggestAddKind, Title: "Suggest a plugin",
		Message: "npm name · Enter adds it to the ★ list · Esc cancels"}
	m.Dialogs = append([]*Dialog{d}, m.Dialogs...)
	m.Refresh()
}

// AddSuggestPlugin persists a user-picked suggested package (Ctrl+F). Bare
// npm name: an "npm:" prefix the user pasted is stripped, and anything pi
// would refuse as a spec never reaches prefs.json.
func (m *Model) AddSuggestPlugin(name string) {
	name = pluginName(strings.TrimSpace(name))
	if name == "" {
		return
	}
	if !validPluginSpec("npm:" + name) {
		m.AddBlock(Block{Kind: "notice", Text: "not an npm package name: " + name, Err: true})
		m.Refresh()
		return
	}
	prefs := LoadPrefs(m.prefsPath)
	for _, s := range m.suggestions() {
		if s.name == name {
			m.AddBlock(Block{Kind: "notice", Text: name + " is already suggested"})
			m.Refresh()
			return
		}
	}
	prefs.SuggestPlugins = append(prefs.SuggestPlugins, name)
	if err := SavePrefs(m.prefsPath, prefs); err != nil {
		m.AddBlock(Block{Kind: "notice", Text: "prefs.json: " + err.Error(), Err: true})
		m.Refresh()
		return
	}
	m.SuggestPlugins = prefs.SuggestPlugins
	m.AddBlock(Block{Kind: "notice", Text: "★ " + name + " suggested — Enter installs it"})
	m.Refresh()
}

// psecRows builds one section's right pane. Payload parallels Options: the
// /command name for runnable rows, "@agent"/"@theme"/"@login" for action
// rows, "" for info-only rows.
func psecRows(m *Model, d *Dialog) (opts, descs, payload []string, msg string) {
	switch d.CurPsec() {
	case PsecAgent:
		msg = "Enter opens the agent settings dialog · Esc closes"
		return []string{"Open agent settings →"},
			[]string{"model · thinking · steering · images · skills · … (pi parity)"},
			[]string{psecActAgent}, msg
	case PsecTheme:
		// The theme list lives here now (the standalone picker is gone).
		// ↑↓ live-previews through previewTheme, Enter applies and keeps
		// the hub open. Accent hex rides the desc so the row is filterable.
		msg = "↑↓ previews live · Enter applies · Esc closes"
		for _, n := range theme.Names() {
			opts = append(opts, n)
			desc := theme.Get(n).Accent
			if n == m.currentTheme() {
				desc = "✓ current · " + desc
			}
			descs = append(descs, desc)
			payload = append(payload, "theme:"+n)
		}
		if len(opts) == 0 {
			opts = []string{"— no themes —"}
			descs = []string{"the theme table is empty"}
			payload = []string{""}
		}
	case PsecLogin:
		msg = "Enter opens login · Esc closes"
		return []string{"Open login →"},
			[]string{"providers · keys · OAuth"},
			[]string{psecActLogin}, msg
	case PsecCmd:
		// The local registry only: pi's re-implemented builtins and
		// pitago's own commands. Extension/prompt/skill commands stay in
		// their own sections, so nothing third-party lands here.
		msg = "Enter fills /command · Ctrl+S assigns Alt-shortcut · Esc closes"
		for _, r := range m.builtinCmdRows() {
			opts = append(opts, "/"+r.name)
			descs = append(descs, shortDesc(r.desc, 42)+" ["+r.tag+"]"+m.shortcutSuffix(r.name))
			payload = append(payload, r.name)
		}
		if len(opts) == 0 {
			opts = []string{"— no commands —"}
			descs = []string{"the command registry is empty"}
			payload = []string{""}
		}
	case PsecKeys:
		// The /shortcuts reference, in the hub: every built-in key plus the
		// Alt shortcuts assigned from the Commands section. Custom rows
		// carry their /command as payload, so Ctrl+S re-opens the capture
		// dialog for it; the built-in keys are info-only.
		msg = "keyboard reference · type filters · Ctrl+S reassigns a custom row · Esc closes"
		for _, r := range m.shortcutRows() {
			opts = append(opts, r.key)
			descs = append(descs, shortDesc(r.desc, 40)+" ["+r.cat+"]")
			payload = append(payload, r.cmd)
		}
		if len(opts) == 0 {
			opts = []string{"— no shortcuts —"}
			descs = []string{"the shortcut table is empty"}
			payload = []string{""}
		}
	case PsecSkill:
		msg = "Enter fills /command · Ctrl+S assigns Alt-shortcut · Esc closes"
		for _, c := range m.Cmds {
			if c.Source != "skill" {
				continue
			}
			opts = append(opts, "/"+c.Name)
			descs = append(descs, shortDesc(c.Description, 48)+m.shortcutSuffix(c.Name))
			payload = append(payload, c.Name)
		}
		if len(opts) == 0 {
			opts = []string{"— no skills —"}
			descs = []string{"/reload refreshes the catalog"}
			payload = []string{""}
		}
	case PsecPrompt:
		msg = "Enter fills /command · Ctrl+S assigns Alt-shortcut · Esc closes"
		for _, c := range m.Cmds {
			if c.Source != "prompt" {
				continue
			}
			opts = append(opts, "/"+c.Name)
			descs = append(descs, shortDesc(c.Description, 48)+m.shortcutSuffix(c.Name))
			payload = append(payload, c.Name)
		}
		if len(opts) == 0 {
			opts = []string{"— no prompts —"}
			descs = []string{"/reload refreshes the catalog"}
			payload = []string{""}
		}
	case PsecExt:
		msg = "Enter fills /command · Ctrl+S assigns Alt-shortcut · Esc closes"
		for _, c := range m.Cmds {
			if c.Source != "extension" {
				continue
			}
			d := shortDesc(c.Description, 40)
			if tag := extTag(c); tag != "" {
				d += " [" + tag + "]"
			}
			opts = append(opts, "/"+c.Name)
			descs = append(descs, d+m.shortcutSuffix(c.Name))
			payload = append(payload, c.Name)
		}
		if len(opts) == 0 {
			opts = []string{"— no extensions —"}
			descs = []string{"/reload refreshes the catalog"}
			payload = []string{""}
		}
	case PsecPlugin:
		msg = "★ suggested · Enter installs · Delete uninstalls · Ctrl+F suggests your own · Esc closes"
		// The star is a property of the package, not of the install state:
		// a suggested package keeps its ★ after being installed, so the
		// user can still see it is one of ours. Uninstalling drops the row
		// back to the ★ suggestion list below (same row, installable).
		sug := m.suggestions()
		isSug := map[string]bool{}
		for _, s := range sug {
			isSug[s.name] = true
		}
		for _, p := range m.Plugins {
			name, desc := p.Name, p.Spec
			if isSug[p.Name] {
				name, desc = "★ "+p.Name, "suggested · "+p.Spec
			}
			opts = append(opts, name)
			if m.plugBusySpec != "" && p.Spec == m.plugBusySpec {
				// the running op owns this row: spinner + live elapsed
				opts[len(opts)-1] = m.pluginBusyFrame() + " " + p.Name
				descs = append(descs, m.pluginBusyRowDesc())
				payload = append(payload, "")
				continue
			}
			descs = append(descs, desc)
			payload = append(payload, "")
		}
		// The suggested-but-not-installed ones: the installable ★ rows.
		// Payload is the marketplace one, so Enter reuses the single
		// pi install path (busy gate + confirm + npm spec).
		for _, s := range sug {
			if marketInstalled(m, s.name) {
				continue // already listed above, star and all
			}
			opts = append(opts, "★ "+s.name)
			descs = append(descs, "suggested · "+s.why+" · Enter installs")
			payload = append(payload, "market:"+s.name)
		}
		if m.plugBusyAction != "" {
			msg = m.pluginBusyLabel(m.plugBusyAction, m.plugBusySpec) +
				" · runs in the background"
		}
		if note := m.plugNoteLine(); note != "" {
			msg = warnStyle.Render(Fit(note, hubBoxW(m)-2)) // the gate prompt / result
		}
	case PsecMarket:
		// The filter is a real remote npm search here (not a local
		// filter), so the header names the query and the row count.
		if q := m.MarketQuery(); q != "" {
			msg = fmt.Sprintf("search %q · %d results · ↑↓ select · Enter installs · Esc clears", q, len(m.Market))
		} else {
			msg = fmt.Sprintf("%d pi packages · type to search npm · ↑↓ select · Enter installs · Esc closes",
				len(m.Market))
		}
		if m.plugBusyAction != "" {
			msg = m.pluginBusyLabel(m.plugBusyAction, m.plugBusySpec) +
				" · runs in the background"
		}
		if note := m.plugNoteLine(); note != "" {
			msg = warnStyle.Render(Fit(note, hubBoxW(m)-2))
		}
		if m.MarketErr != "" {
			opts = []string{"— market unavailable —"}
			descs = []string{Short(m.MarketErr, 60)}
			payload = []string{""}
		}
		for _, e := range m.Market {
			ver := e.Version
			if ver != "" && !strings.HasPrefix(ver, "v") {
				ver = "v" + ver
			}
			// stars are decoration: prefix only once resolved
			stars := ""
			if e.StarsKnown {
				stars = "★" + fmtStars(e.Stars) + " "
			}
			opts = append(opts, e.Name)
			if m.plugBusySpec != "" && m.plugBusySpec == "npm:"+e.Name {
				// the running op owns this row: spinner + live elapsed
				opts[len(opts)-1] = m.pluginBusyFrame() + " " + e.Name
				descs = append(descs, m.pluginBusyRowDesc())
				payload = append(payload, "")
				continue
			}
			if marketInstalled(m, e.Name) {
				descs = append(descs, "✓ installed · "+stars+shortDesc(ver+" "+e.Desc, 44))
				payload = append(payload, "")
			} else {
				descs = append(descs, stars+shortDesc(strings.TrimSpace(ver+" — "+e.Desc), 48))
				payload = append(payload, "market:"+e.Name)
			}
		}
		if len(opts) == 0 {
			// an empty list is two different things: a fetch in flight
			// (rows arrive in a moment) and a registry with nothing
			// for this query. Say which one instead of guessing.
			if marketInflight {
				opts = []string{"— loading plugins… —"}
				descs = []string{"npm is searching; rows appear as soon as it lands"}
			} else {
				opts = []string{"— empty market —"}
				descs = []string{"no pi packages match — Esc clears the search"}
			}
			payload = []string{""}
		} else if marketMore {
			// npm's "total" is a constant, so "more" is just the
			// registry having filled the last page.
			opts = append(opts, fmt.Sprintf("… load more (%d shown) …", len(m.Market)))
			descs = append(descs, "Enter loads the next 100")
			payload = append(payload, "marketmore")
		}
	case PsecMCP:
		// pi's /mcp, master-detail: the servers here, that server's
		// actions in the third column, navigable and runnable with →
		// and Enter. It used to be a second menu opened with Enter, and
		// before that the actions nested under every server — which
		// repeated the same six rows per server and read like a config
		// file. This is the one shape that neither repeats nor hides.
		opts, descs, payload, msg = m.mcpHubServersMenu(d)
	case PsecTool:
		msg = "Per-tool calls this session (from the transcript) · Ctrl+G expands tool output · Esc closes"
		for _, t := range toolStats(m) {
			opts = append(opts, t.name)
			descs = append(descs, t.desc)
			payload = append(payload, "")
		}
		if len(opts) == 0 {
			opts = []string{"— no tool calls yet —"}
			descs = []string{"tools appear here as the agent works"}
			payload = []string{""}
		}
	case PsecTasks:
		// Native replacement for /tasks → Settings: the extension's custom
		// panel can't cross RPC (pi stubs ui.custom), so the values cycle
		// here and persist to the project .pi/tasks-config.json.
		msg = "Enter cycles a value · project .pi/tasks-config.json · Esc closes"
		vals := loadTasksSettings(m.cwd, piAgentDir())
		for _, td := range taskSettings {
			opts = append(opts, td.label)
			descs = append(descs, vals[td.key]+" · Enter: next")
			payload = append(payload, "tasks:"+td.key)
		}
		// The widget lives in prefs.json, not tasks-config.json: it is a
		// pitago render decision, and reading it from a file on every tick
		// would be a read per repaint.
		widget := "off"
		if m.TaskWidgetVisible() {
			widget = "on"
		}
		opts = append(opts, "Show task widget above editor")
		descs = append(descs, widget+" · Enter: toggle · todos also show in the sidebar")
		payload = append(payload, "taskwidget")
	case PsecSide:
		msg = "Enter shows/hides a sidebar section · MCP + Plugins + Commands start hidden · Esc closes"
		for _, k := range sideOrder {
			state := "shown"
			if !m.SideVisible(k) {
				state = "hidden"
			}
			opts = append(opts, sideLabel(k))
			descs = append(descs, state+" · Enter: toggle")
			payload = append(payload, "side:"+k)
		}
	}
	return opts, descs, payload, msg
}

// pluginMeta is the cached package.json snapshot for one npm plugin spec.
type pluginMeta struct {
	version, desc string
	ok            bool
}

var pluginMetaCache = map[string]pluginMeta{}

// pluginInstallPath maps a package spec to its local dir: "npm:pi-lens" →
// <agentDir>/npm/node_modules/pi-lens (scoped names keep their @scope/
// path). Git specs have no stable local path — "" there.
func pluginInstallPath(spec string) string {
	name, ok := strings.CutPrefix(spec, "npm:")
	if !ok || strings.TrimSpace(name) == "" {
		return ""
	}
	if dir := piAgentDir(); dir != "" {
		return filepath.Join(dir, "npm", "node_modules", name)
	}
	return ""
}

// pluginMetaFor reads version/description from the installed package.json
// (cached per spec so the render path never hits the disk twice).
func pluginMetaFor(spec string) (pluginMeta, string) {
	if m, ok := pluginMetaCache[spec]; ok {
		return m, pluginInstallPath(spec)
	}
	m := pluginMeta{}
	path := pluginInstallPath(spec)
	if path != "" {
		if raw, err := os.ReadFile(filepath.Join(path, "package.json")); err == nil {
			var pkg struct {
				Version     string `json:"version"`
				Description string `json:"description"`
			}
			if json.Unmarshal(raw, &pkg) == nil && (pkg.Version != "" || pkg.Description != "") {
				m = pluginMeta{version: pkg.Version, desc: pkg.Description, ok: true}
			}
		}
	}
	pluginMetaCache[spec] = m
	return m, path
}

// pluginSource labels a spec by its installer prefix (npm:/git:/…).
func pluginSource(spec string) string {
	if i := strings.Index(spec, ":"); i >= 0 {
		return spec[:i]
	}
	return "—"
}

// pluginCommands lists the /commands contributed by one plugin spec
// (get_commands sourceInfo.source matches the settings.json spec).
func pluginCommands(m *Model, spec string) []pirpc.RepoCommand {
	var out []pirpc.RepoCommand
	for _, c := range m.Cmds {
		if c.SourceInfo != nil && c.SourceInfo.Source == spec {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// pluginDetailLines builds the highlighted plugin's detail column, each
// line exactly w cells wide: title + Spec · Source · Version · Description
// · Path · Commands (name + short desc, like the model picker's DETAILS
// pane). One dim placeholder when nothing is selected.
func pluginDetailLines(m *Model, d *Dialog, w int) []string {
	if w < 30 {
		w = 30
	}
	ri := -1
	if len(d.FIdx) > 0 && d.Cursor >= 0 && d.Cursor < len(d.FIdx) {
		ri = d.FIdx[d.Cursor]
	}
	if ri < 0 || ri >= len(m.Plugins) {
		return []string{"  " + toolStyle.Width(w-2).Render("— no selection —")}
	}
	p := m.Plugins[ri]
	var lines []string
	lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(cText).Render(Fit(p.Name, w-2)))
	meta, path := pluginMetaFor(p.Spec)
	rows := [][2]string{
		{"Spec", orDash(p.Spec)},
		{"Source", orDash(pluginSource(p.Spec))},
		{"Version", orDash(meta.version)},
		{"Path", orDash(path)},
	}
	const lw = 11
	valW := w - 2 - lw - 1
	if valW < 10 {
		valW = 10
	}
	for _, r := range rows {
		lab := toolStyle.Render(Fit(r[0], lw))
		style := lipgloss.NewStyle().Foreground(cText)
		if r[1] == "—" {
			style = statusBarStyle
		}
		lines = append(lines, "  "+lab+" "+style.Render(Fit(Short(r[1], valW), valW)))
	}
	// Description wraps onto its own lines (values above stay single-row
	// so the two-pane layout math holds).
	lines = append(lines, "  "+toolStyle.Render(Fit("Description", w-2)))
	if strings.TrimSpace(meta.desc) == "" {
		lines = append(lines, "  "+statusBarStyle.Render(Fit("—", w-2)))
	} else {
		for _, ln := range wrapWords(meta.desc, w-2) {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(cText).Render(Fit(ln, w-2)))
		}
	}
	cmds := pluginCommands(m, p.Spec)
	lines = append(lines, "  "+toolStyle.Render(Fit(fmt.Sprintf("Commands (%d)", len(cmds)), w-2)))
	if len(cmds) == 0 {
		lines = append(lines, "  "+statusBarStyle.Render(Fit("— none —", w-2)))
		return lines
	}
	for _, c := range cmds {
		row := "/" + c.Name
		if d := strings.Join(strings.Fields(c.Description), " "); d != "" {
			row += " — " + d
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(cText).Render(Fit(Short(row, w-2), w-2)))
	}
	return lines
}

// marketRow lays out one marketplace list row: the name, padded out, and
// the star chip right-aligned in the last cells. Exactly w display cells;
// the chip is dropped entirely when it does not fit (never a wrapped row).
func marketRow(name, chip string, w int) string {
	if w <= 0 {
		return ""
	}
	if chip == "" {
		return Fit(Short(name, w), w)
	}
	cw := lipgloss.Width(chip)
	if cw+1 >= w {
		return Fit(Short(name, w), w)
	}
	nw := w - cw - 1
	return Fit(Short(name, nw), nw) + " " + chip
}

// marketChip is the star chip for one row ("" while the count is unknown,
// so pending rows render exactly as before).
func marketChip(e MarketEntry) string {
	if !e.StarsKnown {
		return ""
	}
	return "★" + fmtStars(e.Stars)
}

// marketDetailLines builds the highlighted market entry's detail column:
// title + Version · Status · Description + the Enter hint. Same fixed
// width contract as pluginDetailLines.
func marketDetailLines(m *Model, d *Dialog, w int) []string {
	if w < 30 {
		w = 30
	}
	ri := -1
	if len(d.FIdx) > 0 && d.Cursor >= 0 && d.Cursor < len(d.FIdx) {
		ri = d.FIdx[d.Cursor]
	}
	if ri < 0 || ri >= len(m.Market) {
		return []string{"  " + toolStyle.Width(w-2).Render("— no selection —")}
	}
	e := m.Market[ri]
	installed := marketInstalled(m, e.Name)
	var lines []string
	title := e.Name
	if installed {
		title += " ✓"
	}
	lines = append(lines, "  "+lipgloss.NewStyle().Bold(true).Foreground(cText).Render(Fit(title, w-2)))
	status := "not installed"
	style := statusBarStyle
	if installed {
		status = "installed"
		style = okStyle
	}
	// stars: "—" without a repo or when the repo will never resolve,
	// "fetching…" only while the lookup is still genuinely unknown
	starVal := "—"
	switch {
	case e.Repo == "":
	case e.StarsKnown:
		starVal = marketChip(e)
	case marketStarsBad[e.Repo]:
		starVal = "—"
	default:
		starVal = "fetching…"
	}
	dlVal := "—"
	if e.Weekly > 0 {
		dlVal = fmtStars(e.Weekly) + "/wk"
	}
	rows := [][2]string{
		{"Version", orDash(e.Version)},
		{"Spec", "npm:" + e.Name},
		{"Stars", starVal},
		{"Downloads", dlVal},
	}
	const lw = 11
	valW := w - 2 - lw - 1
	if valW < 10 {
		valW = 10
	}
	for _, r := range rows {
		lab := toolStyle.Render(Fit(r[0], lw))
		st := lipgloss.NewStyle().Foreground(cText)
		if r[1] == "—" {
			st = statusBarStyle
		}
		lines = append(lines, "  "+lab+" "+st.Render(Fit(Short(r[1], valW), valW)))
	}
	// while this entry is the one being installed/removed the Status row
	// is the live spinner, not a static installed/not-installed word
	if m.plugBusyAction != "" && m.plugBusySpec == "npm:"+e.Name {
		lines = append(lines, "  "+toolStyle.Render(Fit("Status", lw))+" "+
			warnStyle.Render(Fit(m.pluginBusyFrame()+" "+m.pluginBusyRowDesc(), valW)))
	} else {
		lines = append(lines, "  "+toolStyle.Render(Fit("Status", lw))+" "+style.Render(Fit(status, valW)))
	}
	// Repository: the GitHub URL the star count came from, wrapped on
	// segment boundaries (the value column is too narrow for a full URL).
	if e.Repo != "" {
		lines = append(lines, "  "+toolStyle.Render(Fit("Repository", lw)))
		for _, ln := range wrapURL("https://github.com/"+e.Repo, w-2) {
			lines = append(lines, "  "+codeStyle.Render(Fit(ln, w-2)))
		}
	}
	lines = append(lines, "  "+toolStyle.Render(Fit("Description", w-2)))
	if strings.TrimSpace(e.Desc) == "" {
		lines = append(lines, "  "+statusBarStyle.Render(Fit("—", w-2)))
	} else {
		for _, ln := range wrapWords(e.Desc, w-2) {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(cText).Render(Fit(ln, w-2)))
		}
	}
	hint := "Enter: install via pi install"
	if installed {
		hint = "already installed"
	}
	lines = append(lines, "  "+toolStyle.Render(Fit(hint, w-2)))
	return lines
}

// wrapURL folds a URL into lines of at most n cells, breaking only after
// "/" so a long path splits on segment boundaries and every line still
// reads as the same link. Plain strings only — style after.
func wrapURL(s string, n int) []string {
	if n < 10 {
		n = 10
	}
	var out []string
	cur := ""
	// keep each segment with its trailing slash: "https://",
	// "github.com/", "owner/", "repo"
	for _, seg := range splitAfter(s, '/') {
		if cur == "" {
			cur = seg
			continue
		}
		if lipgloss.Width(cur+seg) > n {
			out = append(out, cur)
			cur = seg
			continue
		}
		cur += seg
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// splitAfter cuts s after every sep, so the separators stay attached to the
// piece before them.
func splitAfter(s string, sep rune) []string {
	var out []string
	start := 0
	for i, r := range s {
		if r == sep {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// wrapWords folds s into lines of at most n display cells (word-boundary,
// hard-splits one overlong word). Plain strings only — style after.
func wrapWords(s string, n int) []string {
	if n < 10 {
		n = 10
	}
	var out []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
			curW = 0
		}
	}
	for _, word := range strings.Fields(s) {
		ww := lipgloss.Width(word)
		if ww > n {
			// fallback: rune-slice the long word into n-wide pieces
			runes := []rune(word)
			for len(runes) > 0 {
				acc, accW := 0, 0
				for acc < len(runes) && accW+lipgloss.Width(string(runes[acc])) <= n {
					accW += lipgloss.Width(string(runes[acc]))
					acc++
				}
				if acc == 0 {
					acc = 1
				}
				out = append(out, string(runes[:acc]))
				runes = runes[acc:]
			}
			continue
		}
		add := ww
		if curW > 0 {
			add++ // space
		}
		if curW+add > n {
			flush()
		}
		if curW > 0 {
			cur.WriteByte(' ')
			curW++
		}
		cur.WriteString(word)
		curW += ww
	}
	flush()
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// psecCursor resolves the highlighted right-pane row to its option index
// (-1 when the list is empty or the cursor is out of range).
func psecCursor(d *Dialog) int {
	if len(d.FIdx) == 0 || d.Cursor < 0 || d.Cursor >= len(d.FIdx) {
		return -1
	}
	return d.FIdx[d.Cursor]
}

// payloadOf parallels DescOf for the right pane's per-row payload.
func payloadOf(d *Dialog, ri int) string {
	if ri < len(d.Payload) {
		return d.Payload[ri]
	}
	return ""
}

// sideStateStyle is the sidebar state word's color: shown renders bright,
// hidden stays dark like the hint.
func sideStateStyle(state string) lipgloss.Style {
	if state == "shown" {
		return lipgloss.NewStyle().Foreground(cText)
	}
	return toolStyle
}

// psecDesc renders one right-pane description, truncated to maxW. Sidebar
// rows lead with their state word (shown bright, hidden dark). Styling
// happens after truncation so no ANSI sequence can be cut in half
// (Fit/Short require unstyled input).
func psecDesc(payload, desc string, maxW int) string {
	desc = Short(desc, maxW)
	if !strings.HasPrefix(payload, "side:") {
		return toolStyle.Render("— " + desc)
	}
	state, rest := desc, ""
	if i := strings.Index(desc, " "); i >= 0 {
		state, rest = desc[:i], desc[i:]
	}
	return toolStyle.Render("— ") + sideStateStyle(state).Render(state) + toolStyle.Render(rest)
}

// shortcutSuffix renders the assigned Alt-shortcut for a /command row
// (" · ⌥X", "" when none).
func (m *Model) shortcutSuffix(cmd string) string {
	if l := m.shortcutForCmd(cmd); l != "" {
		return " · " + shortcutDisplay(l)
	}
	return ""
}

// shortcutTarget resolves the highlighted right-pane row to its assignable
// /command (command/skill/prompt/extension rows, and the custom Alt rows
// of the Shortcuts section; "" otherwise).
func shortcutTarget(d *Dialog, ri int) string {
	switch d.CurPsec() {
	case PsecCmd, PsecSkill, PsecPrompt, PsecExt, PsecKeys:
	default:
		return ""
	}
	return payloadOf(d, ri)
}

// extTag shortens an extension source ("npm:pi-subagents" → "pi-subagents")
// for the row suffix.
func extTag(c pirpc.RepoCommand) string {
	if c.SourceInfo == nil {
		return ""
	}
	s := strings.TrimSpace(c.SourceInfo.Source)
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i:], "/") {
		return s[i+1:]
	}
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func shortDesc(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// toolStat is one per-tool aggregate for the Tools section.
type toolStat struct {
	name string
	desc string
}

// toolStats aggregates the transcript's tool blocks by name (done/running/
// error split). Reads m.blocks directly — same source the chat renders.
func toolStats(m *Model) []toolStat {
	type agg struct {
		n, done, run, err int
		lastArgs          string
	}
	byName := map[string]*agg{}
	var order []string
	for _, b := range m.blocks {
		if b.Kind != "tool" || b.ToolName == "" {
			continue
		}
		a, ok := byName[b.ToolName]
		if !ok {
			a = &agg{}
			byName[b.ToolName] = a
			order = append(order, b.ToolName)
		}
		a.n++
		switch b.ToolStatus {
		case "done":
			a.done++
		case "error":
			a.err++
		default:
			a.run++
		}
		if b.ToolArgs != "" {
			a.lastArgs = b.ToolArgs
		}
	}
	sort.Strings(order)
	out := make([]toolStat, 0, len(order))
	for _, n := range order {
		a := byName[n]
		var parts []string
		parts = append(parts, fmt.Sprintf("%dx", a.n))
		if a.done > 0 {
			parts = append(parts, fmt.Sprintf("%d done", a.done))
		}
		if a.run > 0 {
			parts = append(parts, fmt.Sprintf("%d running", a.run))
		}
		if a.err > 0 {
			parts = append(parts, fmt.Sprintf("%d error", a.err))
		}
		d := strings.Join(parts, " · ")
		if a.lastArgs != "" {
			d += " — " + shortDesc(a.lastArgs, 32)
		}
		out = append(out, toolStat{name: n, desc: d})
	}
	return out
}

// FillCommand closes every dialog and stages a slash command in the input
// (palette parity: user reviews, then Enter sends).
func (m *Model) FillCommand(name string) {
	m.Dialogs = nil
	m.ta.SetValue("/" + name + " ")
	m.refreshPiTasks()
	m.refreshCmds()
	m.refreshAt()
	m.Refresh()
}

// isFilterKind reports pickers whose typing filters the list (generic
// updateDialog path).
func isFilterKind(kind string) bool {
	switch kind {
	case "model", "thinking", "sessions", "login", "logout", "trajectory", "tree", "settings", "subagents", "notification", "fork", "pet", "mcp", suggestAddKind:
		return true
	}
	return false
}

// updatePconfigDialog navigates the two-pane hub: ↑↓ moves in the focused
// pane (moving sections reloads the right pane), ←/→/Tab switches pane,
// typing filters the right pane, Enter on the left focuses the right,
// Enter on the right runs (confirm lives in src/builtin).
func (m Model) updatePconfigDialog(km tea.KeyMsg, d *Dialog) (tea.Model, tea.Cmd) {
	mm, cmd := m.updatePconfigDialogKey(km, d)
	// Sitting on the MCP section asks pi for its list, and the request
	// RIDES ALONG with the key's own result: it must never be returned
	// in place of it, or a stale list would eat every keystroke (pi's
	// list has a 90s timeout, so the pane would read as dead).
	if cmd == nil {
		cmd = m.mcpListIfStale(d)
	}
	return mm, cmd
}

// updatePconfigDialogKey handles one key in the two-pane hub.
func (m Model) updatePconfigDialogKey(km tea.KeyMsg, d *Dialog) (tea.Model, tea.Cmd) {
	// Before anything reads the actions column: the keyboard must not be
	// able to reach a column the terminal is too narrow to draw.
	m.syncMcpActDrawn(d)
	// While pane 3 is the entry editor, it owns the keyboard: every key is
	// text or a field action. Handing any of it to the hub would navigate
	// the panes under the form, which is exactly the "different UI" trap.
	if d.Kind == "pconfig" && d.CurPsec() == PsecMCP && d.McpEdit.Active {
		return m.updateMcpEditHub(km, d)
	}
	switch km.Type {
	case tea.KeyUp, tea.KeyDown:
		down := km.Type == tea.KeyDown
		if d.ProvFocus {
			if n := len(d.Provs); n > 0 {
				if down {
					d.ProvCursor = (d.ProvCursor + 1) % n
				} else {
					d.ProvCursor = (d.ProvCursor - 1 + n) % n
				}
				m.loadRows(d)
				// First visit to an unloaded marketplace fetches it in
				// the background (rows reload when MarketMsg lands).
				if d.CurPsec() == PsecMarket {
					// first visit: fetch a stale page, then let the
					// browse head order itself by stars. The sort is
					// built first so it claims the head before the
					// window hydration latches those repos busy.
					sortCmd := m.marketSortCmd("")
					hyd := m.hydrateMarketStarsCmd(d)
					if !marketFresh() && !marketInflight {
						return m, tea.Batch(m.fetchMarketCmd(), sortCmd, hyd)
					}
					return m, tea.Batch(sortCmd, hyd)
				}
			}
		} else if d.CurPsec() == PsecMCP && d.McpActFocus {
			if n := len(d.McpAct); n > 0 {
				if down {
					d.McpActCursor = (d.McpActCursor + 1) % n
				} else {
					d.McpActCursor = (d.McpActCursor - 1 + n) % n
				}
				m.clearMcpArm() // arrowing away from a half-armed remove
				m.Refresh()
			}
		} else if n := len(d.FIdx); n > 0 {
			if down {
				d.Cursor = (d.Cursor + 1) % n
			} else {
				d.Cursor = (d.Cursor - 1 + n) % n
			}
			// Theme rows are the one hub section that previews while
			// browsing, so ↑↓ repaints in the new palette.
			m.previewTheme(d)
			if d.Kind == "pconfig" && d.CurPsec() == PsecMCP {
				// The actions column follows the highlighted server, so
				// arrowing re-targets it (to its first action, which is
				// what pi pre-selects). The server itself is restored by
				// NAME across the rebuild: LoadPsecRows resets the cursor,
				// and pi's list can reorder under us as servers connect.
				m.clearMcpArm() // arrowing away from a half-armed remove
				on := mcpTargetOf(payloadOf(d, d.Cursor))
				d.McpActCursor, d.McpActRun = 0, false
				m.loadRows(d)
				for f, ri := range d.FIdx {
					if payloadOf(d, ri) == psecActMCPSel+on {
						d.Cursor = f
						break
					}
				}
				m.refreshMcpActions(d) // now that the cursor is back
			}
			// The marketplace hydrates GitHub stars for whatever the
			// moved cursor brought into view (never from render).
			if d.CurPsec() == PsecMarket {
				return m, m.hydrateMarketStarsCmd(d)
			}
		}
		return m, nil
	case tea.KeyLeft:
		// ← steps out of the actions column, then out of the servers,
		// then to the sections — the order the panes sit in.
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP && !d.ProvFocus && d.McpActFocus {
			d.McpActFocus, d.McpActRun = false, false
			m.Refresh()
			return m, nil
		}
		d.ProvFocus = true
		return m, nil
	case tea.KeyRight:
		// → steps into the actions column when the MCP section has some.
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP && !d.ProvFocus && len(d.McpAct) > 0 &&
			m.mcpActColumnDrawn(d) {
			d.McpActFocus, d.McpActRun = true, false
			m.Refresh()
			return m, nil
		}
		d.ProvFocus = false
		return m, nil
	case tea.KeyTab:
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP && !d.ProvFocus {
			// A resize can leave the focus parked on a column that is no
			// longer drawn, so it is re-checked here rather than trusted.
			d.McpActFocus = !d.McpActFocus && m.mcpActColumnDrawn(d)
			d.McpActRun = false
			return m, nil
		}
		d.ProvFocus = !d.ProvFocus
		return m, nil
	case tea.KeyCtrlF:
		// Suggest your own plugin (Plugins tab only): a free-text prompt
		// over the hub, persisted to prefs.json when it submits.
		if d.CurPsec() == PsecPlugin {
			m.openSuggestAdd()
			return m, nil
		}
		return m, nil
	case tea.KeyCtrlS:
		// Assign an Alt-shortcut to the highlighted /command (hub stays
		// underneath the capture dialog; rows reload on close).
		if !d.ProvFocus {
			if ri := psecCursor(d); ri >= 0 {
				if cmd := shortcutTarget(d, ri); cmd != "" {
					m.openShortcutCapture(cmd)
					return m, nil
				}
			}
		}
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if km.Type == tea.KeyBackspace && d.Filter != "" {
			d.Filter = d.Filter[:len(d.Filter)-1]
			d.Reindex()
			if d.Kind == mcpKind {
				m.applyMcpFilter(d) // the filter narrows the SERVER list here
				return m, nil
			}
			if d.Kind == "pconfig" && d.CurPsec() == PsecMCP {
				// Widening the filter has to rebuild the rows for the same
				// reason typing does, or backspace leaves the narrowed view
				// on screen with a shorter filter in the box.
				m.clearMcpArm()
				d.McpActCursor = 0
				m.loadRows(d)
				return m, nil
			}
			// marketplace typing is a remote search: re-arm the debounce
			return m, m.marketSearchTick()
		}
		// Empty filter (forward-delete always): Delete removes the
		// highlighted plugin (sessions/login parity: ⌫ on empty
		// filter deletes). Other sections ignore it.
		if d.CurPsec() == PsecPlugin && !d.ProvFocus {
			if ri := psecCursor(d); ri >= 0 && ri < len(m.Plugins) {
				spec := m.Plugins[ri].Spec
				// one plugin op at a time: pi remove is not safe to
				// double-fire while an install/uninstall is running
				if _, busy := m.PluginBusy(); busy != "" {
					// names the op that is running, not the one refused
					m.Status = m.PluginBusyMsg()
					m.Refresh()
					return m, nil
				}
				if !m.ConfirmPluginOp("remove", spec) {
					return m, nil // first Delete arms the auth gate
				}
				return m, m.StartPluginOp("remove", spec)
			}
		}
		return m, nil
	case tea.KeyEsc:
		// Focus leaves the third column before it leaves the hub.
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP && d.McpActFocus {
			d.McpActFocus, d.McpActRun = false, false
			m.Refresh()
			return m, nil
		}
		// Esc in the marketplace clears the search first (one level,
		// like every other filter dialog); a second Esc closes.
		if d.CurPsec() == PsecMarket && d.Filter != "" {
			d.Filter = ""
			d.Reindex()
			m.MarketErr = ""
			if entries, more, fresh := marketCached(""); fresh {
				m.Market, marketMore = entries, more
				m.Status = "ready"
				m.refreshHubSection(PsecMarket)
				m.Refresh()
				return m, tea.Batch(m.marketSortCmd(""), m.hydrateMarketStarsCmd(d))
			}
			m.Refresh()
			return m, tea.Batch(m.fetchMarketCmd(), m.marketSortCmd(""), m.hydrateMarketStarsCmd(d))
		}
		m.Dialogs = m.Dialogs[1:]
		// The remove gate is shared by the hub and the editor, so
		// leaving a surface must drop it: arming a delete and walking
		// away must not turn the next surface's first Enter into it.
		m.clearMcpArm()
		m.refreshPiTasks()
		// The hub (or panel) underneath shows rows built at ITS open; a
		// change made here must not leave it describing the old state.
		m.refreshHubUnderneath()
		m.Refresh()
		return m, m.ReconcileTurnCmd()
	case tea.KeyEnter:
		if d.ProvFocus {
			d.ProvFocus = false
			return m, nil
		}
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP && len(d.McpAct) > 0 &&
			m.mcpActColumnDrawn(d) {
			if !d.McpActFocus {
				// Same move pi makes with Enter (open the server's
				// actions), except the actions are already on screen: it
				// just takes the focus.
				d.McpActFocus, d.McpActCursor = true, 0
				m.Refresh()
				return m, nil
			}
			// Focus is on an action: Enter RUNS it. The flag is what tells
			// the confirm handler to read the column rather than the row
			// under the cursor; without the dispatch below it was set and
			// nothing ever ran it.
			d.McpActRun = true
			return m.confirmDialog(d)
		}
		return m.confirmDialog(d)
	}
	if km.Type == tea.KeyRunes {
		d.Filter += km.String()
		d.Reindex()
		if d.Kind == mcpKind {
			// Typing a server name is the point of this panel: the filter
			// narrows the left pane, not just the action rows.
			m.applyMcpFilter(d)
		}
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP {
			// Typing searches the servers (there are few and they are the
			// thing you are looking for); the actions column belongs to
			// one server and has nothing to search.
			m.clearMcpArm()
			d.McpActCursor = 0
			m.loadRows(d)
			return m, nil
		}
		// marketplace typing debounces into a remote npm search
		return m, m.marketSearchTick()
	}
	return m, nil
}

// updatePconfigWheel scrolls the focused pane (wheel down = next row):
// sections when the left pane has focus (moving reloads the right pane,
// like ↑↓), contents otherwise. Lets mouse users scroll the hub without
// touching the chat/sidebar behind it.
func (m Model) updatePconfigWheel(d *Dialog, down bool) (tea.Model, tea.Cmd) {
	t := tea.KeyDown
	if !down {
		t = tea.KeyUp
	}
	return m.updatePconfigDialog(tea.KeyMsg{Type: t}, d)
}

// renderPconfigDialog draws the two-pane hub: left = sections with counts,
// right = the selected section's rows. The Plugins section adds a third
// DETAILS column on wide terminals (like the /model picker). Layout math
// mirrors the /model picker (fixed scroll windows so the box never
// resizes while scrolling).
func (m Model) renderPconfigDialog(d *Dialog) string {
	m.syncMcpActDrawn(d)
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(d.Title) + "\n")
	// The MCP section's hint is derived, not read from d.Message: the
	// message is cached at row-build time and a resize does not rebuild it,
	// so a cached hint would still promise a column that just disappeared.
	msg := d.Message
	if d.Kind == "pconfig" && d.CurPsec() == PsecMCP {
		msg = m.mcpHubSectionHint(d, d.McpBaseMsg)
	}
	if msg != "" {
		b.WriteString(statusBarStyle.Render(msg) + "\n")
	}
	// the marketplace filter is a remote npm search, not a local filter
	filterLabel := "filter: "
	if d.CurPsec() == PsecMarket {
		filterLabel = "search: "
	}
	b.WriteString(statusBarStyle.Render(filterLabel+d.Filter+"▌") + "\n")
	b.WriteString("\n")

	boxW := hubBoxW(&m)
	leftW := 30
	if boxW < 100 {
		leftW = 24
	}
	rightW := boxW - 8 - leftW - 3
	if rightW < 30 {
		rightW = 30
	}
	// Plugins and Marketplace get a third DETAILS column (oh-my-pi
	// style, like the /model picker): list + specs side by side, and the
	// MCP editor panel brings its own. The hub's MCP SECTION does not
	// take one: pi's row already carries state · exposure · scope, and a
	// third column squeezes that out of the row. Narrow terminals keep
	// the classic two panes (the spec lives in the row desc).
	detW := 42
	// The hub's MCP section uses the third column for the highlighted
	// server's ACTIONS, like Plugins use theirs for a spec — and unlike a
	// spec block, they are navigable (→/←) and Enter runs them, so pi's
	// second menu does not have to open at all.
	isMarket := d.CurPsec() == PsecMarket && len(m.Market) > 0
	isDetail := boxW >= mcpHubDetailW && (d.Kind == mcpKind || d.CurPsec() == PsecMCP || (d.CurPsec() == PsecPlugin && len(m.Plugins) > 0) ||
		isMarket)
	// The hub's MCP section's middle pane is names only (the details are in
	// the third column), so its room goes to that column instead.
	if d.Kind == "pconfig" && d.CurPsec() == PsecMCP {
		detW = 46
	}
	listW := rightW
	if isDetail {
		listW = rightW - detW - 3
		if listW < 20 {
			listW = 20
			detW = rightW - listW - 3
		}
	}
	win := hubWindow(&m)

	// left window (sections): label + count badge.
	ptotal := len(d.Provs)
	pstart, pend, pAbove, pBelow := fixedWin(d.ProvCursor, ptotal, win)
	var leftLines []string
	if pAbove {
		leftLines = append(leftLines, "  "+toolStyle.Width(leftW-2).Render(fmt.Sprintf("…(+%d above)", pstart)))
	}
	for pi := pstart; pi < pend; pi++ {
		cw := leftW - 2
		nm := Short(d.Provs[pi], cw-5)
		cnt := ""
		// Count badge is a hub-section idea (how many rows live in that
		// section). The MCP panel's left pane is a list of servers, and
		// a count there reads as noise next to a status dot.
		if pi < len(d.PsecIDs) && d.Kind != mcpKind {
			if n := psecCount(&m, d.PsecIDs[pi]); n >= 0 {
				cnt = fmt.Sprintf("%d", n)
			}
		}
		pad := cw - 2 - lipgloss.Width(nm) - len(cnt)
		if pad < 1 {
			pad = 1
		}
		content := Fit(nm+strings.Repeat(" ", pad)+cnt, cw-2)
		mark := "  "
		style := statusBarStyle
		if pi == d.ProvCursor {
			mark = "▸ "
			if d.ProvFocus {
				style = rowHiStyle
			} else {
				style = lipgloss.NewStyle().Foreground(cText)
			}
		}
		leftLines = append(leftLines, mark+style.Width(leftW-2).Render(content))
	}
	if pBelow {
		leftLines = append(leftLines, "  "+toolStyle.Width(leftW-2).Render(fmt.Sprintf("…(+%d below)", ptotal-pend)))
	}
	for len(leftLines) < win {
		leftLines = append(leftLines, "  "+statusBarStyle.Width(leftW-2).Render(""))
	}

	// right window (section rows): same fixed-win rule as the left pane.
	// In plugin detail mode the middle column is name-only — the spec
	// lives in the DETAILS column (like the /model picker's wide layout).
	total := len(d.FIdx)
	start, end, rAbove, rBelow := fixedWin(d.Cursor, total, win)
	var rightLines []string
	if rAbove {
		rightLines = append(rightLines, "  "+toolStyle.Width(listW-2).Render(fmt.Sprintf("…(+%d above)", start)))
	}
	optW := 28
	if listW-10 < optW {
		optW = listW - 10
	}
	if optW < 10 {
		optW = 10
	}
	if isDetail {
		optW = listW - 4
		if optW < 10 {
			optW = 10
		}
	}

	for fi := start; fi < end; fi++ {
		ri := d.FIdx[fi]
		mark := "  "
		style := statusBarStyle
		if fi == d.Cursor {
			mark = "▸ "
			if d.ProvFocus {
				style = lipgloss.NewStyle().Foreground(cText)
			} else {
				style = rowHiStyle
			}
		}
		row := Fit(Short(d.Options[ri], optW), optW)
		switch {
		case !isDetail:
			// The desc column, and only where there is no third column to
			// carry the detail instead. `isDetail` already covers the hub's
			// MCP section and the MCP editor panel, so it needs no clause
			// of its own here: narrow, every row gets its desc back — which
			// is the point, because below mcpHubDetailW there is nowhere
			// else to put the state · exposure · scope.
			desc := DescOf(d, ri)
			// The armed-remove prompt rides the Remove row here too, for
			// the same reason as in the hub: the instruction belongs on
			// the action it belongs to. Same predicate as the gate, window
			// included, so it cannot promise a second Enter that re-arms.
			if d.Kind == mcpKind && strings.Contains(payloadOf(d, ri), "mcp:remove") &&
				m.McpRemoveArmed(d.McpSelected()) {
				desc = "press Enter again · any other key cancels"
			}
			if desc != "" {
				row += "  " + psecDesc(payloadOf(d, ri), desc, listW-4-optW-3)
			}
		case isDetail && isMarket:
			// The wide layout drops the desc column, so the star count
			// rides the row itself ("" while still unknown).
			chip := ""
			if ri >= 0 && ri < len(m.Market) {
				chip = marketChip(m.Market[ri])
			}
			row = marketRow(d.Options[ri], chip, optW)
		}
		rightLines = append(rightLines, mark+style.Width(listW-2).Render(row))
	}
	if rBelow {
		rightLines = append(rightLines, "  "+toolStyle.Width(listW-2).Render(fmt.Sprintf("…(+%d below)", total-end)))
	}
	if total == 0 {
		rightLines = append(rightLines, "  "+toolStyle.Width(listW-2).Render("— no match —"))
	}
	for len(rightLines) < win {
		rightLines = append(rightLines, "  "+statusBarStyle.Width(listW-2).Render(""))
	}

	leftHead, midHead := d.twoPaneHeads()
	sep := sepStyle.Render("│")
	if !isDetail {
		b.WriteString("  " + sideTitleStyle.Width(leftW-2).Render(leftHead) + " │ " +
			"  " + sideTitleStyle.Width(listW-2).Render(strings.ToUpper(midHead)+" · "+fmt.Sprintf("%d", total)) + "\n")

		n := len(leftLines)
		if len(rightLines) > n {
			n = len(rightLines)
		}
		for i := 0; i < n; i++ {
			l, r := "", ""
			if i < len(leftLines) {
				l = leftLines[i]
			} else {
				l = "  " + statusBarStyle.Width(leftW-2).Render("")
			}
			if i < len(rightLines) {
				r = rightLines[i]
			} else {
				r = "  " + statusBarStyle.Width(listW-2).Render("")
			}
			b.WriteString(l + " " + sep + " " + r + "\n")
		}
	} else {
		b.WriteString("  " + sideTitleStyle.Width(leftW-2).Render(leftHead) + " │ " +
			"  " + sideTitleStyle.Width(listW-2).Render(strings.ToUpper(midHead)+" · "+fmt.Sprintf("%d", total)) + " │ " +
			"  " + sideTitleStyle.Width(detW-2).Render(m.detailHead(d)) + "\n")

		// Fixed box height: the detail column never stretches the
		// dialog — overflow folds into a "…(+N more)" marker, like the
		// scroll markers of the other two panes.
		var detLines []string
		if d.Kind == "pconfig" && d.CurPsec() == PsecMCP {
			detLines = m.mcpHubPane(d, detW)
		} else {
			detLines = m.detailLines(d, detW)
		}
		if len(detLines) > win {
			detLines = append(detLines[:win-1],
				"  "+toolStyle.Width(detW-2).Render(fmt.Sprintf("…(+%d more)", len(detLines)-win+1)))
		}
		for len(detLines) < win {
			detLines = append(detLines, "  "+statusBarStyle.Width(detW-2).Render(""))
		}
		n := len(leftLines)
		if len(rightLines) > n {
			n = len(rightLines)
		}
		if len(detLines) > n {
			n = len(detLines)
		}
		for i := 0; i < n; i++ {
			l, r, dt := "", "", ""
			if i < len(leftLines) {
				l = leftLines[i]
			} else {
				l = "  " + statusBarStyle.Width(leftW-2).Render("")
			}
			if i < len(rightLines) {
				r = rightLines[i]
			} else {
				r = "  " + statusBarStyle.Width(listW-2).Render("")
			}
			if i < len(detLines) {
				dt = detLines[i]
			} else {
				dt = "  " + statusBarStyle.Width(detW-2).Render("")
			}
			b.WriteString(l + " " + sep + " " + r + " " + sep + " " + dt + "\n")
		}
	}

	foot := "↑↓ sections · → contents · Enter open · Esc close"
	if !d.ProvFocus {
		foot = "↑↓ select · ← sections · Tab switch · type filters · Enter run · Esc close"
		if d.CurPsec() == PsecMarket {
			// the filter is a remote npm search, not a local one
			foot = "↑↓ select · ← sections · Tab switch · type to search npm · Enter install · Esc clears"
		}
		if d.CurPsec() == PsecMCP {
			// Enter on a server takes focus on its actions; the second one
			// runs the highlighted action. Saying only "Enter run" sent
			// people looking for an action that had not been reached yet.
			if m.mcpActColumnDrawn(d) {
				foot = "↑↓ select · → actions · ← sections · type filters · Enter focus · then Enter runs · Esc close"
			} else {
				// Tab is intercepted on this section, so promising it here
				// would be a key that does nothing at all.
				foot = "↑↓ select · ← sections · type filters · Enter open · Esc close"
			}
		}
	}
	if d.Kind == mcpKind {
		foot = "↑↓ server · → actions · Tab switch · type filters · Enter run · Esc close"
	}
	b.WriteString("\n" + toolStyle.Render(foot))
	box := dlgStyle.Width(boxW).Render(b.String())
	hint := ""
	if len(m.Dialogs) > 1 {
		hint = statusBarStyle.Render(fmt.Sprintf("(%d more dialogs pending)", len(m.Dialogs)-1))
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center, box),
		hint,
	)
}

// --- the hub's MCP section -------------------------------------------

// mcpScopeName is where pi found the server (its "scope" column).
func mcpScopeName(s pirpc.McpServerInfo) string {
	if s.Scope != "" {
		return s.Scope
	}
	return s.Source // an extension-registered server has no scope
}

// mcpExposureName is pi's exposure, defaulting to codemode.
func mcpExposureName(s pirpc.McpServerInfo) string {
	if s.Exposure == "" {
		return "codemode"
	}
	return s.Exposure
}

// mcpFileStateLine describes a server from the mcp.json snapshot alone,
// used before pi has ever been listed.
func mcpFileStateLine(s McpServer) string {
	switch {
	case s.Disabled:
		return "disabled in mcp.json · state unknown until listed"
	case s.Connected:
		return fmt.Sprintf("● %d/%d direct · ~%s tok", s.Direct, s.Total, fmtComma(s.Tokens))
	default:
		return "○ not connected · state unknown until listed"
	}
}

// mcpHubNames is the server list the section shows: pi's list when it has
// been read (its own order, needs-attention first), otherwise the
// mcp.json snapshot, so the section is never blank before the first list.
func (m Model) mcpHubNames() []string {
	if len(m.McpInfo) > 0 {
		out := make([]string, 0, len(m.McpInfo))
		for _, s := range m.McpInfo {
			out = append(out, s.Name)
		}
		return out
	}
	out := make([]string, 0, len(m.MCP))
	for _, s := range m.MCP {
		out = append(out, s.Name)
	}
	return out
}

// mcpHubFileServer is the server's entry in the mcp.json snapshot.
func (m Model) mcpHubFileServer(name string) (McpServer, bool) {
	for _, s := range m.MCP {
		if s.Name == name {
			return s, true
		}
	}
	return McpServer{}, false
}

// mcpHubActionRows builds one server's action rows, in pi's order and pi's
// wording. The action KIND is passed explicitly, never derived from the
// label: deriving it turned "Edit server…" into the payload
// "@mcpact:editserver…@alpha", which matched no dispatch case — so the row
// did nothing at all.
func mcpHubActionRows(m *Model, sel string, opts, descs, payload []string) ([]string, []string, []string) {
	add := func(kind, label, desc string) {
		opts = append(opts, label)
		descs = append(descs, desc)
		payload = append(payload, psecActMCPAct+kind+"@"+sel)
	}
	srv, known := m.McpInfoFor(sel)
	if !known {
		// pi has not listed this server (or not at all): re-reading the
		// list is the only thing that can be offered.
		add("reload", "Reload pi MCPs", sel+" · re-reads mcp.json after an edit")
		return opts, descs, payload
	}
	// A server pi found in another file (a repo-local project mcp.json, or
	// one an extension registered) still gets pi's actions — those are not
	// file-scoped. Only the ENTRY edit is, and it names that file: an edit
	// that resolved the agent dir instead would open a different server, or
	// create a second one under the same name.
	elsewhere := srv.Scope != "" && srv.Scope != "global" && srv.Source != ""
	if !srv.Enabled {
		add("enable", "Enable", sel+" · "+mcpScopeFile(srv))
	} else {
		if srv.State == "needs-auth" {
			add("signin", "Sign in", sel+" · opens the browser")
		}
		if srv.State == "connected" {
			add("tools", "Tools", sel+" · "+fmt.Sprintf("%d offered", len(srv.Tools)))
		}
		switch srv.State {
		case "failed", "disconnected", "connected", "needs-auth":
			add("reconnect", "Reconnect", sel+" · listing is what connects")
		}
		// pi offers Sign out when the connection carries an oauthUrl. The
		// CLI list has no such flag, so its signal is the URL transport:
		// a stdio server can hold no stored credentials.
		if srv.State == "connected" && srv.IsHTTP() {
			add("signout", "Sign out", sel+" · deletes the stored credentials")
		}
		add("exposure", "Exposure", sel+" · "+mcpExposureName(srv))
		add("disable", "Disable", sel+" · "+mcpScopeFile(srv))
	}
	// The entry itself: pi can add and remove from the CLI, but pitago
	// never surfaced it, and there is no verb for editing args or env.
	editDesc := "command · args · env · transport · headers"
	if elsewhere {
		editDesc = srv.Source
	}
	add("edit", "Edit server…", sel+" · "+editDesc)
	if !elsewhere {
		add("remove", "Remove server…", sel+" · delete the entry (.bak kept)")
	}
	return opts, descs, payload
}

// mcpSourceName is the file pi read the entry from (pi's details block
// prints scope: source). Falls back to the scope when the CLI omitted it.
func mcpSourceName(s pirpc.McpServerInfo) string {
	if s.Source != "" {
		return s.Source
	}
	return s.Scope
}

// mcpScopeLabel is pi's `entry.scope ?? "config"`.
func mcpScopeLabel(s pirpc.McpServerInfo) string {
	if s.Scope == "" {
		return "config"
	}
	return s.Scope
}

// mcpServerError is pi's error block: the server's own message, plus the
// connection error when it is not connected.
func mcpServerError(s pirpc.McpServerInfo) string {
	if s.State == "connected" {
		return ""
	}
	return McpFirstLine(s.Error)
}

// mcpHubNotices is pi's error slot: config errors and overridden
// servers, one per line, prefixed the way pi prefixes them.
func (m Model) mcpHubNotices() string {
	var lines []string
	for _, e := range m.McpConfigErrs {
		lines = append(lines, "config: "+e)
	}
	for _, o := range m.McpOverridden {
		lines = append(lines, "overridden: "+o)
	}
	return strings.Join(lines, "\n")
}

// piEmptyServers is pi's empty-state wording, with our path.
func piEmptyServers(path string) string {
	return fmt.Sprintf("No MCP servers configured. Add them to %s or .pi/mcp.json.", path)
}

// refreshMcpActions re-derives the MCP section's third column from the
// cursor as it is NOW. It has to be callable after a cursor move, not only
// from the row build: LoadPsecRows resets the cursor to the top, so a
// column derived there would describe the first server, not the one the
// user just moved to.
func (m *Model) refreshMcpActions(d *Dialog) {
	d.McpAct, d.McpActDesc, d.McpActPayload = m.mcpHubActions(d)
	if d.McpActCursor >= len(d.McpAct) || d.McpActCursor < 0 {
		d.McpActCursor = 0
	}
	d.McpActRun = false // a rebuilt column is not a pending action
	// A rebuild is a fresh surface: an arm must not survive an async
	// listing landing under the user, or a later Enter deletes with no
	// confirmation they saw.
	m.clearMcpArm()
}

// mcpHubActions is the third column: the highlighted server's actions, in
// pi's order and pi's wording. Every row's payload names its own target,
// so the column cannot act on a different server than the one on screen.
func (m Model) mcpHubActions(d *Dialog) (labels, descs, payload []string) {
	sel := m.mcpHubTarget(d)
	if sel == "" {
		return nil, nil, nil
	}
	return mcpHubActionRows(&m, sel, nil, nil, nil)
}

// McpHubAction is the payload Enter should run in the actions column, if
// Enter asked for one. Exported: the handler that runs an MCP row lives
// in src/builtin, which cannot see the dialog's fields directly.
func McpHubAction(d *Dialog) (string, bool) {
	// Belt and braces: every key handler already refuses an undrawn column,
	// but this is the only place that can actually return a payload, so it
	// asks too rather than trusting four call sites to stay in agreement.
	if !d.McpActDrawn || !d.McpActRun || d.McpActCursor < 0 || d.McpActCursor >= len(d.McpActPayload) {
		return "", false
	}
	d.McpActRun = false
	return d.McpActPayload[d.McpActCursor], true
}

// McpHubSelected is the action row Enter would run right now (the cursor's
// payload), for callers that dispatch without the pending-action flag.
func McpHubSelected(d *Dialog) (string, bool) {
	if d.McpActFocus {
		return McpHubAction(d)
	}
	return "", false
}

// McpRemoveArmed reports whether the two-press remove gate is waiting on
// a second Enter for `name` (Exported: the Enter actions live in
// src/builtin, and the gate is now announced on the row itself).
func (m Model) McpRemoveArmed(name string) bool {
	return name != "" && m.mcpArmName == name &&
		!m.mcpArmAt.IsZero() && time.Since(m.mcpArmAt) < mcpArmWindow
}

// clearMcpArm drops a half-armed remove: the gate lives on the model so
// the hub and the editor share it, so every surface that navigates away
// from it must say so.
func (m *Model) clearMcpArm() { m.mcpArmName, m.mcpArmAt = "", time.Time{} }

// mcpHubServersMenu is the middle pane: pi's server list, verbatim —
// servers needing the user first, then by name, each row carrying
// `state · exposure · scope`.
func (m Model) mcpHubServersMenu(d *Dialog) (opts, descs, payload []string, msg string) {
	if notice := m.mcpHubNotices(); notice != "" {
		msg = notice
	}
	// The filter belongs to the server list, the way it does in the /mcp
	// panel: typing is how you find one. It used to be ignored here, so
	// every rebuild after a keystroke put the whole list back — the filter
	// box looked live and nothing narrowed.
	filter := strings.ToLower(strings.TrimSpace(d.Filter))
	servers := m.mcpHubServers()
	matched := 0
	for _, s := range servers {
		if filter != "" && !strings.Contains(strings.ToLower(s.Name), filter) {
			continue
		}
		matched++
		opts = append(opts, s.Name)
		descs = append(descs, fmt.Sprintf("%s · %s · %s", m.mcpState(s, true), mcpExposureName(s), mcpScopeName(s)))
		payload = append(payload, psecActMCPSel+s.Name)
	}
	if matched == 0 && filter != "" {
		opts = append(opts, fmt.Sprintf("— no server matches %q —", d.Filter))
		descs = append(descs, "Esc clears the filter")
		payload = append(payload, "")
	} else if matched == 0 {
		opts = append(opts, "— no MCP servers configured —")
		descs = append(descs, "")
		payload = append(payload, "")
		msg = strings.TrimSpace(msg + " · " + piEmptyServers(mcpDocPath(piAgentDir())))
	}
	// One line of hints. The server's own details live in the third column,
	// not above the panes, where they pushed the layout around.
	// pi's config errors stay in the message slot; the hint joins them
	// rather than replacing them.
	// The hint is not cached into Message: it depends on the terminal
	// width, and a resize does not rebuild the rows. The base message is
	// kept instead, and the hint is derived on every render.
	d.McpBaseMsg = msg
	msg = m.mcpHubSectionHint(d, msg)
	return opts, descs, payload, msg
}

// mcpHubSectionHint appends the actions hint to whatever the section
// already says, and says nothing about a column that is not drawn. It also
// does not say "Enter runs one": the first Enter only takes focus.
func (m Model) mcpHubSectionHint(d *Dialog, msg string) string {
	switch {
	case m.mcpActColumnDrawn(d):
		if msg == "" {
			msg = "→ its actions · Enter focus · then Enter runs · Esc close"
		} else {
			msg += " · → its actions · Enter focus · then Enter runs · Esc close"
		}
	case msg == "":
		msg = "Enter open · Esc close"
	}
	return msg
}

// mcpListIfStale triggers `pi mcp list --json` when the hub is sitting on
// the MCP section and the list is missing or old. The clock is stamped on
// the REQUEST, not on the response: pi's list has a 90s timeout, and
// without that every keystroke during it would start another.
//
// The returned command rides ALONG with a key's own result — it must
// never be returned in its place, or a stale list would swallow
// navigation (see updatePconfigDialog).
func (m *Model) mcpListIfStale(d *Dialog) tea.Cmd {
	if d.CurPsec() != PsecMCP || !m.McpInfoStale() {
		return nil
	}
	m.McpInfoAt = time.Now()
	return m.RunBuiltin(BuiltinMcpList, "")
}

// mcpScopeFile is where a write for this server lands, as pi words it.
func mcpScopeFile(s pirpc.McpServerInfo) string {
	switch s.Scope {
	case "global", "project":
		return "saved to the " + s.Scope + " mcp.json"
	case "extension":
		return "for this session" // an extension registered it: not persisted
	default:
		return "saved to mcp.json"
	}
}

// mcpHubTarget is the server the actions column acts on: the highlighted
// row's own payload names it — a server row names itself, and there is
// nothing else in this pane. Reading the payload (never a row index) is
// what makes it immune to the filter: the cursor counts rows the filter
// has hidden, but a payload is absolute.
func (m Model) mcpHubTarget(d *Dialog) string {
	i := psecCursor(d)
	if i < 0 || i >= len(d.Payload) {
		return ""
	}
	return mcpTargetOf(d.Payload[i])
}

// mcpTargetOf pulls the server name out of a hub MCP payload: both
// "@mcpsel:<name>" and "@mcpact:<kind>@<name>" carry it. The prefix is
// stripped first, then the name is what follows the FIRST '@' of what is
// left — the prefix's own '@' is already gone, and a name may contain
// more. This must agree with confirmMcpHubAction in src/builtin, which
// splits the same way; a divergence runs an action against a server that
// does not exist.
func mcpTargetOf(payload string) string {
	switch {
	case strings.HasPrefix(payload, psecActMCPSel):
		return strings.TrimPrefix(payload, psecActMCPSel)
	case strings.HasPrefix(payload, psecActMCPAct):
		// Cut at the FIRST separator after the prefix, not the last: a
		// server name may contain "@" (scoped npm/pypi ids), and the
		// runner in src/builtin splits the same way. Taking the last "@"
		// resolved "remove@foo@bar" to "bar".
		if _, name, ok := strings.Cut(strings.TrimPrefix(payload, psecActMCPAct), "@"); ok {
			return name
		}
	}
	return ""
}

// mcpHubServers is pi's server list: what `pi mcp list` reported, sorted so
// the ones needing the user come first. Before the list lands, the
// mcp.json snapshot names them — pi shows "starting" in that window.
func (m Model) mcpHubServers() []pirpc.McpServerInfo {
	out := append([]pirpc.McpServerInfo(nil), m.McpInfo...)
	if len(out) == 0 {
		for _, s := range m.MCP {
			out = append(out, pirpc.McpServerInfo{Name: s.Name, State: "starting"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := mcpAttentionRank(out[i]), mcpAttentionRank(out[j])
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// mcpAttentionRank is pi's: needs-auth first, then failed, disconnected,
// connecting, connected, and disabled last (a server the user turned off
// is not something needing their attention).
func mcpAttentionRank(s pirpc.McpServerInfo) int {
	if !s.Enabled {
		return 5
	}
	switch s.State {
	case "needs-auth":
		return 0
	case "failed":
		return 1
	case "disconnected":
		return 2
	case "connected":
		return 4
	default:
		return 3
	}
}

// mcpState is pi's describeState, with its exact wording.
func (m Model) mcpState(s pirpc.McpServerInfo, withError bool) string {
	if !s.Enabled {
		return "disabled"
	}
	switch s.State {
	case "needs-auth":
		return "needs sign-in"
	case "failed":
		if withError {
			if line := McpFirstLine(s.Error); line != "" {
				return "failed: " + line
			}
		}
		return "failed"
	case "connected":
		out := fmt.Sprintf("connected · %s", mcpPlural(len(s.Tools), "tool"))
		if s.Resources > 0 {
			out += " · " + mcpPlural(s.Resources, "resource")
		}
		return out
	case "connecting":
		return "connecting…"
	case "":
		return "starting"
	default:
		return s.State
	}
}

// mcpTransport is pi's describeTransport: `pi mcp list` puts the URL in
// `transport` for an http server and the command line for a stdio one,
// which is exactly what pi assembles from the entry.
func mcpTransport(s pirpc.McpServerInfo) string { return s.Transport }
