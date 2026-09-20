package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"gotui/src/extension"
)

func (m Model) showSide() bool { return !m.hideSide && m.winW >= 80 }

func (m Model) mainW() int {
	w := m.winW - 2 // single column margin
	if m.showSide() {
		w = m.winW - sideW - 5 // chat + gap + sidebar
	}
	if w < 30 {
		w = 30
	}
	return w
}

// blocks helpers ----------------------------------------------------------

// gutter prefixes a block with a left status icon (pi-style): the first
// non-empty line gets the icon, continuation lines get a blank 2-cell
// gutter so the column stays aligned. Blank separator lines stay empty.
func gutter(icon, body string) string {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	first := true
	for _, ln := range lines {
		if ln == "" {
			out = append(out, "")
			continue
		}
		if first {
			out = append(out, icon+" "+ln)
			first = false
		} else {
			out = append(out, "  "+ln)
		}
	}
	return strings.Join(out, "\n")
}

func (m Model) renderBlocks() string {
	var b strings.Builder
	w := m.vp.Width
	cw := w - 2 // gutter takes 2 cells
	if cw < 10 {
		cw = 10
	}
	if len(m.blocks) == 0 && m.connErr == "" {
		empty := lipgloss.NewStyle().Foreground(cMuted).Width(w).
			Align(lipgloss.Center).
			Render("\nNo messages yet — type below to start.\n")
		b.WriteString(empty)
	}
	if m.connErr != "" {
		b.WriteString(gutter(errStyle.Render("×"), errStyle.Render("! "+m.connErr)+"\n"))
	}
	for _, bl := range m.blocks {
		var icon, body string
		switch bl.Kind {
		case "user":
			icon = statusBarStyle.Render("●")
			body = userStyle.Width(cw-2).Render(bl.Text) + "\n\n"
		case "assistant":
			icon = statusBarStyle.Render("●")
			body = lipgloss.NewStyle().Foreground(cText).Width(cw).Render(bl.Text) + "\n\n"
		case "thinking":
			t := bl.Text
			if len(t) > 300 {
				t = t[:300] + "…"
			}
			icon = statusBarStyle.Render("○")
			body = toolStyle.Render(Short(t, 160)) + "\n\n"
		case "tool":
			switch bl.ToolStatus {
			case "done":
				icon = okStyle.Render("●")
			case "error":
				icon = errStyle.Render("×")
			default:
				icon = statusBarStyle.Render("○")
			}
			head := bl.ToolName
			if bl.ToolArgs != "" {
				head += " " + bl.ToolArgs
			}
			if strings.TrimSpace(head) == "" {
				head = "tool"
			}
			body = lipgloss.NewStyle().Foreground(cText).Render(Short(head, 140)) + "\n"
			if r := strings.TrimSpace(bl.ToolResult); r != "" {
				body += toolStyle.Render("  └ "+Short(oneLineStr(r), 160)) + "\n"
			}
			body += "\n"
		case "bash":
			icon = statusBarStyle.Render("●")
			body = codeStyle.Render(Short(bl.Text, 400)) + "\n\n"
		case "tree":
			icon = statusBarStyle.Render("●")
			body = codeStyle.Render(Short(bl.Text, 3000)) + "\n\n"
		case "notice":
			if bl.Err {
				icon = errStyle.Render("×")
				body = errStyle.Render("! "+bl.Text) + "\n\n"
			} else {
				icon = toolStyle.Render("·")
				body = toolStyle.Render(bl.Text) + "\n\n"
			}
		default:
			icon = statusBarStyle.Render("●")
			body = lipgloss.NewStyle().Foreground(cText).Render(bl.Text) + "\n\n"
		}
		b.WriteString(gutter(icon, body))
	}
	if m.thinking {
		b.WriteString(gutter(statusBarStyle.Render("○"), statusBarStyle.Render(m.Status)+"\n"))
	}
	return b.String()
}

