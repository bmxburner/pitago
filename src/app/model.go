package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"pitago/src/components/chat"
	"pitago/src/components/mention"
	"pitago/src/components/recent"
	"pitago/src/pirpc"
)

// Block is one rendered unit in the chat column (see components/chat).
type Block = chat.Block

// Dialog is a modal: extension permission prompt or native picker/settings.

type Dialog struct {
	ID            string
	Method        string // select | confirm (extension UI)
	Kind          string // "ui" | "model" | "thinking" | "settings" | "login" | "loginDone" | "secret" | "sessions" | ...
	Title         string
	Message       string
	Options       []string
	Descs         []string
	Providers     []string // model picker: parallel provider per option
	Paths         []string // sessions picker: parallel session file per option
	Scope         string   // sessions picker: "current" | "all" (Tab toggles)
	Payload       []string // yank picker: full message text per option
	Cursor        int
	Filter        string // picker filter / secret buffer
	FIdx          []int
	Settings      SettingsState
	LoginProvider string // login flow: provider id
	LoginEnv      string // login flow: env var
}

// SettingsState snapshots tunable agent settings.

type SettingsState struct {
	Steering, FollowUp     string
	AutoCompact, AutoRetry bool
	Thinking, Model        string
}

// RecentModel is one entry of the sidebar list (see components/recent).
type RecentModel = recent.RecentModel

type Model struct {
	vp           viewport.Model
	sideVp       viewport.Model // sidebar scroll: clips content to sideH, Ctrl/Alt+↑↓/PgUp/PgDn or wheel over it scrolls
	ta           textarea.Model
	Pi           *pirpc.Client
	blocks       []Block
	tools        map[string]int // toolCallId -> block index
	curAsst      int
	curThink     int
	asstDelta    bool // text deltas streamed into curAsst (message_end must not re-add)
	thinkDelta   bool // thinking deltas streamed into curThink (same)
	thinking     bool
	Status       string
	extStat      string
	ready        bool
	winW         int
	winH         int
	hideSide     bool // Ctrl+B: hide sidebar for clean drag-select of chat
	Mouse        bool // --mouse: terminal reports clicks (sidebar recent switch)
	baseVpH      int
	cwd          string
	ModelLbl     string
	AppVersion   string // pitago build version for the welcome header ("" = omit)
	thinkLvl     string // thinking level from get_state
	autoCompact  bool   // auto-compaction from get_state
	ctxWindow    int    // model context window from get_state/stats
	session      string
	sessStart    time.Time // session clock for sidebar "time"
	turnStart    time.Time // last turn start (for "last" + speed)
	turnOutBase  int       // stats.Out at last turn_start
	pendSpeed    bool      // compute last/speed on next statsMsg
	lastDur      time.Duration
	lastSpeed    float64 // tok/s of last turn
	ws           wsData  // workspace git status (polled)
	Stats        pirpc.Stats
	queue        pirpc.Queue
	Todos        []TodoItem  // tracked from todo-tool calls (sidebar)
	MCP          []McpServer // pi agent-dir MCP snapshot (sidebar)
	Dialogs      []*Dialog
	connErr      string
	AutoRetry    bool          // no RPC getter; tracked locally (default on)
	respawning   bool          // reconnecting pi: skip pi_exited notice
	spawnOpts    pirpc.Options // for respawning pi (login)
	KeyPath      string        // keystore API keys
	sessionFile  string        // respawn keeps the same session
	Cmds         []pirpc.RepoCommand
	cmdOpen      bool
	cmdCursor    int
	cmdOffset    int   // first visible row of the scroll window
	cmdItems     []int // indices into cmds
	atOpen       bool
	atCursor     int
	atOffset     int // first visible row of the @ scroll window
	atRow        int // input row holding the @ token
	atStart      int // rune index where the @ token starts
	atPrefix     string
	atItems      []mention.Item
	imgAtts      []imgAttach // input tray: dropped/pasted/@-completed images as [Image N] chips
	imgSeq       int         // chip counter, never renumbered
	trayFocus    bool        // cursor moved into the tray (↓ from last input line)
	imgCursor    int         // selected chip while trayFocus
	trayRet      int         // input offset to restore on Esc
	pet          petState
	recentModels []RecentModel
	recentPath   string // persisted recent models ("" = don't persist)
	builtins     []Builtin
	confirm      map[string]ConfirmFunc
	expandTools  bool // Ctrl+G: expand every tool block (write/read/diff previews), pi-style
}

