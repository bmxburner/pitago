package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"gotui/internal/pirpc"
)

const sideW = 34 // sidebar width

// palette (keep the existing theme)
const (
	cAccent   = lipgloss.Color("214")
	cBorder   = lipgloss.Color("238")
	cMuted    = lipgloss.Color("243")
	cText     = lipgloss.Color("252")
	cGreen    = lipgloss.Color("114")
	cYellow   = lipgloss.Color("221")
	cRed      = lipgloss.Color("203")
	cSide     = lipgloss.Color("141") // sidebar: purple
	cInput    = lipgloss.Color("39")  // input focus: cyan
	cInputDim = lipgloss.Color("30")  // input idle: dark teal
)

// Block is one rendered unit in the chat column.
type Block struct {
	Kind       string // user, assistant, thinking, tool, bash, notice
	Text       string
	ToolName   string
	ToolArgs   string
	ToolStatus string // running, done, error
	ToolResult string
	ToolCallID string
	Err        bool
}

// Dialog is a modal: extension permission prompt or native picker/settings.
type Dialog struct {
	ID            string
	Method        string // select | confirm (extension UI)
	Kind          string // "ui" | "model" | "thinking" | "settings" | "login" | "loginDone" | "secret"
	Title         string
	Message       string
	Options       []string
	Descs         []string
	Providers     []string // model picker: parallel provider per option
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

// builtinCmds are pi built-ins unavailable via RPC prompt, re-implemented natively.
func builtinCmds() []pirpc.RepoCommand {
	return []pirpc.RepoCommand{
		{Name: "model", Description: "Select model (picker)", Source: "builtin"},
		{Name: "thinking", Description: "Change thinking level", Source: "builtin"},
		{Name: "tree", Description: "View session tree", Source: "builtin"},
		{Name: "settings", Description: "Agent settings", Source: "builtin"},
		{Name: "login", Description: "Provider login (API key / OAuth)", Source: "builtin"},
		{Name: "logout", Description: "Remove saved API key", Source: "builtin"},
		{Name: "reload", Description: "Reload pi command list", Source: "builtin"},
	}
}

type model struct {
	vp          viewport.Model
	ta          textarea.Model
	pi          *pirpc.Client
	blocks      []Block
	tools       map[string]int // toolCallId -> block index
	curAsst     int
	curThink    int
	thinking    bool
	status      string
	extStat     string
	ready       bool
	winW        int
	winH        int
	baseVpH     int
	cwd         string
	modelLbl    string
	session     string
	stats       pirpc.Stats
	queue       pirpc.Queue
	dialogs     []*Dialog
	connErr     string
	autoRetry   bool          // no RPC getter; tracked locally (default on)
	respawning  bool          // reconnecting pi: skip pi_exited notice
	spawnOpts   pirpc.Options // for respawning pi (login)
	keyPath     string        // keystore API keys
	sessionFile string        // respawn keeps the same session
	cmds        []pirpc.RepoCommand
	cmdOpen     bool
	cmdCursor   int
	cmdItems    []int // indices into cmds
}

var (
	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("229")).
			Bold(true).
			Padding(0, 1)
	badgeStyle = lipgloss.NewStyle().
			Background(cAccent).
			Foreground(lipgloss.Color("235")).
			Bold(true).
			Padding(0, 1)
	statusBarStyle = lipgloss.NewStyle().Foreground(cMuted)
	userStyle      = lipgloss.NewStyle().
			BorderStyle(lipgloss.ThickBorder()).
			BorderLeft(true).
			BorderForeground(cAccent).
			PaddingLeft(1).
			Foreground(lipgloss.Color("15")).
			Bold(true)
	toolStyle = lipgloss.NewStyle().Foreground(cMuted)
	codeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("150"))
	sideStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cSide).
			Padding(0, 1)
	inputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cInputDim).
			Padding(0, 1)
	inputFocusStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cInput).
			Padding(0, 1)
	sideTitleStyle = lipgloss.NewStyle().Bold(true).Foreground(cSide)
	sepStyle       = lipgloss.NewStyle().Foreground(cBorder)
	cmdPopStyle    = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cAccent).
			Padding(0, 1)
	cmdHiStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	dlgStyle   = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cAccent).
			Padding(1, 3)
	errStyle  = lipgloss.NewStyle().Foreground(cRed)
	okStyle   = lipgloss.NewStyle().Foreground(cGreen)
	warnStyle = lipgloss.NewStyle().Foreground(cYellow)
)

// messages ---------------------------------------------------------------

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
type sentAckMsg struct{ err error }
type sessionResetMsg struct{ err error }
type modelCycleMsg struct {
	label string
	err   error
}
type pickerMsg struct {
	kind                      string
	options, descs, providers []string
	current                   string
	err                       error
}
type settingsMsg struct {
	st  SettingsState
	err error
}
type treeMsg struct {
	text string
	err  error
}
type settingsRefreshMsg struct {
	notice string
	err    error
}
type loginKeyMsg struct {
	provider, env, key string
}
type respawnMsg struct {
	client *pirpc.Client
	err    error
}
type cmdsRefreshMsg struct {
	cmds     []pirpc.RepoCommand
	err      error
	announce bool // manual /reload: reports the result
}

func initialModel(pi *pirpc.Client, cwd string) model {
	ta := textarea.New()
	ta.Placeholder = "Ask pi anything… (/commands still work)"
	ta.Focus()
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.Prompt = "› "
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(cInput)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(cMuted)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(cMuted)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	return model{
		ta:        ta,
		pi:        pi,
		tools:     make(map[string]int),
		curAsst:   -1,
		curThink:  -1,
		status:    "connecting to pi…",
		cwd:       cwd,
		modelLbl:  "…",
		autoRetry: true,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchAll(), m.pollCmds())
}

// fetchAll loads state/messages/stats/commands after (re)connect.
func (m model) fetchAll() tea.Cmd {
	return func() tea.Msg {
		state, err := m.pi.GetState()
		if err != nil {
			return connectedMsg{err: err}
		}
		msgs, _ := m.pi.GetMessages()
		stats, _ := m.pi.GetStats()
		cmds, _ := m.pi.GetCommands()
		return connectedMsg{state: state, msgs: msgs, stats: stats, cmds: cmds}
	}
}

func (m model) mainW() int {
	w := m.winW - sideW - 5
	if w < 30 {
		w = 30
	}
	return w
}

// blocks helpers ----------------------------------------------------------

func (m *model) addBlock(b Block) int {
	m.blocks = append(m.blocks, b)
	return len(m.blocks) - 1
}

func (m *model) ensureAsst() int {
	if m.curAsst < 0 {
		m.curAsst = m.addBlock(Block{Kind: "assistant"})
	}
	return m.curAsst
}

func (m *model) ensureThink() int {
	if m.curThink < 0 {
		m.curThink = m.addBlock(Block{Kind: "thinking"})
	}
	return m.curThink
}

func (m *model) ensureTool(callID, name string) int {
	if i, ok := m.tools[callID]; ok && callID != "" {
		return i
	}
	i := m.addBlock(Block{Kind: "tool", ToolName: name, ToolStatus: "running", ToolCallID: callID})
	if callID != "" {
		m.tools[callID] = i
	}
	return i
}