// renderSidebar mirrors pi's session panel: SESSION, model+ctx, STATS,
// RECENT MODELS (clickable), COMMANDS, WORKSPACE, cwd. Content is built by
// buildSidebarContent and shown through sideVp, so a tall sidebar clips to
// the box and scrolls (wheel over it) instead of overflowing the layout.
// recentAt maps clicks with sideVp.YOffset, so it stays correct scrolled.

func (m Model) buildSidebarContent() string {
	inner := sideInnerW
	var b strings.Builder
	b.WriteString(m.renderPet(inner))
	b.WriteString(sideTitleStyle.Render("SESSION") + "\n")
	first := m.firstUser()
	if first == "" {
		dot := statusBarStyle.Render("○")
		if m.thinking {
			dot = okStyle.Render("●")
		}
		first = dot + " " + Short(m.Status, inner-2)
	} else {
		first = Short(first, inner)
	}
	b.WriteString(statusBarStyle.Render(first) + "\n")
	sess := m.session
	if sess == "" {
		sess = "…"
	}
	b.WriteString(statusBarStyle.Render(Short(sess, inner)) + "\n")
	b.WriteString(sep() + "\n")

	modelName := Short(m.ModelLbl, inner-8)
	if m.thinkLvl != "" {
		modelName = Short(m.ModelLbl+" - "+m.thinkLvl, inner-8)
	}
	b.WriteString(statusBarStyle.Render("model · ") + lipgloss.NewStyle().Foreground(cText).Render(modelName) + "\n")
	barW := inner - len("ctx ") - len(" 100%")
	if barW < 4 {
		barW = 4
	}
	pct := fmt.Sprintf("%3.0f%%", m.Stats.ContextPct)
	b.WriteString(statusBarStyle.Render("ctx "+ctxBar(m.Stats.ContextPct, barW)+" "+pct) + "\n")
	used := m.Stats.ContextToks
	if used == 0 {
		used = m.Stats.TokensTotal
	}
	win := m.ctxWindow
	if win == 0 {
		win = m.Stats.ContextWin
	}
	compact := "manual"
	if m.autoCompact {
		compact = "compact auto"
	}
	tokLine := "tok —"
	if win > 0 {
		tokLine = fmt.Sprintf("%s/%s tkns - %s", FmtNum(used), FmtNum(win), compact)
	} else if used > 0 {
		tokLine = "tok " + FmtNum(used)
	}
	b.WriteString(statusBarStyle.Render(Short(tokLine, inner)) + "\n")
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render(twoCol("Stats", "Tokens", inner)) + "\n")
	elapsed := "—"
	if !m.sessStart.IsZero() {
		elapsed = fmtDur(time.Since(m.sessStart))
	}
	last := "—"
	if m.lastDur > 0 {
		last = fmtDur(m.lastDur)
	}
	speed := "—"
	if m.lastSpeed > 0 {
		speed = fmt.Sprintf("%.0f tok/s", m.lastSpeed)
	}
	cache := "—"
	if m.Stats.TokensTotal > 0 && m.Stats.CacheRead > 0 {
		cache = fmt.Sprintf("%.0f%%", 100*float64(m.Stats.CacheRead)/float64(m.Stats.TokensTotal))
	}
	cost := "—"
	if m.Stats.Cost > 0 {
		cost = fmt.Sprintf("$%.2f", m.Stats.Cost)
	}
	b.WriteString(statusBarStyle.Render(twoCol("time "+elapsed, "in "+FmtNum(m.Stats.In), inner)) + "\n")
	b.WriteString(statusBarStyle.Render(twoCol("last "+last, "out "+FmtNum(m.Stats.Out), inner)) + "\n")
	b.WriteString(statusBarStyle.Render(twoCol("speed "+speed, "total "+FmtNum(m.Stats.TokensTotal), inner)) + "\n")
	b.WriteString(statusBarStyle.Render(twoCol(fmt.Sprintf("turns %d", m.Stats.UserMsgs), "cache "+cache, inner)) + "\n")
	left := "—"
	if m.Stats.ContextPct > 0 {
		left = fmt.Sprintf("%.0f%%", 100-m.Stats.ContextPct)
	}
	b.WriteString(statusBarStyle.Render(twoCol("left "+left, "cost "+cost, inner)) + "\n")
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render("RECENT MODELS") + "\n")
	if len(m.recentModels) == 0 {
		b.WriteString(toolStyle.Render("—") + "\n")
	} else {
		for i, r := range m.recentModels {
			cur := r.ID == m.ModelLbl || r.DispLabel() == m.ModelLbl
			mark, style := "○ ", statusBarStyle
			row := style
			if cur {
				mark, style = "● ", okStyle
				row = lipgloss.NewStyle().Foreground(cText)
			}
			b.WriteString(style.Render(mark) + row.Render(fmt.Sprintf("%d. %s", i+1, Short(r.DispLabel(), inner-5))) + "\n")
		}
	}
	b.WriteString(toolStyle.Render(m.recentHint()) + "\n")
	b.WriteString(sep() + "\n")

	b.WriteString(sideTitleStyle.Render("COMMANDS") + "\n")
	if len(m.Cmds) == 0 {
		b.WriteString(toolStyle.Render("—") + "\n")
	} else {
		// Per-source counts: extension vs prompt vs skill vs builtin
		// (source taxonomy owned by src/extension).
		ext, prm, skl, bin := extension.Summarize(m.Cmds)
		b.WriteString(statusBarStyle.Render(Short(fmt.Sprintf("%d ext · %d prompt · %d skill · %d builtin", ext, prm, skl, bin), inner)) + "\n")
	}
	if len(m.queue.Steering)+len(m.queue.FollowUp) > 0 {
		b.WriteString(statusBarStyle.Render(fmt.Sprintf("queue: %d steer · %d follow",
			len(m.queue.Steering), len(m.queue.FollowUp))) + "\n")
	}
	b.WriteString(m.renderMcpSection(inner))
	b.WriteString(m.renderTodosSection(inner))
	if m.ws.ok {
		b.WriteString(sideTitleStyle.Render(Short("WORKSPACE · "+m.ws.branch, inner)) + "\n")
		for _, f := range m.ws.files {
			stat := fmt.Sprintf("+%d -%d", f.add, f.del)
			gap := inner - lipgloss.Width(f.path) - lipgloss.Width(stat)
			name := f.path
			if gap < 1 {
				name = Short(f.path, inner-lipgloss.Width(stat)-1)
				gap = 1
			}
			b.WriteString(statusBarStyle.Render(name+strings.Repeat(" ", gap)) + okStyle.Render(stat) + "\n")
		}
		if m.ws.more > 0 {
			b.WriteString(toolStyle.Render(fmt.Sprintf("… %d more files", m.ws.more)) + "\n")
		}
		if m.ws.untracked > 0 {
			b.WriteString(toolStyle.Render(fmt.Sprintf("?%d untracked", m.ws.untracked)) + "\n")
		}
	}
	b.WriteString(toolStyle.Render(Short(m.cwd, inner)) + "\n")
	return b.String()
}

