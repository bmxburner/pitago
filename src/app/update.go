package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"openpi/src/extension"
	"openpi/src/pirpc"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// dialog captures all keys while open
	if len(m.Dialogs) > 0 {
		if km, ok := msg.(tea.KeyMsg); ok {
			return m.updateDialog(km)
		}
		// Async results stay swallowed while a dialog is open — except
		// paste (Ctrl+V into the /login key field must land).
		switch msg.(type) {
		case tea.WindowSizeMsg, pasteDoneMsg:
		default:
			return m, nil
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.winW, m.winH = msg.Width, msg.Height
		mainW := m.mainW()
		// Layout fits winH exactly: header(1) + viewport + input(6 + tray:
		// textarea 3 + footer 1 + border 2 + image chips 0/1).
		vpH := msg.Height - 7 - m.chipH()
		if vpH < 5 {
			vpH = 5
		}
		m.baseVpH = vpH
		if !m.ready {
			m.vp = viewport.New(mainW, vpH)
			m.sideVp = viewport.New(sideInnerW, m.sideContentH())
			m.ready = true
		} else {
			m.vp.Width = mainW
			m.vp.Height = vpH
			m.sideVp.Width = sideInnerW
			m.sideVp.Height = m.sideContentH()
		}
		m.ta.SetWidth(mainW - 6)
		m.Refresh()
		return m, nil

	case connectedMsg:
		if msg.err != nil {
			m.connErr = msg.err.Error()
			m.Status = "cannot connect to pi"
			return m, nil
		}
		m.ModelLbl = msg.state.Model.ID
		if m.ModelLbl == "" {
			m.ModelLbl = msg.state.Model.Name
		}
		m.thinkLvl = msg.state.ThinkingLevel
		m.autoCompact = msg.state.AutoCompaction
		m.ctxWindow = msg.state.Model.ContextWindow
		m.sessStart = time.Now()
		m.pushRecent(msg.state.Model.Provider, m.ModelLbl, m.ModelLbl)
		if msg.state.SessionName != "" {
			m.session = msg.state.SessionName
		} else {
			m.session = ShortID(msg.state.SessionID)
		}
		m.Stats = msg.stats
		m.Cmds = append(BuiltinRepo(m.builtins), msg.cmds...)
		m.Todos = restoreTodos(msg.msgs)
		m.MCP = getMcpServers()
		m.sessionFile = msg.state.SessionFile
		m.blocks = nil
		m.tools = make(map[string]int)
		m.restore(msg.msgs)
		m.Status = "ready"
		m.RefreshFollow()
		return m, nil

	case piEventMsg:
		return m.handleEvent(msg.Event)

	case statsMsg:
		if msg.err == nil {
			m.Stats = msg.stats
			if m.ctxWindow == 0 {
				m.ctxWindow = msg.stats.ContextWin
			}
			if m.pendSpeed {
				m.pendSpeed = false
				if !m.turnStart.IsZero() {
					d := time.Since(m.turnStart)
					m.lastDur = d.Round(time.Second)
					if secs := d.Seconds(); secs > 0 {
						if delta := msg.stats.Out - m.turnOutBase; delta > 0 {
							m.lastSpeed = float64(delta) / secs
						} else {
							m.lastSpeed = 0
						}
					}
				}
			}
			m.Refresh()
		}
		return m, nil

	case stateRefreshMsg:
		if msg.err == nil {
			lbl := msg.state.Model.ID
			if lbl == "" {
				lbl = msg.state.Model.Name
			}
			if lbl != "" {
				m.ModelLbl = lbl
			}
			m.thinkLvl = msg.state.ThinkingLevel
			m.autoCompact = msg.state.AutoCompaction
			if msg.state.Model.ContextWindow > 0 {
				m.ctxWindow = msg.state.Model.ContextWindow
			}
			m.pushRecent(msg.state.Model.Provider, m.ModelLbl, m.ModelLbl)
			m.Refresh()
		}
		return m, nil

	case wsTickMsg:
		return m, tea.Batch(m.wsRefresh(), m.pollWs())

	case wsMsg:
		m.ws = msg.data
		m.MCP = getMcpServers()
		m.Refresh()
		return m, nil

	case sentAckMsg:
		if msg.err != nil {
			m.AddBlock(Block{Kind: "notice", Text: msg.err.Error(), Err: true})
			m.thinking = false
			m.Status = "ready"
			m.Refresh()
		}
		return m, nil

	case pasteDoneMsg:
		m.applyPaste(msg)
		return m, nil

	case petTickMsg:
		// 500ms loop while busy/flashing: face animation + elapsed counter.
		if m.pet.status.Busy() || m.pet.status.Flashing() {
			m.pet.tick++
			m.Refresh()
			return m, petTickCmd()
		}
		m.pet.ticking = false
		return m, nil

	case petFlashMsg:
		// Stale timers (an older flash generation) must not cut a new status.
		if msg.gen == m.pet.gen && (m.pet.status == petSuccess || m.pet.status == petError) {
			m.pet.status = petIdle
			m.Refresh()
		}
		return m, nil

	case SessionResetMsg:
		if msg.Err != nil {
			m.AddBlock(Block{Kind: "notice", Text: msg.Err.Error(), Err: true})
		}
		m.blocks = nil
		m.tools = make(map[string]int)
		m.curAsst, m.curThink = -1, -1
		m.asstDelta, m.thinkDelta = false, false
		m.thinking = false
		m.pet = petState{}
		m.Status = "ready"
		m.Todos = nil
		m.imgAtts = nil // pending chips belong to the old session
		m.trayFocus = false
		m.applyPopupH()
		m.sessStart = time.Now()
		m.turnStart = time.Time{}
		m.pendSpeed = false
		m.lastDur, m.lastSpeed = 0, 0
		m.RefreshFollow()
		return m, m.queryStats()

	case ModelCycleMsg:
		if msg.Err != nil {
			m.Status = "ready"
			m.AddBlock(Block{Kind: "notice", Text: "model switch failed: " + msg.Err.Error(), Err: true})
		} else {
			m.ModelLbl = msg.Label
			id := msg.ID
			if id == "" {
				id = msg.Label
			}
			m.pushRecent(msg.Provider, id, msg.Label)
			m.Status = "ready"
			m.AddBlock(Block{Kind: "notice", Text: "model switched → " + msg.Label})
		}
		m.Refresh()
		return m, nil

	case PickerMsg:
		m.Status = "ready"
		if msg.Err != nil {
			m.AddBlock(Block{Kind: "notice", Text: "failed to load list: " + msg.Err.Error(), Err: true})
			m.Refresh()
			return m, nil
		}
		title := "Select model"
		if msg.Kind == "thinking" {
			title = "Thinking level"
		}
		d := &Dialog{Kind: msg.Kind, Title: title, Options: msg.Options, Descs: msg.Descs, Providers: msg.Providers}
		// preselect the current value
		for i, o := range d.Options {
			if o == msg.Current {
				d.Cursor = i
				break
			}
		}
		d.Reindex()
		// keep cursor after reindex
		for i, ri := range d.FIdx {
			if d.Options[ri] == msg.Current {
				d.Cursor = i
				break
			}
		}
		m.Dialogs = append(m.Dialogs, d)
		m.Refresh()
		return m, nil

	case SettingsMsg:
		m.Status = "ready"
		if msg.Err != nil {
			m.AddBlock(Block{Kind: "notice", Text: "settings error: " + msg.Err.Error(), Err: true})
			m.Refresh()
			return m, nil
		}
		opts, descs := msg.Opts, msg.Descs
		if len(m.Dialogs) > 0 && m.Dialogs[0].Kind == "settings" {
			d := m.Dialogs[0]
			d.Options, d.Descs, d.Settings = opts, descs, msg.St
			d.Reindex()
		} else {
			d := &Dialog{Kind: "settings", Title: "Agent settings", Options: opts, Descs: descs, Settings: msg.St}
			d.Reindex()
			m.Dialogs = append(m.Dialogs, d)
		}
		m.Refresh()
		return m, nil

	case TreeMsg:
		m.Status = "ready"
		if msg.Err != nil {
			m.AddBlock(Block{Kind: "notice", Text: "tree error: " + msg.Err.Error(), Err: true})
		} else {
			m.AddBlock(Block{Kind: "tree", Text: msg.Text})
		}
		m.Refresh()
		return m, nil

	case SettingsRefreshMsg:
		if msg.Err != nil {
			m.AddBlock(Block{Kind: "notice", Text: msg.Err.Error(), Err: true})
		} else if msg.Notice != "" {
			m.AddBlock(Block{Kind: "notice", Text: msg.Notice})
		}
		m.Status = "ready"
		m.Refresh()
		return m, nil

	case LoginKeyMsg:
		if err := pirpc.SaveKey(m.KeyPath, msg.Env, msg.Key); err != nil {
			m.AddBlock(Block{Kind: "notice", Text: "failed to save key: " + err.Error(), Err: true})
			m.Refresh()
			return m, nil
		}
		m.AddBlock(Block{Kind: "notice", Text: "saved key " + msg.Provider + " — reconnecting pi…"})
		return m, m.RespawnPi()

	case respawnMsg:
		m.respawning = false
		if msg.err != nil {
			m.Status = "ready"
			m.AddBlock(Block{Kind: "notice", Text: "pi reconnect failed: " + msg.err.Error() + " — restart the TUI", Err: true})
			m.Refresh()
			return m, nil
		}
		m.Pi = msg.client
		msg.client.OnEvent = func(e pirpc.Event) { ProgRef.Send(piEventMsg{e}) }
		m.Status = "reloading…"
		m.Refresh()
		return m, m.fetchAll()

	case CmdsRefreshMsg:
		if msg.Err == nil {
			merged := append(BuiltinRepo(m.builtins), msg.Cmds...)
			if cmdSig(merged) != cmdSig(m.Cmds) {
				if msg.Announce {
					old := make(map[string]bool, len(m.Cmds))
					for _, c := range m.Cmds {
						old[c.Source+"/"+c.Name] = true
					}
					var added []string
					for _, c := range merged {
						if !old[c.Source+"/"+c.Name] {
							added = append(added, "/"+c.Name)
						}
					}
					if len(added) == 0 {
						m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("no new commands (total %d)", len(merged))})
					} else {
						m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("new commands: %s (total %d)", strings.Join(added, ", "), len(merged))})
					}
				}
				m.Cmds = merged
				m.refreshCmds()
				m.refreshAt()
				m.Status = "ready"
				m.Refresh()
			} else if msg.Announce {
				m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("no new commands (total %d)", len(m.Cmds))})
				m.Status = "ready"
				m.Refresh()
			}
		} else if msg.Announce {
			m.AddBlock(Block{Kind: "notice", Text: "failed to load commands: " + msg.Err.Error(), Err: true})
			m.Status = "ready"
			m.Refresh()
		}
		return m, m.pollCmds()

	case tea.MouseMsg:
		// Left-click a sidebar recent model to switch to it.
		if msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft {
			if idx, ok := m.recentAt(msg.X, msg.Y); ok {
				r := m.recentModels[idx]
				if r.ID == m.ModelLbl || r.DispLabel() == m.ModelLbl {
					return m, nil // already current
				}
				return m, m.SwitchToRecent(idx)
			}
		}
		// else: fall through so viewport/textarea get wheel-scroll etc.

	case tea.KeyMsg:
		// Alt+1..5: jump straight to a recent model (best-effort per terminal).
		if msg.Alt && len(msg.Runes) == 1 {
			if n := int(msg.Runes[0] - '1'); n >= 0 && n < len(m.recentModels) && n < maxRecent {
				return m, m.SwitchToRecent(n)
			}
		}
		// Alt+↑↓ PgUp PgDn Home End: scroll sidebar without a mouse.
		// Wheel needs --mouse, so this is the only scroll path by default.
		if msg.Alt {
			switch msg.Type {
			case tea.KeyUp, tea.KeyDown, tea.KeyPgUp, tea.KeyPgDown, tea.KeyHome, tea.KeyEnd:
				if m.showSide() {
					km := msg
					km.Alt = false // viewport KeyMap matches "up", not "alt+up"
					var c tea.Cmd
					m.sideVp, c = m.sideVp.Update(km)
					return m, c
				}
			}
		}
		if m.atOpen && m.handleAtKey(msg) {
			return m, nil
		}
		if m.cmdOpen && m.handleCmdKey(msg) {
			return m, nil
		}
		// trayFocus: nav keys stay in the tray, everything else exits it
		// and processes normally (typing lands in the input, Enter sends).
		if m.trayFocus {
			if cmd, done := m.handleTrayKey(msg); done {
				return m, cmd
			}
			m.exitTray()
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyCtrlB:
			m.ToggleSide()
			return m, nil
		case tea.KeyCtrlY:
			return m, m.YankLast()
		case tea.KeyCtrlV:
			// Owned here (not the textarea): multi-backend read + visible
			// errors instead of the silent built-in paste.
			return m, m.pasteCmd(false)
		case tea.KeyDown:
			// Last input line + tray → cursor moves into the [Image N] row.
			if len(m.imgAtts) > 0 && m.onLastLine() {
				m.enterTray()
				return m, nil
			}
		case tea.KeyBackspace:
			// Empty input + tray → pop the last [Image N] chip.
			if m.ta.Value() == "" && len(m.imgAtts) > 0 {
				m.imgAtts = m.imgAtts[:len(m.imgAtts)-1]
				m.applyPopupH()
				m.Refresh()
				return m, nil
			}
		case tea.KeyCtrlO:
			return m, m.OpenYank()
		case tea.KeyCtrlG:
			m.expandTools = !m.expandTools
			m.RefreshFollow()
			return m, nil
		case tea.KeyCtrlR:
			return m, m.OpenRecents()
		case tea.KeyCtrlN:
			m.Status = "opening new session…"
			m.Refresh()
			return m, func() tea.Msg {
				return SessionResetMsg{Err: m.Pi.NewSession()}
			}
		case tea.KeyCtrlP:
			m.Status = "switching model…"
			m.Refresh()
			return m, func() tea.Msg {
				label, err := m.Pi.CycleModel()
				return ModelCycleMsg{Label: label, Err: err}
			}
		case tea.KeyEsc:
			if m.thinking {
				m.Status = "cancelling…"
				m.Refresh()
				return m, func() tea.Msg {
					steer, follow, _ := m.Pi.ClearQueue()
					restored := append(steer, follow...)
					if len(restored) > 0 {
						_ = restored // returned text, shown as notice for brevity
					}
					_, err := m.Pi.Abort()
					return sentAckMsg{err: err}
				}
			}
			return m, nil
		case tea.KeyEnter:
			return m, m.submitInput()
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
	if km, ok := msg.(tea.KeyMsg); ok && km.Paste {
		m.collectDrops() // terminal drop/paste: long paths → [Image N] chips
	}
	if m.ready {
		// wheel over the sidebar scrolls it, not the chat
		if mm, ok := msg.(tea.MouseMsg); ok && mm.Action == tea.MouseActionPress &&
			(mm.Button == tea.MouseButtonWheelUp || mm.Button == tea.MouseButtonWheelDown) &&
			m.overSide(mm.X) {
			m.sideVp, cmd = m.sideVp.Update(msg)
		} else {
			m.vp, cmd = m.vp.Update(msg)
		}
		cmds = append(cmds, cmd)
	}
	m.refreshCmds()
	m.refreshAt()
	m.Refresh()
	return m, tea.Batch(cmds...)
}

