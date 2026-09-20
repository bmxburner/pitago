package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// cmdWin is the visible row window of the / command popup (the full match
// list scrolls; the popup never grows past this).
const cmdWin = 10

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
		pl := strings.ToLower(p)
		for i := range m.Cmds {
			name := strings.ToLower(m.Cmds[i].Name)
			if strings.HasPrefix(name, pl) || strings.Contains(name, pl) {
				m.cmdItems = append(m.cmdItems, i)
			}
		}
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

// ensureCmdVisible keeps cmdCursor inside the [cmdOffset, cmdOffset+cmdWin) window.

func (m *Model) ensureCmdVisible() {
	if m.cmdCursor < m.cmdOffset {
		m.cmdOffset = m.cmdCursor
	}
	if m.cmdCursor >= m.cmdOffset+cmdWin {
		m.cmdOffset = m.cmdCursor - cmdWin + 1
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
	if n > cmdWin {
		n = cmdWin
	}
	extra := 0 // scroll hints above/below the window
	if m.cmdOffset > 0 {
		extra++
	}
	if m.cmdOffset+cmdWin < len(m.cmdItems) {
		extra++
	}
	return n + extra + 3 // rows + hints + footer + border
}

func (m *Model) applyPopupH() {
	if !m.ready {
		return
	}
	h := m.baseVpH - m.popupH() - m.atPopupH()
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
	end := m.cmdOffset + cmdWin
	if end > len(m.cmdItems) {
		end = len(m.cmdItems)
	}
	if m.cmdOffset > 0 {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d above)", m.cmdOffset)) + "\n")
	}
	for i := m.cmdOffset; i < end; i++ {
		c := m.Cmds[m.cmdItems[i]]
		row := "/" + c.Name
		if c.Description != "" {
			row += " — " + c.Description
		}
		row += " [" + c.Source + "]"
		row = Short(row, mainW-8)
		if i == m.cmdCursor {
			b.WriteString("▸ " + cmdHiStyle.Render(row) + "\n")
		} else {
			b.WriteString("  " + statusBarStyle.Render(row) + "\n")
		}
	}
	if end < len(m.cmdItems) {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d below)", len(m.cmdItems)-end)) + "\n")
	}
	b.WriteString(toolStyle.Render(fmt.Sprintf("(%d/%d) Tab complete · Enter send · Esc close", m.cmdCursor+1, len(m.cmdItems))))
	return cmdPopStyle.Width(mainW).Render(strings.TrimRight(b.String(), "\n"))
}

// render ----------------------------------------------------------------------