func compactArgs(raw string) string {
	s := strings.Join(strings.Fields(raw), " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

// update -------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// dialog captures all keys while open
	if len(m.dialogs) > 0 {
		if km, ok := msg.(tea.KeyMsg); ok {
			return m.updateDialog(km)
		}
		if _, ok := msg.(tea.WindowSizeMsg); !ok {
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.winW, m.winH = msg.Width, msg.Height
		mainW := m.mainW()
		// Layout fits winH exactly: header(1) + body(winH-2) + bar(1).
		// body = viewport + popup + input(6: textarea 3 + footer 1 + border 2).
		vpH := msg.Height - 8
		if vpH < 5 {
			vpH = 5
		}
		m.baseVpH = vpH
		if !m.ready {
			m.vp = viewport.New(mainW, vpH)
			m.ready = true
		} else {
			m.vp.Width = mainW
			m.vp.Height = vpH
		}
		m.ta.SetWidth(mainW - 6)
		m.refresh()
		return m, nil

	case connectedMsg:
		if msg.err != nil {
			m.connErr = msg.err.Error()
			m.status = "cannot connect to pi"
			return m, nil
		}
		m.modelLbl = msg.state.Model.ID
		if m.modelLbl == "" {
			m.modelLbl = msg.state.Model.Name
		}
		if msg.state.SessionName != "" {
			m.session = msg.state.SessionName
		} else {
			m.session = shortID(msg.state.SessionID)
		}
		m.stats = msg.stats
		m.cmds = append(builtinCmds(), msg.cmds...)
		m.sessionFile = msg.state.SessionFile
		m.blocks = nil
		m.tools = make(map[string]int)
		m.restore(msg.msgs)
		m.status = "ready"
		m.refreshFollow()
		return m, nil

	case piEventMsg:
		return m.handleEvent(msg.Event)

	case statsMsg:
		if msg.err == nil {
			m.stats = msg.stats
			m.refresh()
		}
		return m, nil

	case sentAckMsg:
		if msg.err != nil {
			m.addBlock(Block{Kind: "notice", Text: msg.err.Error(), Err: true})
			m.thinking = false
			m.status = "ready"
			m.refresh()
		}
		return m, nil

	case sessionResetMsg:
		if msg.err != nil {
			m.addBlock(Block{Kind: "notice", Text: msg.err.Error(), Err: true})
		}
		m.blocks = nil
		m.tools = make(map[string]int)
		m.curAsst, m.curThink = -1, -1
		m.thinking = false
		m.status = "ready"
		m.refreshFollow()
		return m, m.queryStats()

	case modelCycleMsg:
		if msg.err != nil {
			m.status = "ready"
			m.addBlock(Block{Kind: "notice", Text: "model switch failed: " + msg.err.Error(), Err: true})
		} else {
			m.modelLbl = msg.label
			m.status = "ready"
			m.addBlock(Block{Kind: "notice", Text: "model switched → " + msg.label})
		}
		m.refresh()
		return m, nil

	case pickerMsg:
		m.status = "ready"
		if msg.err != nil {
			m.addBlock(Block{Kind: "notice", Text: "failed to load list: " + msg.err.Error(), Err: true})
			m.refresh()
			return m, nil
		}
		title := "Select model"
		if msg.kind == "thinking" {
			title = "Thinking level"
		}
		d := &Dialog{Kind: msg.kind, Title: title, Options: msg.options, Descs: msg.descs, Providers: msg.providers}
		// preselect the current value
		for i, o := range d.Options {
			if o == msg.current {
				d.Cursor = i
				break
			}
		}
		d.reindex()
		// keep cursor after reindex
		for i, ri := range d.FIdx {
			if d.Options[ri] == msg.current {
				d.Cursor = i
				break
			}
		}
		m.dialogs = append(m.dialogs, d)
		m.refresh()
		return m, nil

	case settingsMsg:
		m.status = "ready"
		if msg.err != nil {
			m.addBlock(Block{Kind: "notice", Text: "settings error: " + msg.err.Error(), Err: true})
			m.refresh()
			return m, nil
		}
		opts, descs := settingsOptions(msg.st)
		if len(m.dialogs) > 0 && m.dialogs[0].Kind == "settings" {
			d := m.dialogs[0]
			d.Options, d.Descs, d.Settings = opts, descs, msg.st
			d.reindex()
		} else {
			d := &Dialog{Kind: "settings", Title: "Agent settings", Options: opts, Descs: descs, Settings: msg.st}
			d.reindex()
			m.dialogs = append(m.dialogs, d)
		}
		m.refresh()
		return m, nil

	case treeMsg:
		m.status = "ready"
		if msg.err != nil {
			m.addBlock(Block{Kind: "notice", Text: "tree error: " + msg.err.Error(), Err: true})
		} else {
			m.addBlock(Block{Kind: "tree", Text: msg.text})
		}
		m.refresh()
		return m, nil

	case settingsRefreshMsg:
		if msg.err != nil {
			m.addBlock(Block{Kind: "notice", Text: msg.err.Error(), Err: true})
		} else if msg.notice != "" {
			m.addBlock(Block{Kind: "notice", Text: msg.notice})
		}
		m.status = "ready"
		m.refresh()
		return m, nil

	case loginKeyMsg:
		if err := pirpc.SaveKey(m.keyPath, msg.env, msg.key); err != nil {
			m.addBlock(Block{Kind: "notice", Text: "failed to save key: " + err.Error(), Err: true})
			m.refresh()
			return m, nil
		}
		m.addBlock(Block{Kind: "notice", Text: "saved key " + msg.provider + " — reconnecting pi…"})
		return m, m.respawnPi()

	case respawnMsg:
		m.respawning = false
		if msg.err != nil {
			m.status = "ready"
			m.addBlock(Block{Kind: "notice", Text: "pi reconnect failed: " + msg.err.Error() + " — restart the TUI", Err: true})
			m.refresh()
			return m, nil
		}
		m.pi = msg.client
		msg.client.OnEvent = func(e pirpc.Event) { progRef.Send(piEventMsg{e}) }
		m.status = "reloading…"
		m.refresh()
		return m, m.fetchAll()

	case cmdsRefreshMsg:
		if msg.err == nil {
			merged := append(builtinCmds(), msg.cmds...)
			if cmdSig(merged) != cmdSig(m.cmds) {
				if msg.announce {
					old := make(map[string]bool, len(m.cmds))
					for _, c := range m.cmds {
						old[c.Source+"/"+c.Name] = true
					}
					var added []string
					for _, c := range merged {
						if !old[c.Source+"/"+c.Name] {
							added = append(added, "/"+c.Name)
						}
					}
					if len(added) == 0 {
						m.addBlock(Block{Kind: "notice", Text: fmt.Sprintf("no new commands (total %d)", len(merged))})
					} else {
						m.addBlock(Block{Kind: "notice", Text: fmt.Sprintf("new commands: %s (total %d)", strings.Join(added, ", "), len(merged))})
					}
				}
				m.cmds = merged
				m.refreshCmds()
				m.status = "ready"
				m.refresh()
			} else if msg.announce {
				m.addBlock(Block{Kind: "notice", Text: fmt.Sprintf("no new commands (total %d)", len(m.cmds))})
				m.status = "ready"
				m.refresh()
			}
		} else if msg.announce {
			m.addBlock(Block{Kind: "notice", Text: "failed to load commands: " + msg.err.Error(), Err: true})
			m.status = "ready"
			m.refresh()
		}
		return m, m.pollCmds()

	case tea.KeyMsg:
		if m.cmdOpen && m.handleCmdKey(msg) {
			return m, nil
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyCtrlN:
			m.status = "opening new session…"
			m.refresh()
			return m, func() tea.Msg {
				return sessionResetMsg{err: m.pi.NewSession()}
			}
		case tea.KeyCtrlP:
			m.status = "switching model…"
			m.refresh()
			return m, func() tea.Msg {
				label, err := m.pi.CycleModel()
				return modelCycleMsg{label: label, err: err}
			}
		case tea.KeyEsc:
			if m.thinking {
				m.status = "cancelling…"
				m.refresh()
				return m, func() tea.Msg {
					steer, follow, _ := m.pi.ClearQueue()
					restored := append(steer, follow...)
					if len(restored) > 0 {
						_ = restored // returned text, shown as notice for brevity
					}
					_, err := m.pi.Abort()
					return sentAckMsg{err: err}
				}
			}
			return m, nil
		case tea.KeyEnter:
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			if name, arg, ok := isBuiltinCmd(text); ok {
				m.ta.Reset()
				m.refreshCmds()
				m.refresh()
				return m, m.runBuiltin(name, arg)
			}
			if m.thinking {
				m.ta.Reset()
				return m, m.sendCmd(true, text)
			}
			m.ta.Reset()
			m.refresh()
			return m, m.sendCmd(false, text)
		}
	}

	var cmds []tea.Cmd
	var cmd tea.Cmd
	// Chat scroll: single-line input yields scroll keys to the viewport,
	// so input cursor and chat don't move together.
	if km, ok := msg.(tea.KeyMsg); ok && !strings.Contains(m.ta.Value(), "\n") {
		switch km.Type {
		case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
			m.vp, cmd = m.vp.Update(msg)
			return m, cmd
		}
	}
	m.ta, cmd = m.ta.Update(msg)
	cmds = append(cmds, cmd)
	if m.ready {
		m.vp, cmd = m.vp.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.refreshCmds()
	m.refresh()
	return m, tea.Batch(cmds...)
}

func (m *model) sendCmd(steer bool, text string) tea.Cmd {
	m.thinking = true
	m.status = "pi is running…"
	m.refreshFollow()
	return func() tea.Msg {
		var err error
		if steer {
			_, err = m.pi.Steer(text)
			if err != nil { // fallback: follow_up via plain prompt
				_, err = m.pi.Prompt(text)
			}
		} else {
			_, err = m.pi.Prompt(text)
		}
		return sentAckMsg{err: err}
	}
}

func (m *model) queryStats() tea.Cmd {
	return func() tea.Msg {
		s, err := m.pi.GetStats()
		return statsMsg{stats: s, err: err}
	}
}

// events --------------------------------------------------------------------

func (m model) handleEvent(ev pirpc.Event) (tea.Model, tea.Cmd) {
	switch ev.Type {
	case "agent_start":
		m.thinking = true
		m.status = "pi is running…"
	case "turn_start":
		m.curAsst, m.curThink = -1, -1
	case "message_update":
		m.applyDelta(ev.Raw)
	case "message_end":
		m.applyMessageEnd(ev.Raw)
		m.curAsst, m.curThink = -1, -1
	case "tool_execution_start":
		var p struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		i := m.ensureTool(p.ToolCallID, p.ToolName)
		if len(p.Args) > 0 && m.blocks[i].ToolArgs == "" {
			m.blocks[i].ToolArgs = compactArgs(string(p.Args))
		}
		m.blocks[i].ToolStatus = "running"
	case "tool_execution_update":
		var p struct {
			ToolCallID    string `json:"toolCallId"`
			PartialResult struct {
				Content []pirpc.ContentBlock `json:"content"`
			} `json:"partialResult"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		i := m.ensureTool(p.ToolCallID, "")
		m.blocks[i].ToolResult = joinText(p.PartialResult.Content)
	case "tool_execution_end":
		var p struct {
			ToolCallID string `json:"toolCallId"`
			ToolName   string `json:"toolName"`
			Result     struct {
				Content []pirpc.ContentBlock `json:"content"`
			} `json:"result"`
			IsError bool `json:"isError"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		i := m.ensureTool(p.ToolCallID, p.ToolName)
		if p.IsError {
			m.blocks[i].ToolStatus = "error"
		} else {
			m.blocks[i].ToolStatus = "done"
		}
		m.blocks[i].ToolResult = joinText(p.Result.Content)
	case "agent_settled":
		m.thinking = false
		m.status = "ready"
		m.refresh()
		return m, tea.Batch(m.queryStats(), m.fetchCmdsOnce())
	case "agent_end":
		m.refresh()
		return m, nil
	case "extension_ui_request":
		return m.handleUIRequest(ev.Raw), nil
	case "queue_update":
		m.queue = pirpc.ParseQueue(ev.Raw)
	case "compaction_start":
		m.addBlock(Block{Kind: "notice", Text: "🗜️ compacting context…"})
	case "compaction_end":
		m.addBlock(Block{Kind: "notice", Text: "🗜️ context compacted"})
	case "auto_retry_start":
		m.addBlock(Block{Kind: "notice", Text: "🔁 provider error, retrying…"})
	case "auto_retry_end":
		var p struct {
			Success bool `json:"success"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		if !p.Success {
			m.addBlock(Block{Kind: "notice", Text: "🔁 retry failed", Err: true})
		}
	case "extension_error":
		var p struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		m.addBlock(Block{Kind: "notice", Text: "extension error: " + p.Error, Err: true})
	case "pi_exited":
		m.thinking = false
		if m.respawning {
			break // intentional reconnect, respawnMsg will follow
		}
		m.status = "pi has exited"
		m.addBlock(Block{Kind: "notice", Text: "pi has exited — Ctrl+C to close the TUI", Err: true})
	}
	m.refresh()
	return m, nil
}

func (m *model) applyDelta(raw []byte) {
	var mu pirpc.MessageUpdate
	if err := json.Unmarshal(raw, &mu); err != nil {
		return
	}
	d := mu.Event
	switch d.Type {
	case "text_start":
		m.ensureAsst()
	case "text_delta":
		m.blocks[m.ensureAsst()].Text += d.Delta
	case "thinking_start":
		m.ensureThink()
	case "thinking_delta":
		m.blocks[m.ensureThink()].Text += d.Delta
	case "toolcall_start":
		m.ensureTool(d.ID, d.ToolName)
	case "toolcall_end":
		if d.ToolCall != nil {
			i := m.ensureTool(d.ToolCall.ID, d.ToolCall.Name)
			m.blocks[i].ToolArgs = compactArgs(string(d.ToolCall.Arguments))
		}
	}
}

func (m *model) applyMessageEnd(raw []byte) {
	var env struct {
		Message pirpc.AgentMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return
	}
	msg := env.Message
	switch msg.Role {
	case "user":
		if t := pirpc.TextOf(msg.Content); strings.TrimSpace(t) != "" {
			m.addBlock(Block{Kind: "user", Text: t})
		}
	case "assistant":
		if msg.StopReason == "error" && msg.ErrorMessage != "" {
			m.addBlock(Block{Kind: "notice", Text: "pi error: " + msg.ErrorMessage, Err: true})
			break
		}
		for _, b := range pirpc.BlocksOf(msg.Content) {
			switch b.Type {
			case "text":
				if m.curAsst >= 0 && m.blocks[m.curAsst].Text == "" {
					m.blocks[m.curAsst].Text = b.Text
				} else if strings.TrimSpace(b.Text) != "" {
					m.curAsst = m.addBlock(Block{Kind: "assistant", Text: b.Text})
				}
			case "thinking":
				m.addBlock(Block{Kind: "thinking", Text: b.Thinking})
			case "toolCall":
				i := m.ensureTool(b.ID, b.Name)
				if m.blocks[i].ToolArgs == "" {
					m.blocks[i].ToolArgs = compactArgs(string(b.Arguments))
				}
			}
		}
	case "toolResult":
		text := joinTextBlocks(pirpc.BlocksOf(msg.Content))
		if text == "" {
			text = pirpc.TextOf(msg.Content)
		}
		if i, ok := m.tools[msg.ToolCallID]; ok {
			if m.blocks[i].ToolStatus == "running" {
				m.blocks[i].ToolStatus = "done"
			}
			if m.blocks[i].ToolResult == "" {
				m.blocks[i].ToolResult = text
			}
		} else if strings.TrimSpace(text) != "" {
			m.addBlock(Block{Kind: "tool", ToolName: msg.ToolName, ToolStatus: "done", ToolResult: text})
		}
	case "bashExecution":
		out := msg.Output
		if len(out) > 2000 {
			out = out[:2000] + "…"
		}
		m.addBlock(Block{Kind: "bash", Text: "$ " + msg.Command + "\n" + out})
	}
}

// restore converts get_messages into blocks (two-pass via m.tools map).
func (m *model) restore(msgs []pirpc.AgentMessage) {
	for _, msg := range msgs {
		switch msg.Role {
		case "user":
			if t := strings.TrimSpace(pirpc.TextOf(msg.Content)); t != "" {
				m.addBlock(Block{Kind: "user", Text: t})
			}
		case "assistant":
			for _, b := range pirpc.BlocksOf(msg.Content) {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) != "" {
						m.addBlock(Block{Kind: "assistant", Text: b.Text})
					}
				case "toolCall":
					m.ensureTool(b.ID, b.Name)
					idx := m.tools[b.ID]
					m.blocks[idx].ToolStatus = "done"
					m.blocks[idx].ToolArgs = compactArgs(string(b.Arguments))
				}
			}
			if t := strings.TrimSpace(pirpc.TextOf(msg.Content)); t != "" && len(pirpc.BlocksOf(msg.Content)) == 0 {
				m.addBlock(Block{Kind: "assistant", Text: t})
			}
		case "toolResult":
			text := joinTextBlocks(pirpc.BlocksOf(msg.Content))
			if text == "" {
				text = pirpc.TextOf(msg.Content)
			}
			if i, ok := m.tools[msg.ToolCallID]; ok {
				m.blocks[i].ToolResult = text
			}
		case "bashExecution":
			m.addBlock(Block{Kind: "bash", Text: "$ " + msg.Command})
		}
	}
	m.curAsst, m.curThink = -1, -1
}

func joinText(blocks []pirpc.ContentBlock) string {
	out := ""
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	if len(out) > 2000 {
		out = out[:2000] + "…"
	}
	return out
}

func joinTextBlocks(blocks []pirpc.ContentBlock) string { return joinText(blocks) }

// extension UI ---------------------------------------------------------------

func (m model) handleUIRequest(raw []byte) model {
	var req pirpc.UIRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return m
	}
	switch req.Method {
	case "select":
		opts := req.Options
		if len(opts) == 0 {
			opts = []string{"Agree", "Decline"}
		}
		d := &Dialog{
			ID: req.ID, Method: "select", Kind: "ui",
			Title:   orDefault(req.Title, "Pi requests permission"),
			Message: req.Message, Options: opts,
		}
		d.reindex()
		m.dialogs = append(m.dialogs, d)
	case "confirm":
		d := &Dialog{
			ID: req.ID, Method: "confirm", Kind: "ui",
			Title:   orDefault(req.Title, "Confirm"),
			Message: req.Message, Options: []string{"Allow", "Decline"},
		}
		d.reindex()
		m.dialogs = append(m.dialogs, d)
	case "input", "editor":
		// MVP: auto-cancel so the agent uses defaults/timeout
		_ = m.pi.Fire(pirpc.Command{Type: "extension_ui_response", ID: req.ID, Cancelled: boolPtr(true)})
	case "notify":
		m.addBlock(Block{Kind: "notice", Text: req.Message, Err: req.NotifyType == "error"})
	case "setStatus":
		m.extStat = stripANSI(req.StatusText)
	case "set_editor_text":
		m.ta.SetValue(req.Text)
	case "setWidget":
		// skipped: pi extension widgets don't render in this TUI
		_ = req
	}
	m.refresh()
	return m
}

func (m model) updateDialog(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.dialogs[0]
	n := len(d.FIdx)
	switch km.Type {
	case tea.KeyUp:
		if n > 0 {
			if d.Cursor > 0 {
				d.Cursor--
			} else {
				d.Cursor = n - 1
			}
		}
		return m, nil
	case tea.KeyDown:
		if n > 0 {
			if d.Cursor < n-1 {
				d.Cursor++
			} else {
				d.Cursor = 0
			}
		}
		return m, nil
	case tea.KeyBackspace:
		if (d.Kind == "model" || d.Kind == "thinking" || d.Kind == "secret") && d.Filter != "" {
			d.Filter = d.Filter[:len(d.Filter)-1]
			d.reindex()
		}
		return m, nil
	case tea.KeyEsc:
		if d.Kind == "ui" {
			m.answerDialog(d, -1)
		} else {
			m.dialogs = m.dialogs[1:]
			m.refresh()
		}
		return m, nil
	case tea.KeyEnter:
		return m.confirmDialog(d)
	}
	if km.Type == tea.KeyRunes {
		if d.Kind == "model" || d.Kind == "thinking" {
			// type to filter the picker
			d.Filter += km.String()
			d.reindex()
			return m, nil
		}
		if d.Kind == "secret" {
			d.Filter += km.String()
			return m, nil
		}
		s := strings.ToLower(km.String())
		if s == "y" && d.Method == "confirm" {
			m.answerDialog(d, 0)
			return m, nil
		}
		if s == "n" && d.Kind == "ui" {
			m.answerDialog(d, len(d.Options)-1)
			return m, nil
		}
	}
	return m, nil
}

// confirmDialog handles Enter per dialog kind.
func (m model) confirmDialog(d *Dialog) (tea.Model, tea.Cmd) {
	if d.Kind == "ui" {
		if len(d.FIdx) == 0 {
			return m, nil
		}
		m.answerDialog(d, d.FIdx[d.Cursor])
		return m, nil
	}
	if len(d.FIdx) == 0 {
		return m, nil
	}
	ri := d.FIdx[d.Cursor]
	switch d.Kind {
	case "model":
		prov, id := d.Providers[ri], d.Options[ri]
		m.dialogs = m.dialogs[1:]
		m.status = "switching model…"
		m.refresh()
		return m, func() tea.Msg {
			label, err := m.pi.SetModelByID(prov, id)
			return modelCycleMsg{label: label, err: err}
		}
	case "thinking":
		level := d.Options[ri]
		m.dialogs = m.dialogs[1:]
		m.status = "switching thinking…"
		m.refresh()
		return m, func() tea.Msg {
			if err := m.pi.SetLevel(level); err != nil {
				return settingsRefreshMsg{err: err}
			}
			return settingsRefreshMsg{notice: "thinking → " + level}
		}
	case "settings":
		return m.settingsAction(ri)
	case "login":
		prov := d.Options[ri]
		m.dialogs = m.dialogs[1:]
		m.openLoginMethod(prov, pirpc.LookupEnv(prov))
		return m, nil
	case "loginMethod":
		prov := d.LoginProvider
		switch ri {
		case 0: // enter API key
			env := d.LoginEnv
			m.dialogs[0] = &Dialog{Kind: "secret", Title: "API key — " + prov,
				Message:       "Save to " + env + " (gotui keystore, file 0600). Pi reconnects automatically.",
				LoginProvider: prov, LoginEnv: env}
			m.refresh()
			return m, nil
		case 1: // OAuth
			m.dialogs[0] = &Dialog{Kind: "loginOAuth", Title: "OAuth — " + prov,
				Message:       "1. Open another terminal\n2. Run: pi\n3. Type: /login " + prov + " then follow the steps\n4. Come back here and reload",
				Options:       []string{"Done — reload", "Close"},
				LoginProvider: prov}
			m.dialogs[0].reindex()
			m.refresh()
			return m, nil
		default: // reload models
			m.dialogs = m.dialogs[1:]
			m.status = "reloading models…"
			m.refresh()
			return m, func() tea.Msg {
				models, err := m.pi.GetModels()
				if err != nil {
					return settingsRefreshMsg{err: err}
				}
				return settingsRefreshMsg{notice: fmt.Sprintf("pi sees %d models", len(models))}
			}
		}
	case "loginOAuth":
		m.dialogs = m.dialogs[1:]
		if ri == 0 {
			m.status = "reloading models…"
			m.refresh()
			return m, func() tea.Msg {
				models, err := m.pi.GetModels()
				if err != nil {
					return settingsRefreshMsg{err: err}
				}
				return settingsRefreshMsg{notice: fmt.Sprintf("pi sees %d models", len(models))}
			}
		}
		m.refresh()
		return m, nil
	case "logout":
		prov := d.Options[ri]
		desc := descOf(d, ri)
		m.dialogs = m.dialogs[1:]
		return m, m.doLogout(prov, desc)
	case "secret":
		key := strings.TrimSpace(d.Filter)
		if key == "" {
			return m, nil
		}
		prov, env := d.LoginProvider, d.LoginEnv
		m.dialogs = m.dialogs[1:]
		m.refresh()
		return m, func() tea.Msg {
			return loginKeyMsg{provider: prov, env: env, key: key}
		}
	}
	return m, nil
}

func (m *model) answerDialog(d *Dialog, choice int) {
	m.dialogs = m.dialogs[1:]
	cmd := pirpc.Command{Type: "extension_ui_response", ID: d.ID}
	switch d.Method {
	case "select":
		if choice < 0 {
			cmd.Cancelled = boolPtr(true)
		} else {
			v := d.Options[choice]
			cmd.Value = &v
		}
	case "confirm":
		cmd.Confirmed = boolPtr(choice == 0)
	}
	_ = m.pi.Fire(cmd)
	m.refresh()
}

// reindex recomputes the visible list from Filter.
func (d *Dialog) reindex() {
	d.FIdx = d.FIdx[:0]
	f := strings.ToLower(d.Filter)
	for i := range d.Options {
		if f == "" || strings.Contains(strings.ToLower(d.Options[i]), f) ||
			(i < len(d.Descs) && strings.Contains(strings.ToLower(d.Descs[i]), f)) {
			d.FIdx = append(d.FIdx, i)
		}
	}
	if d.Cursor >= len(d.FIdx) {
		d.Cursor = 0
	}
}

// isBuiltinCmd detects built-in commands typed in the input (with optional arg).
func isBuiltinCmd(text string) (name, arg string, ok bool) {
	for _, b := range []string{"/model", "/thinking", "/tree", "/settings", "/login", "/logout", "/reload"} {
		if text == b {
			return b[1:], "", true
		}
		if rest, found := strings.CutPrefix(text, b+" "); found {
			return b[1:], strings.TrimSpace(rest), true
		}
	}
	return "", "", false
}

// runBuiltin runs a native built-in command.
func (m *model) runBuiltin(name, arg string) tea.Cmd {
	switch name {
	case "model":
		m.status = "loading models…"
		m.refresh()
		return func() tea.Msg {
			models, err := m.pi.GetModels()
			if err != nil {
				return pickerMsg{kind: "model", err: err}
			}
			opts := make([]string, 0, len(models))
			descs := make([]string, 0, len(models))
			provs := make([]string, 0, len(models))
			for _, mi := range models {
				opts = append(opts, mi.ID)
				descs = append(descs, mi.Name+" · "+mi.Provider)
				provs = append(provs, mi.Provider)
			}
			return pickerMsg{kind: "model", options: opts, descs: descs, providers: provs, current: m.modelLbl}
		}
	case "thinking":
		m.status = "loading thinking levels…"
		m.refresh()
		return func() tea.Msg {
			levels, err := m.pi.GetLevels()
			if err != nil {
				return pickerMsg{kind: "thinking", err: err}
			}
			return pickerMsg{kind: "thinking", options: levels}
		}
	case "tree":
		m.status = "loading session tree…"
		m.refresh()
		return func() tea.Msg {
			nodes, leaf, err := m.pi.GetTree()
			if err != nil {
				return treeMsg{err: err}
			}
			return treeMsg{text: renderTree(nodes, leaf)}
		}
	case "settings":
		m.status = "loading settings…"
		m.refresh()
		return m.loadSettings()
	case "reload":
		m.status = "reloading commands…"
		m.refresh()
		return func() tea.Msg {
			cmds, err := m.pi.GetCommands()
			return cmdsRefreshMsg{cmds: cmds, err: err, announce: true}
		}
	case "login":
		return m.openLogin(arg)
	case "logout":
		return m.openLogout(arg)
	}
	return nil
}

// openLogin opens the provider picker for /login [provider?].
func (m *model) openLogin(arg string) tea.Cmd {
	for _, p := range pirpc.ProviderEnvs {
		if strings.EqualFold(p.Provider, arg) || strings.EqualFold(p.Label, arg) {
			m.openLoginMethod(p.Provider, p.Env)
			return nil
		}
	}
	opts := make([]string, 0, len(pirpc.ProviderEnvs))
	descs := make([]string, 0, len(pirpc.ProviderEnvs))
	for _, p := range pirpc.ProviderEnvs {
		opts = append(opts, p.Provider)
		descs = append(descs, p.Label+" · "+p.Env)
	}
	d := &Dialog{Kind: "login", Title: "Provider login", Options: opts, Descs: descs}
	if arg != "" {
		d.Filter = arg
	}
	d.reindex()
	m.dialogs = append(m.dialogs, d)
	m.refresh()
	return nil
}

// openLoginMethod opens the method picker once a provider is chosen.
func (m *model) openLoginMethod(provider, env string) {
	d := &Dialog{
		Kind: "loginMethod", Title: "Login " + provider,
		Message:       "API key goes to gotui's private keystore (" + env + "), pi reconnects automatically. Do OAuth in stock pi.",
		Options:       []string{"Enter API key", "OAuth / subscription", "Logged in — reload"},
		Descs:         []string{"save key + reconnect pi", "guide", "refresh model list"},
		LoginProvider: provider, LoginEnv: env,
	}
	d.reindex()
	m.dialogs = append(m.dialogs, d)
	m.refresh()
}

// openLogout opens the provider picker to delete a saved key.
func (m *model) openLogout(arg string) tea.Cmd {
	keys := pirpc.LoadKeys(m.keyPath)
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
		return m.doLogout(opts[0], descs[0])
	}
	if len(opts) == 0 {
		m.addBlock(Block{Kind: "notice", Text: "keystore is empty — no keys saved"})
		m.refresh()
		return nil
	}
	d := &Dialog{Kind: "logout", Title: "Remove API key", Message: "Pick a provider to delete its key from the keystore.", Options: opts, Descs: descs}
	d.reindex()
	m.dialogs = append(m.dialogs, d)
	m.refresh()
	return nil
}

// doLogout deletes the key then reconnects pi to drop the credential.
func (m *model) doLogout(provider, desc string) tea.Cmd {
	env := pirpc.LookupEnv(provider)
	if env == "" {
		env = desc // fallback
	}
	if err := pirpc.DeleteKey(m.keyPath, env); err != nil {
		m.addBlock(Block{Kind: "notice", Text: "failed to delete key: " + err.Error(), Err: true})
		m.refresh()
		return nil
	}
	m.addBlock(Block{Kind: "notice", Text: "deleted key " + provider + " — reconnecting pi…"})
	return m.respawnPi()
}

// respawnPi kills the old pi and respawns keeping the same session (to pick up added/removed keys).
func (m *model) respawnPi() tea.Cmd {
	m.respawning = true
	m.status = "reconnecting pi…"
	m.refresh()
	opts := m.spawnOpts
	opts.Session = m.sessionFile
	if opts.NoSession {
		opts.Session = ""
	}
	old := m.pi
	return func() tea.Msg {
		old.Close()
		c, err := pirpc.Spawn(opts)
		if err != nil {
			return respawnMsg{err: err}
		}
		return respawnMsg{client: c}
	}
}

// pollCmds periodically reloads pi commands (auto-detects new ones).
func (m model) pollCmds() tea.Cmd {
	return tea.Tick(45*time.Second, func(time.Time) tea.Msg {
		cmds, err := m.pi.GetCommands()
		return cmdsRefreshMsg{cmds: cmds, err: err}
	})
}

func (m model) fetchCmdsOnce() tea.Cmd {
	return func() tea.Msg {
		cmds, err := m.pi.GetCommands()
		return cmdsRefreshMsg{cmds: cmds, err: err}
	}
}

// cmdSig is the command-list signature used to detect changes.
func cmdSig(cmds []pirpc.RepoCommand) string {
	parts := make([]string, 0, len(cmds))
	for _, c := range cmds {
		parts = append(parts, c.Source+"/"+c.Name)
	}
	return strings.Join(parts, "\n")
}

// loadSettings fetches current state to build the settings dialog.
func (m *model) loadSettings() tea.Cmd {
	return func() tea.Msg {
		st, err := m.pi.GetState()
		if err != nil {
			return settingsMsg{err: err}
		}
		if st.ThinkingLevel == "" {
			st.ThinkingLevel = "off"
		}
		return settingsMsg{st: SettingsState{
			Steering:    orDefault(st.SteeringMode, "one-at-a-time"),
			FollowUp:    orDefault(st.FollowUpMode, "one-at-a-time"),
			AutoCompact: st.AutoCompaction,
			AutoRetry:   m.autoRetry,
			Thinking:    st.ThinkingLevel,
			Model:       m.modelLbl,
		}}
	}
}

func onoff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func settingsOptions(st SettingsState) ([]string, []string) {
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
func (m model) settingsAction(ri int) (tea.Model, tea.Cmd) {
	if len(m.dialogs) == 0 {
		return m, nil
	}
	d := m.dialogs[0]
	st := d.Settings
	refresh := func() tea.Msg {
		s, err := m.pi.GetState()
		if err != nil {
			return settingsMsg{err: err}
		}
		return settingsMsg{st: SettingsState{
			Steering:    orDefault(s.SteeringMode, st.Steering),
			FollowUp:    orDefault(s.FollowUpMode, st.FollowUp),
			AutoCompact: s.AutoCompaction,
			AutoRetry:   m.autoRetry,
			Thinking:    orDefault(s.ThinkingLevel, st.Thinking),
			Model:       m.modelLbl,
		}}
	}
	switch ri {
	case 0: // model picker
		m.dialogs = m.dialogs[1:]
		m.refresh()
		return m, m.runBuiltin("model", "")
	case 1: // thinking picker
		m.dialogs = m.dialogs[1:]
		m.refresh()
		return m, m.runBuiltin("thinking", "")
	case 2:
		next := "all"
		if st.Steering == "all" {
			next = "one-at-a-time"
		}
		m.status = "switching steering…"
		m.refresh()
		return m, func() tea.Msg {
			if err := m.pi.SetSteering(next); err != nil {
				return settingsMsg{err: err}
			}
			return refresh()
		}
	case 3:
		next := "all"
		if st.FollowUp == "all" {
			next = "one-at-a-time"
		}
		m.status = "switching follow-up…"
		m.refresh()
		return m, func() tea.Msg {
			if err := m.pi.SetFollowUp(next); err != nil {
				return settingsMsg{err: err}
			}
			return refresh()
		}
	case 4:
		m.status = "switching auto-compact…"
		m.refresh()
		return m, func() tea.Msg {
			if err := m.pi.SetAutoCompact(!st.AutoCompact); err != nil {
				return settingsMsg{err: err}
			}
			return refresh()
		}
	case 5:
		m.autoRetry = !st.AutoRetry
		m.status = "switching auto-retry…"
		m.refresh()
		auto := m.autoRetry
		return m, func() tea.Msg {
			if err := m.pi.SetAutoRetry(auto); err != nil {
				m.autoRetry = !auto
				return settingsMsg{err: err}
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
				mark = " ◀ current"
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
		t := short(pirpc.TextOf(e.Message.Content), 60)
		extra := ""
		for _, bl := range pirpc.BlocksOf(e.Message.Content) {
			if bl.Type == "toolCall" {
				extra += " 🔧" + bl.Name
			}
		}
		if t == "" && extra != "" {
			return e.Message.Role + ":" + extra
		}
		return e.Message.Role + ": " + t + extra
	case "model_change":
		return "model → " + orDefault(e.ModelID, "?")
	case "thinking_level_change":
		return "thinking → " + orDefault(e.Level, "?")
	default:
		return e.Type + " " + shortID(e.ID)
	}
}

// command palette (/ autocomplete) --------------------------------------------

// cmdPrefix returns the text after / when the input is an unfinished command.
func (m *model) cmdPrefix() (string, bool) {
	v := m.ta.Value()
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \n") {
		return "", false
	}
	return v[1:], true
}

func (m *model) refreshCmds() {
	m.cmdItems = m.cmdItems[:0]
	if p, ok := m.cmdPrefix(); ok {
		pl := strings.ToLower(p)
		for i := range m.cmds {
			name := strings.ToLower(m.cmds[i].Name)
			if strings.HasPrefix(name, pl) || strings.Contains(name, pl) {
				m.cmdItems = append(m.cmdItems, i)
				if len(m.cmdItems) >= 8 {
					break
				}
			}
		}
	}
	m.cmdOpen = len(m.cmdItems) > 0
	if m.cmdCursor >= len(m.cmdItems) {
		m.cmdCursor = 0
	}
	m.applyPopupH()
}

func (m *model) popupH() int {
	if !m.cmdOpen {
		return 0
	}
	return len(m.cmdItems) + 3 // rows + footer + border
}

func (m *model) applyPopupH() {
	if !m.ready {
		return
	}
	h := m.baseVpH - m.popupH()
	if h < 3 {
		h = 3
	}
	if h != m.vp.Height {
		m.vp.Height = h
		m.vp.GotoBottom()
	}
}

func (m *model) exactCmdMatch() bool {
	p, ok := m.cmdPrefix()
	if !ok {
		return false
	}
	for _, c := range m.cmds {
		if strings.EqualFold(c.Name, p) {
			return true
		}
	}
	return false
}

// handleCmdKey handles keys while the command popup is open. true = key consumed.
func (m *model) handleCmdKey(km tea.KeyMsg) bool {
	switch km.Type {
	case tea.KeyUp:
		if m.cmdCursor > 0 {
			m.cmdCursor--
		} else {
			m.cmdCursor = len(m.cmdItems) - 1
		}
		return true
	case tea.KeyDown:
		if m.cmdCursor < len(m.cmdItems)-1 {
			m.cmdCursor++
		} else {
			m.cmdCursor = 0
		}
		return true
	case tea.KeyTab:
		m.completeCmd()
		return true
	case tea.KeyEsc:
		m.cmdOpen = false
		m.applyPopupH()
		m.refresh()
		return true
	case tea.KeyEnter:
		if m.exactCmdMatch() {
			return false // exact match sends immediately
		}
		m.completeCmd()
		return true
	}
	return false
}

func (m *model) completeCmd() {
	if !m.cmdOpen || len(m.cmdItems) == 0 {
		return
	}
	c := m.cmds[m.cmdItems[m.cmdCursor]]
	m.ta.SetValue("/" + c.Name + " ")
	m.refreshCmds()
	m.refresh()
}

func (m model) renderCmdPopup() string {
	mainW := m.mainW()
	var b strings.Builder
	for i, ci := range m.cmdItems {
		c := m.cmds[ci]
		row := "/" + c.Name
		if c.Description != "" {
			row += " — " + c.Description
		}
		row += " [" + c.Source + "]"
		row = short(row, mainW-8)
		if i == m.cmdCursor {
			b.WriteString("▸ " + cmdHiStyle.Render(row) + "\n")
		} else {
			b.WriteString("  " + statusBarStyle.Render(row) + "\n")
		}
	}
	b.WriteString(toolStyle.Render("Tab complete · Enter send · Esc close"))
	return cmdPopStyle.Width(mainW).Render(strings.TrimRight(b.String(), "\n"))
}

// render ----------------------------------------------------------------------

func (m *model) refresh() {
	if !m.ready {
		return
	}
	// only stick to bottom when already there — no jump while reading history
	follow := m.vp.AtBottom()
	m.vp.SetContent(m.renderBlocks())
	if follow {
		m.vp.GotoBottom()
	}
}

// refreshFollow rebuilds content and jumps to bottom (for new content worth seeing).
func (m *model) refreshFollow() {
	if !m.ready {
		return
	}
	m.vp.SetContent(m.renderBlocks())
	m.vp.GotoBottom()
}

func (m model) renderBlocks() string {
	var b strings.Builder
	w := m.vp.Width
	if len(m.blocks) == 0 && m.connErr == "" {
		empty := lipgloss.NewStyle().Foreground(cMuted).Width(w).
			Align(lipgloss.Center).
			Render("\n✦ Hi, pi is ready.\nAsk anything below to start.\n")
		b.WriteString(empty)
	}
	if m.connErr != "" {
		b.WriteString(errStyle.Render("⚠️ "+m.connErr) + "\n")
	}
	for _, bl := range m.blocks {
		switch bl.Kind {
		case "user":
			b.WriteString(userStyle.Width(w-2).Render(bl.Text) + "\n\n")
		case "assistant":
			b.WriteString(lipgloss.NewStyle().Foreground(cText).Width(w).Render(bl.Text) + "\n\n")
		case "thinking":
			t := bl.Text
			if len(t) > 300 {
				t = t[:300] + "…"
			}
			b.WriteString(toolStyle.Render("💭 "+short(t, 160)) + "\n\n")
		case "tool":
			icon := "▶"
			st := warnStyle
			switch bl.ToolStatus {
			case "done":
				icon, st = "✓", okStyle
			case "error":
				icon, st = "✗", errStyle
			}
			head := "🔧 " + bl.ToolName
			if bl.ToolArgs != "" {
				head += " " + bl.ToolArgs
			}
			b.WriteString(st.Render(icon+" "+short(head, 140)) + "\n")
			if r := strings.TrimSpace(bl.ToolResult); r != "" {
				b.WriteString(toolStyle.Render("  └ "+short(oneLineStr(r), 160)) + "\n")
			}
			b.WriteString("\n")
		case "bash":
			b.WriteString(codeStyle.Render(short(bl.Text, 400)) + "\n\n")
		case "tree":
			b.WriteString(codeStyle.Render(short(bl.Text, 3000)) + "\n\n")
		case "notice":
			if bl.Err {
				b.WriteString(errStyle.Render("⚠️ "+bl.Text) + "\n\n")
			} else {
				b.WriteString(toolStyle.Render(bl.Text) + "\n\n")
			}
		}
	}
	if m.thinking {
		b.WriteString(warnStyle.Render("● ") + statusBarStyle.Render(m.status) + "\n")
	}
	return b.String()
}

func (m model) renderSidebar() string {
	inner := sideW - 6
	var b strings.Builder
	dot := okStyle.Render("●")
	if m.thinking {
		dot = warnStyle.Render("●")
	}
	b.WriteString(sideTitleStyle.Render("SESSION") + "\n")
	b.WriteString(statusBarStyle.Render(short(m.session+" · "+m.cwd, inner)) + "\n")
	b.WriteString(dot + " " + statusBarStyle.Render(short(m.status, inner-2)) + "\n")
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render("MODEL") + "\n")
	b.WriteString(codeStyle.Render(short(m.modelLbl, inner)) + "\n")
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render("COMMANDS") + "\n")
	if len(m.cmds) == 0 {
		b.WriteString(toolStyle.Render("—") + "\n")
	} else {
		var ext, prm, skl, bin int
		var names []string
		for _, c := range m.cmds {
			switch c.Source {
			case "extension":
				ext++
			case "prompt":
				prm++
			case "skill":
				skl++
			case "builtin":
				bin++
			}
			if len(names) < 3 {
				names = append(names, "/"+c.Name)
			}
		}
		b.WriteString(statusBarStyle.Render(fmt.Sprintf("%d ext · %d prompt · %d skill · %d builtin", ext, prm, skl, bin)) + "\n")
		b.WriteString(toolStyle.Render(short(strings.Join(names, " "), inner)) + "\n")
	}
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render("STATS") + "\n")
	if m.stats.ContextPct > 0 {
		b.WriteString(statusBarStyle.Render(fmt.Sprintf("ctx %.0f%% · tok %s · $%.2f",
			m.stats.ContextPct, fmtNum(m.stats.TokensTotal), m.stats.Cost)) + "\n")
	} else if m.stats.Cost > 0 || m.stats.TokensTotal > 0 {
		b.WriteString(statusBarStyle.Render(fmt.Sprintf("tok %s · $%.2f",
			fmtNum(m.stats.TokensTotal), m.stats.Cost)) + "\n")
	} else {
		b.WriteString(toolStyle.Render("—") + "\n")
	}
	if len(m.queue.Steering)+len(m.queue.FollowUp) > 0 {
		b.WriteString(statusBarStyle.Render(fmt.Sprintf("queue: %d steer · %d follow",
			len(m.queue.Steering), len(m.queue.FollowUp))) + "\n")
	}
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render("ACTIVITY") + "\n")
	n := 0
	for i := len(m.blocks) - 1; i >= 0 && n < 8; i-- {
		bl := m.blocks[i]
		if bl.Kind != "tool" {
			continue
		}
		icon := "▶"
		switch bl.ToolStatus {
		case "done":
			icon = "✓"
		case "error":
			icon = "✗"
		}
		b.WriteString(statusBarStyle.Render(short(icon+" "+bl.ToolName, inner)) + "\n")
		n++
	}
	if n == 0 {
		b.WriteString(toolStyle.Render("— none yet —") + "\n")
	}
	return sideStyle.Width(sideW).Height(m.sideH()).Render(b.String())
}

func sep() string {
	return sepStyle.Render(strings.Repeat("─", sideW-8))
}

func (m model) sideH() int {
	// sidebar box = body = winH-2 → content = body-2 (minus border)
	h := m.winH - 4
	if h < 6 {
		h = 6
	}
	return h
}

func (m model) renderHeader() string {
	left := "✦ gotui × pi"
	right := badgeStyle.Render(short(m.modelLbl, 40))
	if m.thinking {
		right += " " + warnStyle.Render("●")
	}
	gap := m.winW - lipgloss.Width(left) - lipgloss.Width(right) - 4
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Width(m.winW).Render(left + strings.Repeat(" ", gap) + right)
}

func (m model) renderInput() string {
	mainW := m.mainW()
	style := inputFocusStyle
	if m.thinking || len(m.dialogs) > 0 {
		style = inputStyle
	}
	foot := statusBarStyle.Render("↵ send · / commands · ^P model · ^C quit")
	if m.thinking {
		foot = warnStyle.Render("↵ steer · Esc cancel")
	}
	if m.extStat != "" {
		foot += "  " + codeStyle.Render(short(m.extStat, 40))
	}
	return style.Width(mainW).Render(m.ta.View() + "\n" + foot)
}

func (m model) renderDialog() string {
	d := m.dialogs[0]
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cAccent).Render(d.Title) + "\n")
	if d.Message != "" {
		b.WriteString(statusBarStyle.Render(d.Message) + "\n")
	}
	if d.Kind == "model" || d.Kind == "thinking" {
		b.WriteString(statusBarStyle.Render("filter: "+d.Filter+"▌") + "\n")
	}
	if d.Kind == "secret" {
		b.WriteString("\n")
		b.WriteString(cmdHiStyle.Render(strings.Repeat("•", len(d.Filter))+"▌") + "\n")
		b.WriteString("\n" + toolStyle.Render("Enter save · Esc cancel"))
	} else {
		b.WriteString("\n")
		// 12-row scroll window following the cursor
		const win = 12
		total := len(d.FIdx)
		start := d.Cursor - 4
		if start < 0 {
			start = 0
		}
		if start+win > total {
			start = total - win
		}
		if start < 0 {
			start = 0
		}
		end := start + win
		if end > total {
			end = total
		}
		if start > 0 {
			b.WriteString(toolStyle.Render(fmt.Sprintf("…(+%d above)", start)) + "\n")
		}
		for fi := start; fi < end; fi++ {
			ri := d.FIdx[fi]
			cursor := "  "
			style := statusBarStyle
			if fi == d.Cursor {
				cursor = "▸ "
				style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
			}
			row := short(d.Options[ri], 34)
			if desc := descOf(d, ri); desc != "" {
				row += "  " + toolStyle.Render("— "+short(desc, 40))
			}
			b.WriteString(cursor + style.Render(row) + "\n")
		}
		if end < total {
			b.WriteString(toolStyle.Render(fmt.Sprintf("…(+%d below)", total-end)) + "\n")
		}
		if total == 0 {
			b.WriteString(toolStyle.Render("— no match —") + "\n")
		}
	}
	foot := "↑↓ select · Enter confirm · Esc cancel"
	if d.Kind == "model" || d.Kind == "thinking" {
		foot = "type to filter · " + foot
	} else if d.Kind == "settings" {
		foot = "↑↓ select · Enter change · Esc close"
	}
	b.WriteString("\n" + toolStyle.Render(foot))
	box := dlgStyle.Width(62).Render(b.String())
	hint := ""
	if len(m.dialogs) > 1 {
		hint = statusBarStyle.Render(fmt.Sprintf("(%d more dialogs pending)", len(m.dialogs)-1))
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center, box),
		hint,
	)
}

func descOf(d *Dialog, ri int) string {
	if ri < len(d.Descs) {
		return d.Descs[ri]
	}
	return ""
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	if len(m.dialogs) > 0 {
		return m.renderDialog()
	}
	left := lipgloss.JoinVertical(lipgloss.Left, m.vp.View(), m.renderInput())
	if m.cmdOpen {
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.vp.View(),
			m.renderCmdPopup(),
			m.renderInput(),
		)
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", m.renderSidebar())
	bar := statusBarStyle.Render(fmt.Sprintf(" gotui × pi · %s ", m.status))
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, bar)
}

// utils ------------------------------------------------------------------------

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func short(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func oneLineStr(s string) string { return strings.ReplaceAll(s, "\n", " ⏎ ") }

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func boolPtr(b bool) *bool { return &b }

// stripANSI strips ANSI color codes (e.g. pi-lens extension status).
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for i := 0; i < len(s); i++ {
		if !in && s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			in = true
			i++
			continue
		}
		if in {
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
				in = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return strings.TrimSpace(b.String())
}

func fmtNum(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// main --------------------------------------------------------------------------

// progRef lets respawnMsg rewire the event stream (tea.Program.Send is thread-safe).
var progRef *tea.Program

func main() {
	cont := flag.Bool("c", false, "resume the most recent pi session")
	provider := flag.String("provider", "", "pi provider (default from ~/.pi)")
	modelFlag := flag.String("model", "", "pi model (default from ~/.pi)")
	noSession := flag.Bool("no-session", false, "don't persist session")
	flag.Parse()

	// inject saved API keys (/login) into env for the pi child to inherit
	keyPath := pirpc.KeyPath()
	for env, key := range pirpc.LoadKeys(keyPath) {
		if key != "" && os.Getenv(env) == "" {
			_ = os.Setenv(env, key)
		}
	}

	cwd, _ := os.Getwd()
	opts := pirpc.Options{
		Provider: *provider, Model: *modelFlag,
		Continue: *cont, NoSession: *noSession,
	}
	pi, err := pirpc.Spawn(opts)
	if err != nil {
		fmt.Println("Cannot start pi:", err)
		os.Exit(1)
	}
	defer pi.Close()

	m := initialModel(pi, cwd)
	m.spawnOpts = opts
	m.keyPath = keyPath
	prog := tea.NewProgram(m, tea.WithAltScreen())
	progRef = prog
	pi.OnEvent = func(e pirpc.Event) { prog.Send(piEventMsg{e}) }
	if _, err := prog.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