// renderSidebar draws the sidebar box around the visible sideVp slice.
// lipgloss Height covers the content only (border adds 2), so size it by
// sideContentH to keep the outer box exactly sideH tall.
func (m Model) renderSidebar() string {
	return sideStyle.Width(sideW).Height(m.sideContentH()).Render(m.sideVp.View())
}

func sep() string {
	return sepStyle.Render(strings.Repeat("─", sideW-6))
}

func (m Model) sideH() int {
	// sidebar box fills body (winH - header 1) minus its own border 2
	h := m.winH - 3
	if h < 6 {
		h = 6
	}
	return h
}

// sideContentH is the visible sidebar content height (box minus border).
func (m Model) sideContentH() int {
	h := m.sideH() - 2
	if h < 1 {
		h = 1
	}
	return h
}

// overSide reports whether screen x is over the sidebar column.
func (m Model) overSide(x int) bool {
	return m.showSide() && x >= m.mainW()+1 && x <= m.winW
}

// statsLine mirrors opencode's status footer: model · ctx% · cost, no sidebar.

func (m Model) statsLine() string {
	if m.Stats.ContextPct > 0 {
		return fmt.Sprintf("%s · %.0f%% · %s · $%.2f",
			Short(m.ModelLbl, 30), m.Stats.ContextPct, FmtNum(m.Stats.TokensTotal), m.Stats.Cost)
	}
	if m.Stats.Cost > 0 || m.Stats.TokensTotal > 0 {
		return fmt.Sprintf("%s · %s · $%.2f",
			Short(m.ModelLbl, 30), FmtNum(m.Stats.TokensTotal), m.Stats.Cost)
	}
	return Short(m.ModelLbl, 40)
}

