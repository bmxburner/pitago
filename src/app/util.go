package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) firstUser() string {
	for _, b := range m.blocks {
		if b.Kind == "user" && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// fmtDur formats durations like pi: 2m37s, 3m0s, 5s.

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h, mm, s := int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60
	if h > 0 {
		return fmt.Sprintf("%dh%dm", h, mm)
	}
	if mm > 0 {
		return fmt.Sprintf("%dm%ds", mm, s)
	}
	return fmt.Sprintf("%ds", s)
}

// twoCol pads a two-column sidebar row (left col 17 wide).

func twoCol(l, r string, inner int) string {
	const lw = 17
	l, r = Short(l, lw-1), Short(r, inner-lw-1)
	p := lw - lipgloss.Width(l)
	if p < 1 {
		p = 1
	}
	return l + strings.Repeat(" ", p) + r
}

// ctxBar renders a fixed-width context usage bar like pi's session panel.

func ctxBar(pct float64, w int) string {
	if w < 1 {
		w = 1
	}
	f := int(pct/100*float64(w) + 0.5)
	if f > w {
		f = w
	}
	if f < 0 {
		f = 0
	}
	return strings.Repeat("█", f) + strings.Repeat("░", w-f)
}

// recentAt maps a mouse click to a sidebar recent-model row.
// Layout: header(1) + body; sidebar starts at x=mainW+1; inside its box,
// content row 0 is at screen y=2; recents start at recentContentRow.

func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func Short(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func oneLineStr(s string) string { return strings.ReplaceAll(s, "\n", " ⏎ ") }

func OrDefault(s, d string) string {
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

func FmtNum(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