type connectedMsg struct {
	state pirpc.State
	msgs  []pirpc.AgentMessage
	stats pirpc.Stats
	cmds  []pirpc.RepoCommand
	err   error
}

type piEventMsg struct{ pirpc.Event }

type statsMsg struct {
	stats pirpc.Stats
	err   error
}

type stateRefreshMsg struct {
	state pirpc.State
	err   error
}

type wsTickMsg struct{}

type wsMsg struct {
	data wsData
}

type sentAckMsg struct{ err error }

type SessionResetMsg struct{ Err error }

type ModelCycleMsg struct {
	Label    string
	Provider string // may be "" (cycle path); resolved label-only entry
	ID       string // model id when known, else ""
	Err      error
}

type PickerMsg struct {
	Kind                      string
	Options, Descs, Providers []string
	Paths                     []string // sessions picker: parallel session file per option
	Filter                    string   // sessions picker: pre-typed filter (/resume <arg>)
	Scope                     string   // sessions picker: "current" | "all"
	Replace                   bool     // sessions picker: Tab scope swap into the open dialog
	Current                   string
	Err                       error
}

type SettingsMsg struct {
	St SettingsState
	// Opts/Descs are precomputed by src/builtin (this package must not
	// import builtin, so the producer ships them in the message).
	Opts, Descs []string
	Err         error
}

type TreeMsg struct {
	Text string
	Err  error
}

type SettingsRefreshMsg struct {
	Notice string
	Err    error
}

type LoginKeyMsg struct {
	Provider, Env, Key string
}

type respawnMsg struct {
	client *pirpc.Client
	err    error
}

type CmdsRefreshMsg struct {
	Cmds     []pirpc.RepoCommand
	Err      error
	Announce bool // manual /reload: reports the result
}

func New(pi *pirpc.Client, cwd string) Model {
	ta := textarea.New()
	ta.Placeholder = "Type a message… (/ commands · ^V paste)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Prompt = "❯ "
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(cInput)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(cMuted)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(cMuted)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	return Model{
		ta:        ta,
		Pi:        pi,
		tools:     make(map[string]int),
		curAsst:   -1,
		curThink:  -1,
		Status:    "connecting to pi…",
		cwd:       cwd,
		ModelLbl:  "…",
		AutoRetry: true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.fetchAll(), m.pollCmds(), m.pollWs())
}

// fetchAll loads state/messages/stats/commands after (re)connect.

func (m Model) fetchAll() tea.Cmd {
	return func() tea.Msg {
		state, err := m.Pi.GetState()
		if err != nil {
			return connectedMsg{err: err}
		}
		msgs, _ := m.Pi.GetMessages()
		stats, _ := m.Pi.GetStats()
		cmds, _ := m.Pi.GetCommands()
		return connectedMsg{state: state, msgs: msgs, stats: stats, cmds: cmds}
	}
}

func (m *Model) AddBlock(b Block) int {
	m.blocks = append(m.blocks, b)
	return len(m.blocks) - 1
}

func (m *Model) ensureAsst() int {
	if m.curAsst < 0 {
		m.curAsst = m.AddBlock(Block{Kind: "assistant"})
	}
	return m.curAsst
}

func (m *Model) ensureThink() int {
	if m.curThink < 0 {
		m.curThink = m.AddBlock(Block{Kind: "thinking"})
	}
	return m.curThink
}

func (m *Model) ensureTool(callID, name string) int {
	if i, ok := m.tools[callID]; ok && callID != "" {
		return i
	}
	i := m.AddBlock(Block{Kind: "tool", ToolName: name, ToolStatus: "running", ToolCallID: callID})
	if callID != "" {
		m.tools[callID] = i
	}
	return i
}

// setToolArgs records a tool call's arguments: the raw JSON (so write
// content can render a collapsible preview like pi) plus the pretty
// one-line header. Later (fuller) args overwrite earlier partials.
func (m *Model) setToolArgs(i int, name, raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	m.blocks[i].ToolArgsRaw = raw
	if h := prettyArgs(name, raw); h != "" {
		m.blocks[i].ToolArgs = h
	}
}

