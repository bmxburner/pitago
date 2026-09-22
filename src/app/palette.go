package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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

// cmdWin caps the visible popup rows so the whole frame fits winH on
// short terminals: header(1) + chat min(3) + popup box + input(6+chips)
// must stay <= winH. Tall screens keep palette.Win; the box never grows
// past it (see components/palette).
func (m Model) cmdWin() int {
	boxMax := m.winH - 1 - 3 - (6 + m.chipH()) - m.atPopupH()
	win := boxMax - 2 - 1 - 2 // border + footer + both scroll hints
	if win > palette.Win {
		win = palette.Win
	}
	if win < 1 {
		win = 1
	}
	return win
}

func (m *Model) ensureCmdVisible() {
	win := m.cmdWin()
	if m.cmdCursor < m.cmdOffset {
		m.cmdOffset = m.cmdCursor
	}
	if m.cmdCursor >= m.cmdOffset+win {
		m.cmdOffset = m.cmdCursor - win + 1
	}
	if m.cmdOffset < 0 {
		m.cmdOffset = 0
	}
}

func (m *Model) popupH() int {
	if !m.cmdOpen {
		return 0
	}
	win := m.cmdWin()
	n := len(m.cmdItems)
	if n > win {
		n = win
	}
	extra := 0 // scroll hints above/below the window
	if m.cmdOffset > 0 {
		extra++
	}
	if m.cmdOffset+win < len(m.cmdItems) {
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
	win := m.cmdWin()
	end := m.cmdOffset + win
	if end > len(m.cmdItems) {
		end = len(m.cmdItems)
	}
	// plain (unstyled) row text so width math stays ANSI-free
	plain := func(i int) (name, rest string) {
		c := m.Cmds[m.cmdItems[i]]
		name = "/" + c.Name
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
		return name, rest
	}
	foot := fmt.Sprintf("(%d/%d) Tab complete · Enter send · Esc close", m.cmdCursor+1, len(m.cmdItems))
	// The dropdown hugs its content instead of spanning the chat width:
	// box = widest line over ALL matches (scrolling never jitters it)
	// plus padding, capped at mainW.
	contentW := lipgloss.Width(foot)
	if m.cmdOffset > 0 {
		if w := lipgloss.Width(fmt.Sprintf("…(+%d above)", m.cmdOffset)); w > contentW {
			contentW = w
		}
	}
	if below := len(m.cmdItems) - end; below > 0 {
		if w := lipgloss.Width(fmt.Sprintf("…(+%d below)", below)); w > contentW {
			contentW = w
		}
	}
	for pos := range m.cmdItems {
		name, rest := plain(pos)
		if w := 2 + lipgloss.Width(Short(name+rest, mainW)); w > contentW {
			contentW = w
		}
	}
	boxW := contentW + 2 // horizontal padding (lipgloss adds the 2 border cols outside Width)
	if boxW > mainW-2 {
		boxW = mainW - 2
	}
	textW := boxW - 2 - 2 // padding + "▸ " marker
	if textW < 1 {
		textW = 1
	}
	var b strings.Builder
	if m.cmdOffset > 0 {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d above)", m.cmdOffset)) + "\n")
	}
	for i := m.cmdOffset; i < end; i++ {
		name, rest := plain(i)
		row := Short(name+rest, textW)
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
	b.WriteString(toolStyle.Render(foot))
	return cmdPopStyle.Width(boxW).Render(strings.TrimRight(b.String(), "\n"))
}

// render ----------------------------------------------------------------------