func (m Model) renderHeader() string {
	left := Short(m.cwd, 48)
	if m.session != "" {
		left = Short(m.session+" · "+m.cwd, 48)
	}
	dot := "○"
	if m.thinking {
		dot = "●"
	}
	right := dot + " " + Short(m.Status, 32)
	gap := m.winW - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return headerStyle.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) renderInput() string {
	mainW := m.mainW()
	style := inputFocusStyle
	left := "○ ready · ↵ send · / commands · @ files · ^P model · ^R recents · ^C quit"
	if m.thinking {
		style = inputRunStyle // green border + live status, pi-style
		left = "○ " + m.Status + " · ↵ steer · Esc cancel"
	} else if len(m.Dialogs) > 0 {
		style = inputStyle
	}
	right := m.statsLine()
	if m.extStat != "" {
		right = Short(m.extStat, 30) + " · " + right
	}
	gap := mainW - lipgloss.Width(left) - lipgloss.Width(right) - 6
	if gap < 1 {
		gap = 1
	}
	foot := statusBarStyle.Render(left + strings.Repeat(" ", gap) + right)
	return style.Width(mainW).Render(m.ta.View() + "\n" + foot)
}

func (m Model) renderDialog() string {
	d := m.Dialogs[0]
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(cText).Render(d.Title) + "\n")
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
				style = rowHiStyle
			}
			row := Short(d.Options[ri], 34)
			if desc := DescOf(d, ri); desc != "" {
				row += "  " + toolStyle.Render("— "+Short(desc, 40))
			}
			if fi == d.Cursor {
				b.WriteString(cursor + style.Width(52).Render(row) + "\n")
			} else {
				b.WriteString(cursor + style.Render(row) + "\n")
			}
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
	if len(m.Dialogs) > 1 {
		hint = statusBarStyle.Render(fmt.Sprintf("(%d more dialogs pending)", len(m.Dialogs)-1))
	}
	return lipgloss.JoinVertical(lipgloss.Center,
		lipgloss.Place(m.winW, m.winH-2, lipgloss.Center, lipgloss.Center, box),
		hint,
	)
}

func DescOf(d *Dialog, ri int) string {
	if ri < len(d.Descs) {
		return d.Descs[ri]
	}
	return ""
}

func (m Model) View() string {
	if !m.ready {
		return "starting…"
	}
	if len(m.Dialogs) > 0 {
		return m.renderDialog()
	}
	body := lipgloss.JoinVertical(lipgloss.Left, m.vp.View(), m.renderInput())
	if m.cmdOpen || m.atOpen {
		parts := []string{m.vp.View()}
		if m.cmdOpen {
			parts = append(parts, m.renderCmdPopup())
		}
		if m.atOpen {
			parts = append(parts, m.renderAtPopup())
		}
		parts = append(parts, m.renderInput())
		body = lipgloss.JoinVertical(lipgloss.Left, parts...)
	}
	if m.showSide() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, " ", m.renderSidebar())
	}
	return lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body)
}

// utils ------------------------------------------------------------------------