// update -------------------------------------------------------------------

func (m *Model) sendCmd(steer bool, text string, images []pirpc.ImageContent) tea.Cmd {
	m.thinking = true
	m.Status = "pi is running…"
	m.RefreshFollow()
	return func() tea.Msg {
		var err error
		if steer {
			_, err = m.Pi.Steer(text, images...)
			if err != nil { // fallback: follow_up via plain prompt
				_, err = m.Pi.Prompt(text, images...)
			}
		} else {
			_, err = m.Pi.Prompt(text, images...)
		}
		return sentAckMsg{err: err}
	}
}

// submitInput sends the input (or steers mid-turn). Shared by Enter and
// tray-Enter. Empty text + tray sends the images alone.
func (m *Model) submitInput() tea.Cmd {
	text := strings.TrimSpace(m.ta.Value())
	if text == "" && len(m.imgAtts) == 0 {
		return nil
	}
	if b, arg, ok := m.FindBuiltin(text); ok {
		m.ta.Reset()
		m.refreshCmds()
		m.refreshAt()
		m.Refresh()
		return b.Run(m, arg)
	}
	// @image.png → vision attachments (pi CLI parity); the @text
	// stays so history keeps the file ref, images ride the RPC.
	// Tray chips (drops/pastes/Tab-completed @) join in too.
	images, notes := m.takeImages(text)
	for _, n := range notes {
		m.AddBlock(Block{Kind: "notice", Text: n})
	}
	if m.thinking {
		m.ta.Reset()
		m.closeAt()
		return m.sendCmd(true, text, images)
	}
	m.ta.Reset()
	m.closeAt()
	m.Refresh()
	return m.sendCmd(false, text, images)
}

func (m *Model) queryStats() tea.Cmd {
	return func() tea.Msg {
		s, err := m.Pi.GetStats()
		return statsMsg{stats: s, err: err}
	}
}

func (m Model) fetchStateOnce() tea.Cmd {
	return func() tea.Msg {
		st, err := m.Pi.GetState()
		return stateRefreshMsg{state: st, err: err}
	}
}

// workspace git status ------------------------------------------------------

// wsFile is one changed file with diff counts.

func (m *Model) RespawnPi() tea.Cmd {
	m.respawning = true
	m.Status = "reconnecting pi…"
	m.Refresh()
	opts := m.spawnOpts
	opts.Session = m.sessionFile
	if opts.NoSession {
		opts.Session = ""
	}
	old := m.Pi
	return func() tea.Msg {
		old.Close()
		c, err := pirpc.Spawn(opts)
		if err != nil {
			return respawnMsg{err: err}
		}
		return respawnMsg{client: c}
	}
}

// SwitchSession respawns pi onto another session file (the /resume picker).
// Same reconnect path as RespawnPi; fetchAll repopulates the chat. A
// startup ping turns a silently-dying pi into pi's own reason (e.g. the
// session's folder was deleted after listing) instead of "pi has exited".
func (m *Model) SwitchSession(path string) tea.Cmd {
	if path == "" || path == m.sessionFile {
		m.AddBlock(Block{Kind: "notice", Text: "already on this session"})
		m.Refresh()
		return nil
	}
	m.respawning = true
	m.Status = "switching session…"
	m.Refresh()
	opts := m.spawnOpts
	opts.Session = path
	old := m.Pi
	return func() tea.Msg {
		old.Close()
		c, err := pirpc.Spawn(opts)
		if err != nil {
			return respawnMsg{err: err}
		}
		if _, err := c.GetState(); err != nil {
			select {
			case <-c.Done():
				// pi died at startup: report its reason, not "pi has exited"
				c.Close()
				if reason := pirpc.StderrTail(); reason != "" {
					err = fmt.Errorf("resume failed: %s", reason)
				} else {
					err = fmt.Errorf("resume failed: %w", err)
				}
				return respawnMsg{err: err}
			default:
				// slow starter; fetchAll will confirm
			}
		}
		return respawnMsg{client: c}
	}
}

