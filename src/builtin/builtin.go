package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/app"
	"pitago/src/pirpc"
)

func openLogin(m *app.Model, arg string) tea.Cmd {
	for _, p := range pirpc.ProviderEnvs {
		if strings.EqualFold(p.Provider, arg) || strings.EqualFold(p.Label, arg) {
			openLoginMethod(m, p.Provider, p.Env)
			return nil
		}
	}
	opts := make([]string, 0, len(pirpc.ProviderEnvs))
	descs := make([]string, 0, len(pirpc.ProviderEnvs))
	for _, p := range pirpc.ProviderEnvs {
		opts = append(opts, p.Provider)
		descs = append(descs, p.Label+" · "+p.Env)
	}
	d := &app.Dialog{Kind: "login", Title: "Provider login", Options: opts, Descs: descs}
	if arg != "" {
		d.Filter = arg
	}
	d.Reindex()
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
	return nil
}

// openLoginMethod opens the method picker once a provider is chosen.

func openLoginMethod(m *app.Model, provider, env string) {
	d := &app.Dialog{
		Kind: "loginMethod", Title: "Login " + provider,
		Message:       "API key goes to pitago's private keystore (" + env + "), pi reconnects automatically. Do OAuth in stock pi.",
		Options:       []string{"Enter API key", "OAuth / subscription", "Logged in — reload"},
		Descs:         []string{"save key + reconnect pi", "guide", "refresh model list"},
		LoginProvider: provider, LoginEnv: env,
	}
	d.Reindex()
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
}

// openLogout opens the provider picker to delete a saved key.

func openLogout(m *app.Model, arg string) tea.Cmd {
	keys := pirpc.LoadKeys(m.KeyPath)
	var opts, descs []string
	for _, p := range pirpc.ProviderEnvs {
		if _, ok := keys[p.Env]; !ok {
			continue
		}
		opts = append(opts, p.Provider)
		descs = append(descs, p.Label+" · "+p.Env)
	}
	if len(opts) == 0 {
		m.AddBlock(app.Block{Kind: "notice", Text: "keystore is empty — no keys saved"})
		m.Refresh()
		return nil
	}
	for i, o := range opts {
		if strings.EqualFold(o, arg) {
			return doLogout(m, o, descs[i])
		}
	}
	d := &app.Dialog{Kind: "logout", Title: "Remove API key", Message: "Pick a provider to delete its key from the keystore.", Options: opts, Descs: descs}
	if arg != "" {
		d.Filter = arg
	}
	d.Reindex()
	m.Dialogs = append(m.Dialogs, d)
	m.Refresh()
	return nil
}

// doLogout deletes the key then reconnects pi to drop the credential.

func doLogout(m *app.Model, provider, desc string) tea.Cmd {
	env := pirpc.LookupEnv(provider)
	if env == "" {
		env = desc // fallback
	}
	if err := pirpc.DeleteKey(m.KeyPath, env); err != nil {
		m.AddBlock(app.Block{Kind: "notice", Text: "failed to delete key: " + err.Error(), Err: true})
		m.Refresh()
		return nil
	}
	// Drop it from our env too: the respawned pi inherits env, and would
	// otherwise stay logged in until TUI restart.
	if env != "" {
		_ = os.Unsetenv(env)
	}
	m.AddBlock(app.Block{Kind: "notice", Text: "deleted key " + provider + " — reconnecting pi…"})
	return m.RespawnPi()
}

// respawnPi kills the old pi and respawns keeping the same session (to pick up added/removed keys).