func (m Model) handleEvent(ev pirpc.Event) (tea.Model, tea.Cmd) {
	var pcmd tea.Cmd
	switch ev.Type {
	case "agent_start":
		m.thinking = true
		m.Status = "pi is running…"
		pcmd = m.petAnchor()
	case "turn_start":
		m.curAsst, m.curThink = -1, -1
		m.asstDelta, m.thinkDelta = false, false
		m.turnStart = time.Now()
		m.turnOutBase = m.Stats.Out
	case "message_update":
		pcmd = m.applyDelta(ev.Raw)
	case "message_end":
		pcmd = m.applyMessageEnd(ev.Raw)
		m.curAsst, m.curThink = -1, -1
		m.asstDelta, m.thinkDelta = false, false
	case "tool_execution_start":
		var p struct {
			ToolCallID string          `json:"toolCallId"`
			ToolName   string          `json:"toolName"`
			Args       json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		i := m.ensureTool(p.ToolCallID, p.ToolName)
		m.setToolArgs(i, p.ToolName, string(p.Args))
		m.blocks[i].ToolStatus = "running"
		if isTodoTool(p.ToolName) {
			m.updateTodosFromRaw(p.Args, rawField(ev.Raw, "details"), rawField(ev.Raw, "input"))
		}
		pcmd = m.petSet(petWorking)
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
				Details json.RawMessage      `json:"details"`
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
		if m.blocks[i].ToolResult == "" {
			m.blocks[i].ToolResult = diffOfDetails(p.Result.Details)
		}
		if isTodoTool(p.ToolName) {
			m.updateTodosFromRaw(rawField(ev.Raw, "details"), rawField(ev.Raw, "result"))
		}
	case "agent_settled":
		m.thinking = false
		m.Status = "ready"
		m.pendSpeed = true
		m.MCP = getMcpServers()
		m.Refresh()
		return m, tea.Batch(m.queryStats(), m.fetchCmdsOnce(), m.fetchStateOnce(), m.wsRefresh(), m.petSettled())
	case "agent_end":
		m.Refresh()
		return m, nil
	case "extension_ui_request":
		return m.handleUIRequest(ev.Raw), nil
	case "queue_update":
		m.queue = pirpc.ParseQueue(ev.Raw)
	case "compaction_start":
		m.AddBlock(Block{Kind: "notice", Text: "compacting context…"})
	case "compaction_end":
		m.AddBlock(Block{Kind: "notice", Text: "context compacted"})
	case "auto_retry_start":
		m.AddBlock(Block{Kind: "notice", Text: "provider error, retrying…"})
	case "auto_retry_end":
		var p struct {
			Success bool `json:"success"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		if !p.Success {
			m.AddBlock(Block{Kind: "notice", Text: "retry failed", Err: true})
		}
	case "extension_error":
		var p struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(ev.Raw, &p)
		m.AddBlock(Block{Kind: "notice", Text: "extension error: " + p.Error, Err: true})
	case "pi_exited":
		m.thinking = false
		m.pet = petState{}
		if m.respawning {
			break // intentional reconnect, respawnMsg will follow
		}
		m.Status = "pi has exited"
		m.AddBlock(Block{Kind: "notice", Text: "pi has exited — Ctrl+C to close the TUI", Err: true})
	}
	m.Refresh()
	return m, pcmd
}

func (m *Model) applyDelta(raw []byte) tea.Cmd {
	var mu pirpc.MessageUpdate
	if err := json.Unmarshal(raw, &mu); err != nil {
		return nil
	}
	d := mu.Event
	switch d.Type {
	case "text_start":
		m.ensureAsst()
		if m.pet.inTurn {
			return m.petSet(petWriting)
		}
	case "text_delta":
		m.asstDelta = true
		m.blocks[m.ensureAsst()].Text += d.Delta
		if m.pet.inTurn {
			return m.petSet(petWriting)
		}
	case "thinking_start":
		m.ensureThink()
		if m.pet.inTurn {
			return m.petSet(petThinking)
		}
	case "thinking_delta":
		m.thinkDelta = true
		m.blocks[m.ensureThink()].Text += d.Delta
		if m.pet.inTurn {
			return m.petSet(petThinking)
		}
	case "toolcall_start":
		m.ensureTool(d.ID, d.ToolName)
		if m.pet.inTurn {
			return m.petSet(petWorking)
		}
	case "toolcall_end":
		if d.ToolCall != nil {
			i := m.ensureTool(d.ToolCall.ID, d.ToolCall.Name)
			m.setToolArgs(i, d.ToolCall.Name, string(d.ToolCall.Arguments))
		}
		if m.pet.inTurn {
			return m.petSet(petWorking)
		}
	case "done":
		// stop → the model is done for good; any other reason means
		// more rounds are coming (tools already flipped us to working).
		if d.Reason == "stop" {
			m.pet.sawStop = true
			if m.pet.inTurn {
				return m.petSet(petSuccess)
			}
		}
	case "error":
		m.pet.inTurn = false
		return m.petSet(petError)
	}
	return nil
}

func (m *Model) applyMessageEnd(raw []byte) tea.Cmd {
	var env struct {
		Message pirpc.AgentMessage `json:"message"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	msg := env.Message
	switch msg.Role {
	case "user":
		if t := withImages(pirpc.TextOf(msg.Content), pirpc.ImageCount(msg.Content)); strings.TrimSpace(t) != "" {
			m.AddBlock(Block{Kind: "user", Text: t})
		}
	case "assistant":
		if msg.StopReason == "error" && msg.ErrorMessage != "" {
			m.AddBlock(Block{Kind: "notice", Text: "pi error: " + msg.ErrorMessage, Err: true})
			m.pet.inTurn = false
			return m.petSet(petError)
		}
		for _, b := range pirpc.BlocksOf(msg.Content) {
			switch b.Type {
			case "text":
				if m.asstDelta {
					break // already streamed — re-adding would duplicate
				}
				if m.curAsst >= 0 && m.blocks[m.curAsst].Text == "" {
					m.blocks[m.curAsst].Text = b.Text
				} else if strings.TrimSpace(b.Text) != "" {
					m.curAsst = m.AddBlock(Block{Kind: "assistant", Text: b.Text})
				}
			case "thinking":
				if m.thinkDelta {
					break // already streamed — re-adding would duplicate
				}
				if m.curThink >= 0 && m.blocks[m.curThink].Text == "" {
					m.blocks[m.curThink].Text = b.Thinking
				} else if strings.TrimSpace(b.Thinking) != "" {
					m.curThink = m.AddBlock(Block{Kind: "thinking", Text: b.Thinking})
				}
			case "toolCall":
				i := m.ensureTool(b.ID, b.Name)
				m.setToolArgs(i, b.Name, string(b.Arguments))
			}
		}
	case "toolResult":
		text := joinTextBlocks(pirpc.BlocksOf(msg.Content))
		if text == "" {
			text = pirpc.TextOf(msg.Content)
		}
		if text == "" {
			text = diffOfDetails(msg.Details)
		}
		if isTodoTool(msg.ToolName) {
			m.updateTodosFromRaw(msg.Content)
			if strings.HasPrefix(strings.TrimSpace(text), "{") || strings.HasPrefix(strings.TrimSpace(text), "[") {
				m.updateTodosFromRaw(json.RawMessage(strings.TrimSpace(text)))
			}
		}
		if i, ok := m.tools[msg.ToolCallID]; ok {
			if m.blocks[i].ToolStatus == "running" {
				m.blocks[i].ToolStatus = "done"
			}
			if m.blocks[i].ToolResult == "" {
				m.blocks[i].ToolResult = text
			}
		} else if strings.TrimSpace(text) != "" {
			m.AddBlock(Block{Kind: "tool", ToolName: msg.ToolName, ToolStatus: "done", ToolResult: text})
		}
	case "bashExecution":
		out := msg.Output
		if len(out) > 2000 {
			out = out[:2000] + "…"
		}
		m.AddBlock(Block{Kind: "bash", Text: "$ " + msg.Command + "\n" + out})
	}
	return nil
}

// restore converts get_messages into blocks (two-pass via m.tools map).

func (m *Model) restore(msgs []pirpc.AgentMessage) {
	for _, msg := range msgs {
		switch msg.Role {
		case "user":
			if t := withImages(strings.TrimSpace(pirpc.TextOf(msg.Content)), pirpc.ImageCount(msg.Content)); t != "" {
				m.AddBlock(Block{Kind: "user", Text: t})
			}
		case "assistant":
			for _, b := range pirpc.BlocksOf(msg.Content) {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) != "" {
						m.AddBlock(Block{Kind: "assistant", Text: b.Text})
					}
				case "toolCall":
					m.ensureTool(b.ID, b.Name)
					idx := m.tools[b.ID]
					m.blocks[idx].ToolStatus = "done"
					m.setToolArgs(idx, b.Name, string(b.Arguments))
				}
			}
			if t := strings.TrimSpace(pirpc.TextOf(msg.Content)); t != "" && len(pirpc.BlocksOf(msg.Content)) == 0 {
				m.AddBlock(Block{Kind: "assistant", Text: t})
			}
		case "toolResult":
			text := joinTextBlocks(pirpc.BlocksOf(msg.Content))
			if text == "" {
				text = pirpc.TextOf(msg.Content)
			}
			if text == "" {
				text = diffOfDetails(msg.Details)
			}
			if i, ok := m.tools[msg.ToolCallID]; ok {
				m.blocks[i].ToolResult = text
			}
		case "bashExecution":
			m.AddBlock(Block{Kind: "bash", Text: "$ " + msg.Command})
		}
	}
	m.curAsst, m.curThink = -1, -1
	m.asstDelta, m.thinkDelta = false, false
}

func joinText(blocks []pirpc.ContentBlock) string {
	out := ""
	for _, b := range blocks {
		if b.Type == "text" {
			out += b.Text
		}
	}
	// pi caps tool output at 51200 bytes / 2000 lines; the collapsible
	// preview handles display, so keep the full text here (a 211-line
	// file is ~8KB and must survive for expand).
	if len(out) > maxToolResultChars {
		out = out[:maxToolResultChars] + "…"
	}
	return out
}

func joinTextBlocks(blocks []pirpc.ContentBlock) string { return joinText(blocks) }

// withImages appends a 📷 suffix for vision echoes (pi returns user content
// as text + image blocks; TextOf drops the images, so count them back).
func withImages(t string, n int) string {
	if n <= 0 {
		return t
	}
	s := "📷 1 image attached"
	if n > 1 {
		s = fmt.Sprintf("📷 %d images attached", n)
	}
	if strings.TrimSpace(t) == "" {
		return s
	}
	return t + "\n" + s
}

// diffOfDetails pulls an edit diff out of a toolResult details payload
// ({"diff": "..."}), so edit blocks can preview the change like pi.
func diffOfDetails(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var d struct {
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return ""
	}
	return d.Diff
}

// extension UI ---------------------------------------------------------------

// handleUIRequest routes extension_ui_request events.
// Protocol knowledge (methods, defaults, response shape) lives in
// src/extension; this only mutates UI state.
func (m Model) handleUIRequest(raw []byte) Model {
	var req pirpc.UIRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return m
	}
	switch {
	case extension.ShouldAutoCancel(req.Method):
		// MVP: auto-cancel so the agent uses defaults/timeout
		_ = m.Pi.Fire(pirpc.Command{Type: "extension_ui_response", ID: req.ID, Cancelled: boolPtr(true)})
	case req.Method == "select" || req.Method == "confirm":
		d := &Dialog{
			ID: req.ID, Method: req.Method, Kind: "ui",
			Title:   extension.TitleFor(req.Method, req.Title),
			Message: req.Message, Options: extension.OptionsFor(req),
		}
		d.Reindex()
		m.Dialogs = append(m.Dialogs, d)
	case req.Method == "notify":
		m.AddBlock(Block{Kind: "notice", Text: req.Message, Err: req.NotifyType == "error"})
	case req.Method == "setStatus":
		m.extStat = stripANSI(req.StatusText)
	case req.Method == "set_editor_text":
		m.ta.SetValue(req.Text)
	case req.Method == "setWidget":
		// skipped: pi extension widgets don't render in this TUI
		_ = req
	}
	m.Refresh()
	return m
}