func (m *Model) Refresh() {
	if !m.ready {
		return
	}
	// only stick to bottom when already there — no jump while reading history
	follow := m.vp.AtBottom()
	m.vp.SetContent(m.renderBlocks())
	if follow {
		m.vp.GotoBottom()
	}
	m.sideVp.SetContent(m.buildSidebarContent())
}

// ToggleSide hides/shows the sidebar and reflows chat+input widths
// (same math as the WindowSize handler).

func (m *Model) ToggleSide() {
	m.hideSide = !m.hideSide
	if !m.ready {
		return
	}
	w := m.mainW()
	m.vp.Width = w
	m.ta.SetWidth(w - 6)
	m.Refresh()
}

// refreshFollow rebuilds content and jumps to bottom (for new content worth seeing).

func (m *Model) RefreshFollow() {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.renderBlocks())
	m.vp.GotoBottom()
	m.sideVp.SetContent(m.buildSidebarContent())
}

// Cwd is the pi session working directory (picker loaders live outside
// this package and need it to find pi's session dir).
func (m *Model) Cwd() string { return m.cwd }

// SessionFile is the current pi session file ("": ephemeral/unknown).
func (m *Model) SessionFile() string { return m.sessionFile }

// NoSession reports whether this TUI runs without persisting sessions.
func (m *Model) NoSession() bool { return m.spawnOpts.NoSession }

// Builtin is one locally-executed slash command.
//
// Origin tells where the feature comes from and is shown in /help-style
// surfaces: "pi" re-implements one of pi's TUI-level builtins over RPC
// (pi's own builtins never arrive via get_commands), "pitago" is ours.
// Implementations live in src/builtin; this package only holds the table.
type Builtin struct {
	Name, Desc, Usage, Origin string
	Run                       func(m *Model, arg string) tea.Cmd
}

// ConfirmFunc runs the Enter action of a picker dialog kind.
// Implementations live in src/builtin (see Confirmers).
type ConfirmFunc func(m *Model, d *Dialog, ri int) (tea.Model, tea.Cmd)

// UseBuiltins wires the command registry (call once from main).
func (m *Model) UseBuiltins(b []Builtin, c map[string]ConfirmFunc) {
	m.builtins = b
	m.confirm = c
}

// Configure wires spawn options + config paths (call once from main).
func (m *Model) Configure(opts pirpc.Options, keyPath string) {
	m.spawnOpts = opts
	m.KeyPath = keyPath
	m.recentPath = pirpc.RecentPath()
	m.recentModels = recent.Load(m.recentPath)
}

// FindBuiltin matches "/name" or "/name args" against the registry.
// Anything else (extension/prompt/skill commands, chat text) falls through
// to pi via Prompt.
func (m *Model) FindBuiltin(text string) (Builtin, string, bool) {
	if len(text) < 2 || text[0] != '/' {
		return Builtin{}, "", false
	}
	rest := text[1:]
	name, arg := rest, ""
	if i := strings.Index(rest, " "); i >= 0 {
		name, arg = rest[:i], strings.TrimSpace(rest[i+1:])
	}
	if strings.Contains(name, "\n") || name == "" {
		return Builtin{}, "", false
	}
	for _, b := range m.builtins {
		if b.Name == name {
			return b, arg, true
		}
	}
	return Builtin{}, "", false
}

// RunBuiltin executes a registry command by name (nil when unknown).
func (m *Model) RunBuiltin(name, arg string) tea.Cmd {
	for _, b := range m.builtins {
		if b.Name == name {
			return b.Run(m, arg)
		}
	}
	return nil
}

// BuiltinRepo exposes the registry as repo-style commands for the / popup.
func BuiltinRepo(builtins []Builtin) []pirpc.RepoCommand {
	out := make([]pirpc.RepoCommand, 0, len(builtins))
	for _, b := range builtins {
		out = append(out, pirpc.RepoCommand{Name: b.Name, Description: b.Desc, Source: "builtin"})
	}
	return out
}

// ProgRef lets respawns rewire the pi event stream (tea.Program.Send is
// thread-safe). Set once from main before Run.
var ProgRef *tea.Program

// WireClient routes a pi client's events into the UI program.
func WireClient(c *pirpc.Client) {
	c.OnEvent = func(e pirpc.Event) {
		if ProgRef != nil {
			ProgRef.Send(piEventMsg{e})
		}
	}
}
