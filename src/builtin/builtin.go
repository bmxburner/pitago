package builtin

import (
	"fmt"
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
		if arg != "" && !strings.EqualFold(p.Provider, arg) {
			continue
		}
		opts = append(opts, p.Provider)
		descs = append(descs, p.Label+" · "+p.Env)
	}
	if arg != "" && len(opts) == 1 {
		return doLogout(m, opts[0], descs[0])
	}
	if len(opts) == 0 {
		m.AddBlock(app.Block{Kind: "notice", Text: "keystore is empty — no keys saved"})
		m.Refresh()
		return nil
	}
	d := &app.Dialog{Kind: "logout", Title: "Remove API key", Message: "Pick a provider to delete its key from the keystore.", Options: opts, Descs: descs}
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

// renderTree renders the session tree as text.

func renderTree(nodes []pirpc.TreeNode, leaf string) string {
	var b strings.Builder
	count := 0
	var walk func(ns []pirpc.TreeNode, prefix string)
	walk = func(ns []pirpc.TreeNode, prefix string) {
		for i, n := range ns {
			if count >= 100 {
				return
			}
			count++
			last := i == len(ns)-1
			branch, cont := "├── ", "│   "
			if last {
				branch, cont = "└── ", "    "
			}
			mark := ""
			if n.Entry.ID == leaf {
				mark = " • current"
			}
			b.WriteString(prefix + branch + treeLabel(n.Entry) + mark + "\n")
			walk(n.Children, prefix+cont)
		}
	}
	walk(nodes, "")
	if count >= 100 {
		b.WriteString("…(truncated)\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func treeLabel(e pirpc.TreeEntry) string {
	switch e.Type {
	case "message":
		t := app.Short(pirpc.TextOf(e.Message.Content), 60)
		extra := ""
		for _, bl := range pirpc.BlocksOf(e.Message.Content) {
			if bl.Type == "toolCall" {
				extra += " [" + bl.Name + "]"
			}
		}
		if t == "" && extra != "" {
			return e.Message.Role + ":" + extra
		}
		return e.Message.Role + ": " + t + extra
	case "model_change":
		return "model → " + app.OrDefault(e.ModelID, "?")
	case "thinking_level_change":
		return "thinking → " + app.OrDefault(e.Level, "?")
	default:
		return e.Type + " " + app.ShortID(e.ID)
	}
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
			m.Status = "loading thinking levels…"
			m.Refresh()
			return func() tea.Msg {
				levels, err := m.Pi.GetLevels()
				if err != nil {
					return app.PickerMsg{Kind: "thinking", Err: err}
				}
				return app.PickerMsg{Kind: "thinking", Options: levels}
			}
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