func (m Model) updateDialog(km tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := m.Dialogs[0]
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
			d.Reindex()
		}
		return m, nil
	case tea.KeyCtrlV:
		if d.Kind == "secret" {
			return m, m.pasteCmd(true) // paste API key into /login
		}
		return m, nil
	case tea.KeyEsc:
		if d.Kind == "ui" {
			m.answerDialog(d, -1)
		} else {
			m.Dialogs = m.Dialogs[1:]
			m.Refresh()
		}
		return m, nil
	case tea.KeyEnter:
		return m.confirmDialog(d)
	}
	if km.Type == tea.KeyRunes {
		if d.Kind == "model" || d.Kind == "thinking" {
			// type to filter the picker
			d.Filter += km.String()
			d.Reindex()
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

func (m Model) confirmDialog(d *Dialog) (tea.Model, tea.Cmd) {
	if d.Kind == "ui" {
		if len(d.FIdx) == 0 {
			return m, nil
		}
		m.answerDialog(d, d.FIdx[d.Cursor])
		return m, nil
	}
	if d.Kind == "secret" {
		// input dialog: no Options/FIdx, Enter saves the typed key
		if fn, ok := m.confirm["secret"]; ok {
			return fn(&m, d, 0)
		}
		return m, nil
	}
	if len(d.FIdx) == 0 {
		return m, nil
	}
	ri := d.FIdx[d.Cursor]
	if fn, ok := m.confirm[d.Kind]; ok {
		return fn(&m, d, ri)
	}
	return m, nil
}

// answerDialog replies to an extension permission dialog
// (response shape built by src/extension).
func (m *Model) answerDialog(d *Dialog, choice int) {
	m.Dialogs = m.Dialogs[1:]
	_ = m.Pi.Fire(extension.Response(d.ID, d.Method, choice, d.Options))
	m.Refresh()
}

// reindex recomputes the visible list from Filter.

func (d *Dialog) Reindex() {
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

// pollCmds periodically reloads pi commands (auto-detects new ones).
func (m Model) pollCmds() tea.Cmd {
	return tea.Tick(45*time.Second, func(time.Time) tea.Msg {
		cmds, err := m.Pi.GetCommands()
		return CmdsRefreshMsg{Cmds: cmds, Err: err}
	})
}

func (m Model) fetchCmdsOnce() tea.Cmd {
	return func() tea.Msg {
		cmds, err := m.Pi.GetCommands()
		return CmdsRefreshMsg{Cmds: cmds, Err: err}
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