// loadSettings fetches current state to build the settings dialog.
func loadSettings(m *app.Model) tea.Cmd {
	return func() tea.Msg {
		st, err := m.Pi.GetState()
		if err != nil {
			return app.SettingsMsg{Err: err}
		}
		if st.ThinkingLevel == "" {
			st.ThinkingLevel = "off"
		}
		sst := app.SettingsState{
			Steering:    app.OrDefault(st.SteeringMode, "one-at-a-time"),
			FollowUp:    app.OrDefault(st.FollowUpMode, "one-at-a-time"),
			AutoCompact: st.AutoCompaction,
			AutoRetry:   m.AutoRetry,
			Thinking:    st.ThinkingLevel,
			Model:       m.ModelLbl,
		}
		opts, descs := settingsOptions(sst)
		return app.SettingsMsg{St: sst, Opts: opts, Descs: descs}
	}
}

func onoff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func settingsOptions(st app.SettingsState) ([]string, []string) {
	opts := []string{
		"Model: " + st.Model,
		"Thinking: " + st.Thinking,
		"Steering: " + st.Steering,
		"Follow-up: " + st.FollowUp,
		"Auto-compact: " + onoff(st.AutoCompact),
		"Auto-retry: " + onoff(st.AutoRetry),
	}
	descs := []string{
		"Enter: open model picker",
		"Enter: open thinking picker",
		"Enter: switch all/one-at-a-time",
		"Enter: switch all/one-at-a-time",
		"Enter: toggle",
		"Enter: toggle",
	}
	return opts, descs
}

// settingsAction handles Enter on each settings row.

func settingsAction(m *app.Model, ri int) (tea.Model, tea.Cmd) {
	if len(m.Dialogs) == 0 {
		return m, nil
	}
	d := m.Dialogs[0]
	st := d.Settings
	refresh := func() tea.Msg {
		s, err := m.Pi.GetState()
		if err != nil {
			return app.SettingsMsg{Err: err}
		}
		sst := app.SettingsState{
			Steering:    app.OrDefault(s.SteeringMode, st.Steering),
			FollowUp:    app.OrDefault(s.FollowUpMode, st.FollowUp),
			AutoCompact: s.AutoCompaction,
			AutoRetry:   m.AutoRetry,
			Thinking:    app.OrDefault(s.ThinkingLevel, st.Thinking),
			Model:       m.ModelLbl,
		}
		opts, descs := settingsOptions(sst)
		return app.SettingsMsg{St: sst, Opts: opts, Descs: descs}
	}
	switch ri {
	case 0: // model picker
		m.Dialogs = m.Dialogs[1:]
		m.Refresh()
		return m, m.RunBuiltin("model", "")
	case 1: // thinking picker
		m.Dialogs = m.Dialogs[1:]
		m.Refresh()
		return m, m.RunBuiltin("thinking", "")
	case 2:
		next := "all"
		if st.Steering == "all" {
			next = "one-at-a-time"
		}
		m.Status = "switching steering…"
		m.Refresh()
		return m, func() tea.Msg {
			if err := m.Pi.SetSteering(next); err != nil {
				return app.SettingsMsg{Err: err}
			}
			return refresh()
		}
	case 3:
		next := "all"
		if st.FollowUp == "all" {
			next = "one-at-a-time"
		}
		m.Status = "switching follow-up…"
		m.Refresh()
		return m, func() tea.Msg {
			if err := m.Pi.SetFollowUp(next); err != nil {
				return app.SettingsMsg{Err: err}
			}
			return refresh()
		}
	case 4:
		m.Status = "switching auto-compact…"
		m.Refresh()
		return m, func() tea.Msg {
			if err := m.Pi.SetAutoCompact(!st.AutoCompact); err != nil {
				return app.SettingsMsg{Err: err}
			}
			return refresh()
		}
	case 5:
		m.AutoRetry = !st.AutoRetry
		m.Status = "switching auto-retry…"
		m.Refresh()
		auto := m.AutoRetry
		return m, func() tea.Msg {
			if err := m.Pi.SetAutoRetry(auto); err != nil {
				m.AutoRetry = !auto
				return app.SettingsMsg{Err: err}
			}
			return refresh()
		}
	}
	return m, nil
}

