package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/components/palette"
)

// Matching + window size live in components/palette; this file keeps the
// popup wiring on Model.
func (m *Model) cmdPrefix() (string, bool) {
	v := m.ta.Value()
	if !strings.HasPrefix(v, "/") || strings.ContainsAny(v, " \n") {
		return "", false
	}
	return v[1:], true
}

func (m *Model) refreshCmds() {
	m.cmdItems = m.cmdItems[:0]
	if p, ok := m.cmdPrefix(); ok {
		names := make([]string, len(m.Cmds))
		for i := range m.Cmds {
			// name + extension tag + description, so "/pi-subagents"
			// also matches that extension's commands (like pi)
			names[i] = m.Cmds[i].Name + " " + m.Cmds[i].SourceTag() + " " + m.Cmds[i].Description
		}
		m.cmdItems = append(m.cmdItems, palette.Match(p, names)...)
	}
	m.cmdOpen = len(m.cmdItems) > 0
	if m.cmdCursor >= len(m.cmdItems) {
		m.cmdCursor = 0
	}
	if m.cmdCursor < 0 {
		m.cmdCursor = 0
	}
	m.ensureCmdVisible()
	m.applyPopupH()
}

// ensureCmdVisible keeps cmdCursor inside the [cmdOffset, cmdOffset+palette.Win) window.

func (m *Model) ensureCmdVisible() {
	if m.cmdCursor < m.cmdOffset {
		m.cmdOffset = m.cmdCursor
	}
	if m.cmdCursor >= m.cmdOffset+palette.Win {
		m.cmdOffset = m.cmdCursor - palette.Win + 1
	}
	if m.cmdOffset < 0 {
		m.cmdOffset = 0
	}
}

func (m *Model) popupH() int {
	if !m.cmdOpen {
		return 0
	}
	n := len(m.cmdItems)
	if n > palette.Win {
		n = palette.Win
	}
	extra := 0 // scroll hints above/below the window
	if m.cmdOffset > 0 {
		extra++
	}
	if m.cmdOffset+palette.Win < len(m.cmdItems) {
		extra++
	}
	return n + extra + 3 // rows + hints + footer + border
}

func (m *Model) applyPopupH() {
	if !m.ready {
		return
	}
	h := m.baseVpH - m.popupH() - m.atPopupH() - m.chipH()
	if h < 3 {
		h = 3
	}
	if h != m.vp.Height {
		m.vp.Height = h
		m.vp.GotoBottom()
	}
}

func (m *Model) exactCmdMatch() bool {
	p, ok := m.cmdPrefix()
	if !ok {
		return false
	}
	for _, c := range m.Cmds {
		if strings.EqualFold(c.Name, p) {
			return true
		}
	}
	return false
}

// handleCmdKey handles keys while the command popup is open. true = key consumed.

func (m *Model) handleCmdKey(km tea.KeyMsg) bool {
	switch km.Type {
	case tea.KeyUp:
		if m.cmdCursor > 0 {
			m.cmdCursor--
		} else {
			m.cmdCursor = len(m.cmdItems) - 1
		}
		m.ensureCmdVisible()
		return true
	case tea.KeyDown:
		if m.cmdCursor < len(m.cmdItems)-1 {
			m.cmdCursor++
		} else {
			m.cmdCursor = 0
		}
		m.ensureCmdVisible()
		return true
	case tea.KeyTab:
		m.completeCmd()
		return true
	case tea.KeyEsc:
		m.cmdOpen = false
		m.applyPopupH()
		m.Refresh()
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

func (m *Model) completeCmd() {
	if !m.cmdOpen || len(m.cmdItems) == 0 {
		return
	}
	c := m.Cmds[m.cmdItems[m.cmdCursor]]
	m.ta.SetValue("/" + c.Name + " ")
	m.refreshCmds()
	m.refreshAt()
	m.Refresh()
}

func (m Model) renderCmdPopup() string {
	mainW := m.mainW()
	var b strings.Builder
	end := m.cmdOffset + palette.Win
	if end > len(m.cmdItems) {
		end = len(m.cmdItems)
	}
	if m.cmdOffset > 0 {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d above)", m.cmdOffset)) + "\n")
	}
	for i := m.cmdOffset; i < end; i++ {
		c := m.Cmds[m.cmdItems[i]]
		name := "/" + c.Name
		rest := ""
		if tag := c.SourceTag(); tag != "" {
			// extension command: "[u:npm:pi-subagents] desc", like pi
			rest = " — [" + tag + "]"
			if c.Description != "" {
				rest += " " + c.Description
			}
		} else {
			if c.Description != "" {
				rest = " — " + c.Description
			}
			rest += " [" + c.Source + "]"
		}
		row := Short(name+rest, mainW-8)
		// command name cyan, annotation keeps the row color
		nl := len(name)
		if nl > len(row) {
			nl = len(row)
		}
		if i == m.cmdCursor {
			b.WriteString("▸ " + cmdNameHiStyle.Render(row[:nl]) + cmdHiStyle.Render(row[nl:]) + "\n")
		} else {
			b.WriteString("  " + cmdNameStyle.Render(row[:nl]) + statusBarStyle.Render(row[nl:]) + "\n")
		}
	}
	if end < len(m.cmdItems) {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d below)", len(m.cmdItems)-end)) + "\n")
	}
	b.WriteString(toolStyle.Render(fmt.Sprintf("(%d/%d) Tab complete · Enter send · Esc close", m.cmdCursor+1, len(m.cmdItems))))
	return cmdPopStyle.Width(mainW).Render(strings.TrimRight(b.String(), "\n"))
}

// render ----------------------------------------------------------------------