// renderTree renders the session tree pi-style: branch connectors, a "• "
// prefix on the active leaf path, "[label] " bookmarks, and one pi-formatted
// row per entry (see treeRow). Usage entries are skipped like pi (their
// children still render). Read-only: pi's RPC has no navigate_tree, so
// branch switching stays in pi's own TUI.
func renderTree(nodes []pirpc.TreeNode, leaf string) string {
	if len(nodes) == 0 {
		return "No entries in session"
	}
	tcm := buildToolCallMap(nodes)
	active := activePathIDs(nodes, leaf)
	var b strings.Builder
	count := 0
	var walk func(ns []pirpc.TreeNode, prefix string)
	walk = func(ns []pirpc.TreeNode, prefix string) {
		for i, n := range ns {
			if count >= 100 {
				return
			}
			if n.Entry.Type == "usage" {
				walk(n.Children, prefix)
				continue
			}
			count++
			last := i == len(ns)-1
			branch, cont := "├── ", "│   "
			if last {
				branch, cont = "└── ", "    "
			}
			b.WriteString(prefix + branch + treeRow(n, tcm, active) + "\n")
			walk(n.Children, prefix+cont)
		}
	}
	walk(nodes, "")
	if count == 0 {
		return "No entries in session"
	}
	if count >= 100 {
		b.WriteString("…(truncated)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// buildToolCallMap indexes assistant toolCall blocks by id so toolResult
// rows can show what ran (pi keeps the same map for its tree list).
func buildToolCallMap(nodes []pirpc.TreeNode) map[string]pirpc.ContentBlock {
	m := map[string]pirpc.ContentBlock{}
	var walk func(ns []pirpc.TreeNode)
	walk = func(ns []pirpc.TreeNode) {
		for _, n := range ns {
			if n.Entry.Type == "message" {
				for _, bl := range pirpc.BlocksOf(n.Entry.Message.Content) {
					if bl.Type == "toolCall" && bl.ID != "" {
						m[bl.ID] = bl
					}
				}
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return m
}

// activePathIDs marks the leaf and every ancestor up to the root (pi's
// buildActivePath: the "• " trail of the current branch).
func activePathIDs(nodes []pirpc.TreeNode, leaf string) map[string]bool {
	if leaf == "" {
		return nil
	}
	parent := map[string]string{}
	var walk func(ns []pirpc.TreeNode)
	walk = func(ns []pirpc.TreeNode) {
		for _, n := range ns {
			if n.Entry.ParentID != nil {
				parent[n.Entry.ID] = *n.Entry.ParentID
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	out := map[string]bool{leaf: true}
	for id := leaf; ; {
		p, ok := parent[id]
		if !ok || p == "" {
			break
		}
		out[p] = true
		id = p
	}
	return out
}

// treeRow is one pi tree-list row: "• " when on the active path,
// "[label] " bookmarks, then the entry text (pi's getEntryDisplayText,
// plain — colors stay in pi's TUI).
func treeRow(n pirpc.TreeNode, tcm map[string]pirpc.ContentBlock, active map[string]bool) string {
	e := n.Entry
	var content string
	switch e.Type {
	case "message":
		content = treeMessage(e, tcm)
	case "custom_message":
		content = "[" + e.CustomType + "]: " + treeNorm(pirpc.TextOf(e.Content))
	case "compaction":
		content = fmt.Sprintf("[compaction: %dk tokens]", (e.TokensBefore+500)/1000)
	case "branch_summary":
		content = "[branch summary]: " + treeNorm(e.Summary)
	case "model_change":
		content = "[model: " + app.OrDefault(e.ModelID, "?") + "]"
	case "thinking_level_change":
		content = "[thinking: " + app.OrDefault(e.ThinkingLevel, "?") + "]"
	case "custom":
		content = "[custom: " + e.CustomType + "]"
	case "label":
		lbl := e.Label
		if lbl == "" {
			lbl = "(cleared)"
		}
		content = "[label: " + lbl + "]"
	case "session_info":
		if e.Name != "" {
			content = "[title: " + e.Name + "]"
		} else {
			content = "[title: empty]"
		}
	default:
		content = e.Type + " " + app.ShortID(e.ID)
	}
	pre := ""
	if active[e.ID] {
		pre += "• "
	}
	if n.Label != "" {
		pre += "[" + n.Label + "] "
	}
	return pre + content
}

// treeMessage formats message entries like pi: "user: …", "assistant: …"
// (with (aborted)/error/(no content) fallbacks), toolResult as the tool
// call ("[read: path]", "[bash: cmd]", …) via the toolCall map, and
// bashExecution as "[bash]: command".
func treeMessage(e pirpc.TreeEntry, tcm map[string]pirpc.ContentBlock) string {
	msg := e.Message
	switch msg.Role {
	case "user":
		return "user: " + treeNorm(pirpc.TextOf(msg.Content))
	case "assistant":
		if t := treeNorm(pirpc.TextOf(msg.Content)); t != "" {
			return "assistant: " + t
		}
		if msg.StopReason == "aborted" {
			return "assistant: (aborted)"
		}
		if strings.TrimSpace(msg.ErrorMessage) != "" {
			err := treeNorm(msg.ErrorMessage)
			if r := []rune(err); len(r) > 80 {
				err = string(r[:80])
			}
			return "assistant: " + err
		}
		return "assistant: (no content)"
	case "toolResult":
		if msg.ToolCallID != "" {
			if tc, ok := tcm[msg.ToolCallID]; ok {
				return treeTool(tc.Name, tc.Arguments)
			}
		}
		return "[" + app.OrDefault(msg.ToolName, "tool") + "]"
	case "bashExecution":
		cmd := msg.Command
		if cmd == "" {
			cmd = pirpc.TextOf(msg.Content)
		}
		return "[bash]: " + treeNorm(cmd)
	default:
		if msg.Role == "" {
			return "[message]"
		}
		return "[" + msg.Role + "]"
	}
}

// treeTool formats one tool call like pi's tree list ("[read: path:1-3]",
// "[bash: cmd]", "[grep: /pat/ in path]", …).
func treeTool(name string, args json.RawMessage) string {
	shortPath := func(keys ...string) string { return pirpc.Shorten(treeArg(args, keys...)) }
	switch strings.ToLower(name) {
	case "read":
		p := shortPath("path", "file_path")
		off, hasOff := treeArgNum(args, "offset")
		lim, hasLim := treeArgNum(args, "limit")
		if !hasOff && !hasLim {
			return "[read: " + p + "]"
		}
		start := 1
		if hasOff && off >= 1 {
			start = int(off)
		}
		if hasLim && lim >= 1 {
			return fmt.Sprintf("[read: %s:%d-%d]", p, start, start+int(lim)-1)
		}
		return fmt.Sprintf("[read: %s:%d]", p, start)
	case "write":
		return "[write: " + shortPath("path", "file_path") + "]"
	case "edit":
		return "[edit: " + shortPath("path", "file_path") + "]"
	case "bash":
		cmd := treeNorm(treeArg(args, "command"))
		if r := []rune(cmd); len(r) > 50 {
			cmd = string(r[:50]) + "..."
		}
		return "[bash: " + cmd + "]"
	case "grep":
		pat := treeNorm(treeArg(args, "pattern"))
		if pat == "" {
			pat = "..."
		}
		return "[grep: /" + pat + "/ in " + app.OrDefault(shortPath("path"), ".") + "]"
	case "find":
		pat := treeNorm(treeArg(args, "pattern"))
		if pat == "" {
			pat = "..."
		}
		return "[find: " + pat + " in " + app.OrDefault(shortPath("path"), ".") + "]"
	case "ls":
		return "[ls: " + app.OrDefault(shortPath("path"), ".") + "]"
	default:
		s := strings.Join(strings.Fields(string(args)), " ")
		if s == "" || s == "null" {
			return "[" + name + "]"
		}
		if r := []rune(s); len(r) > 40 {
			return "[" + name + ": " + string(r[:40]) + "...]"
		}
		return "[" + name + ": " + s + "]"
	}
}

// treeArg reads one string field from tool-call JSON args ("": absent).
func treeArg(raw json.RawMessage, keys ...string) string {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	for _, k := range keys {
		v, ok := m[k]
		if !ok || string(v) == "null" {
			continue
		}
		var s string
		if err := json.Unmarshal(v, &s); err != nil {
			continue
		}
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// treeArgNum reads one numeric field from tool-call JSON args.
func treeArgNum(raw json.RawMessage, key string) (float64, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok || string(v) == "null" {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(v, &n); err != nil {
		return 0, false
	}
	return n, true
}

// treeNorm matches pi's row text: newline/tab → space, trimmed, 200 chars.
func treeNorm(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 200 {
		return string(r[:200])
	}
	return s
}

// Origin marks where a builtin feature comes from.
const (
	// OriginPi re-implements one of pi's TUI-level builtins over RPC.
	// pi's own builtins never arrive via get_commands, so pitago intercepts
	// them locally instead of leaking "/..." text into the chat.
	OriginPi = "pi"
	// OriginPitago is pitago's own addition on top of pi.
	OriginPitago = "pitago"
)

// Builtin is one locally-executed slash command (implementation lives here,
// wire-up in src/app via UseBuiltins).
type Builtin = app.Builtin

// All lists every intercepted command: pi's BUILTIN_SLASH_COMMANDS plus
// pitago's own /recent. Anything else (extension/prompt/skill commands,
// chat text) falls through to pi via Prompt.
func All() []app.Builtin {
	pi := func(name, desc, usage string, run func(m *app.Model, arg string) tea.Cmd) app.Builtin {
		return app.Builtin{Name: name, Desc: desc, Usage: usage, Origin: OriginPi, Run: run}
	}
	all := []app.Builtin{
		pi("settings", "Open settings menu", "/settings", func(m *app.Model, arg string) tea.Cmd {
			m.Status = "loading settings…"
			m.Refresh()
			return loadSettings(m)
		}),
		pi("model", "<provider/model> — Select model (opens selector UI)", "/model", func(m *app.Model, arg string) tea.Cmd {
			m.Status = "loading models…"
			m.Refresh()
			return func() tea.Msg {
				models, err := m.Pi.GetModels()
				if err != nil {
					return app.PickerMsg{Kind: "model", Err: err}
				}
				opts := make([]string, 0, len(models))
				descs := make([]string, 0, len(models))
				provs := make([]string, 0, len(models))
				for _, mi := range models {
					opts = append(opts, mi.ID)
					descs = append(descs, mi.Name+" · "+mi.Provider)
					provs = append(provs, mi.Provider)
				}
				return app.PickerMsg{Kind: "model", Options: opts, Descs: descs, Providers: provs, Current: m.ModelLbl}
			}
		}),
		pi("tree", "Navigate session tree (switch branches)", "/tree", func(m *app.Model, arg string) tea.Cmd {
			m.Status = "loading session tree…"
			m.Refresh()
			return func() tea.Msg {
				nodes, leaf, err := m.Pi.GetTree()
				if err != nil {
					return app.TreeMsg{Err: err}
				}
				return app.TreeMsg{Text: renderTree(nodes, leaf)}
			}
		}),
		pi("thinking", "<level> — Set thinking level", "/thinking", func(m *app.Model, arg string) tea.Cmd {
			return m.OpenThinking()
		}),
		pi("reload", "Reload keybindings, extensions, skills, prompts, themes, and context files", "/reload", func(m *app.Model, arg string) tea.Cmd {
			m.Status = "reloading commands…"
			m.Refresh()
			return func() tea.Msg {
				cmds, err := m.Pi.GetCommands()
				return app.CmdsRefreshMsg{Cmds: cmds, Err: err, Announce: true}
			}
		}),
		pi("login", "<provider> — Configure provider authentication", "/login [provider]", func(m *app.Model, arg string) tea.Cmd {
			return openLogin(m, arg)
		}),
		pi("logout", "Remove provider authentication", "/logout [provider]", func(m *app.Model, arg string) tea.Cmd {
			return openLogout(m, arg)
		}),
		pi("new", "Start a new session", "/new", func(m *app.Model, arg string) tea.Cmd {
			m.Status = "opening new session…"
			m.Refresh()
			return func() tea.Msg {
				return app.SessionResetMsg{Err: m.Pi.NewSession()}
			}
		}),
		pi("quit", "Quit pi", "/quit", func(m *app.Model, arg string) tea.Cmd {
			return tea.Quit
		}),
		pi("session", "Show session info and stats", "/session", func(m *app.Model, arg string) tea.Cmd {
			m.Status = "loading session info…"
			m.Refresh()
			return func() tea.Msg {
				st, err := m.Pi.GetState()
				if err != nil {
					return app.SettingsRefreshMsg{Err: err}
				}
				stats, _ := m.Pi.GetStats()
				sess := st.SessionName
				if sess == "" {
					sess = app.ShortID(st.SessionID)
				}
				return app.SettingsRefreshMsg{Notice: fmt.Sprintf("%s · %s · %d msgs · %d tools · %s · $%.2f",
					sess, m.ModelLbl, st.MessageCount, stats.ToolCalls, app.FmtNum(stats.TokensTotal), stats.Cost)}
			}
		}),
		pi("resume", "Resume a session (like pi)", "/resume [path]", func(m *app.Model, arg string) tea.Cmd {
			return m.OpenResume(arg)
		}),
		{
			Name: "recent", Desc: "Switch recent model (pitago)", Usage: "/recent",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				return m.OpenRecents()
			},
		},
		{
			Name: "yank", Desc: "Copy last assistant answer to clipboard (chat-only)", Usage: "/yank",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				return m.YankLast()
			},
		},
		{
			Name: "copy", Desc: "Copy last assistant answer to clipboard (chat-only)", Usage: "/copy",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				return m.YankLast()
			},
		},
		{
			Name: "sidebar", Desc: "Hide/show sidebar (hide for clean drag-select of chat)", Usage: "/sidebar",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				m.ToggleSide()
				return nil
			},
		},
		{
			Name: "plugins", Desc: "Collapse/expand installed pi plugins in the sidebar", Usage: "/plugins",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				m.TogglePlugins()
				return nil
			},
		},
		{
			Name: "mouse", Desc: "Toggle mouse (click sidebar, wheel scroll) — off for native text selection", Usage: "/mouse [on|off]",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				return m.ToggleMouse(arg)
			},
		},
		{
			Name: "update", Desc: "Check + install pitago update (pitago)", Usage: "/update",
			Origin: OriginPitago,
			Run: func(m *app.Model, arg string) tea.Cmd {
				m.Status = "checking for updates…"
				m.Refresh()
				return m.CheckUpdatesCmd(false)
			},
		},
	}
	// Pi builtins with no RPC equivalent stay intercepted so they report
	// instead of leaking into the chat (old runBuiltin default branch).
	for _, name := range []string{
		"scoped-models", "export", "import", "share", "name",
		"changelog", "hotkeys", "fork", "clone", "trust", "compact",
	} {
		name := name
		all = append(all, app.Builtin{
			Name: name, Desc: "pi TUI-only (no RPC equivalent)", Usage: "/" + name,
			Origin: OriginPi,
			Run: func(m *app.Model, arg string) tea.Cmd {
				m.AddBlock(app.Block{Kind: "notice", Text: fmt.Sprintf("/%s needs pi's own TUI — run it in pi directly", name)})
				m.Refresh()
				return nil
			},
		})
	}
	return all
}
