package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// @ file mentions (pi parity) -------------------------------------------------
//
// Pi's TUI completes @path with a fuzzy file finder (fd, respects
// .gitignore) and sends the raw "@path" text to the agent — the model reads
// the file with its tools. Only CLI startup @args are expanded locally into
// <file> blocks. So gotui only needs the autocomplete half: typing @ opens
// this popup, Tab/Enter completes, the prompt text reaches pi untouched.
//
// Divergence: stdlib walk instead of fd (no binary dependency), skips .git
// and node_modules, no .gitignore parsing. Scoped "dir/" prefixes list that
// directory directly; a bare query fuzzy-matches basenames recursively.

// atItem is one file suggestion.
type atItem struct {
	value string // completion incl @ + quotes, e.g. @src/app/ or @"my dir/x.go"
	label string // basename (+ / for dirs)
	desc  string // display path
	dir   bool
}

const (
	atWin      = 10 // visible rows, same window style as cmdWin
	atMaxFuzzy = 20 // fuzzy results (matches pi's top-20)
	atMaxList  = 30 // direct directory listing cap
)

// cursorPos returns the textarea cursor as (row, rune-col), clamped.
func (m *Model) cursorPos() (row, col int) {
	row = m.ta.Line()
	li := m.ta.LineInfo()
	col = li.StartColumn + li.ColumnOffset
	lines := strings.Split(m.ta.Value(), "\n")
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	if row < 0 {
		return 0, 0
	}
	if n := len([]rune(lines[row])); col < 0 {
		col = 0
	} else if col > n {
		col = n
	}
	return row, col
}

func isAtDelim(r rune) bool {
	switch r {
	case ' ', '\t', '"', '\'', '=': // pi's PATH_DELIMITERS
		return true
	}
	return false
}

// unclosedQuote finds an unterminated " before the cursor (@"..." paths
// with spaces), -1 when quotes are balanced.
func unclosedQuote(text []rune) int {
	in := false
	start := -1
	for i, r := range text {
		if r == '"' {
			in = !in
			if in {
				start = i
			}
		}
	}
	if in {
		return start
	}
	return -1
}

// atToken extracts the @file token ending at col (port of pi's
// extractAtPrefix). ok=false for plain text, emails (a@b) and non-@
// quoted paths.
func atToken(line []rune, col int) (prefix string, start int, ok bool) {
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}
	text := line[:col]
	if q := unclosedQuote(text); q >= 0 {
		if q > 0 && text[q-1] == '@' && (q-1 == 0 || isAtDelim(text[q-2])) {
			return string(text[q-1:]), q - 1, true
		}
		return "", 0, false
	}
	tok := 0
	for i := len(text) - 1; i >= 0; i-- {
		if isAtDelim(text[i]) {
			tok = i + 1
			break
		}
	}
	if tok >= len(text) || text[tok] != '@' {
		return "", 0, false
	}
	if tok > 0 && !isAtDelim(text[tok-1]) {
		return "", 0, false // email-like: a@b
	}
	return string(text[tok:]), tok, true
}

// atValue builds the completion text: @"..." when quoted or spaced.
func atValue(rel string, quoted bool) string {
	if !quoted && !strings.Contains(rel, " ") {
		return "@" + rel
	}
	return "@\"" + rel + "\""
}

// atListDir lists one directory filtered by a filename prefix.
func atListDir(searchDir, displayBase, filePart string, quoted bool) []atItem {
	ents, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}
	fl := strings.ToLower(filePart)
	items := make([]atItem, 0, len(ents))
	for _, e := range ents {
		if filePart != "" && !strings.HasPrefix(strings.ToLower(e.Name()), fl) {
			continue
		}
		dir := e.IsDir()
		if !dir && e.Type()&os.ModeSymlink != 0 { // follow symlinked dirs (pi does)
			if st, err := os.Stat(filepath.Join(searchDir, e.Name())); err == nil && st.IsDir() {
				dir = true
			}
		}
		rel := displayBase + e.Name()
		if dir {
			rel += "/"
		}
		label := e.Name()
		if dir {
			label += "/"
		}
		items = append(items, atItem{value: atValue(rel, quoted), label: label, desc: rel, dir: dir})
	}
	sort.Slice(items, func(i, j int) bool { // dirs first, then alpha (pi order)
		if items[i].dir != items[j].dir {
			return items[i].dir
		}
		return strings.ToLower(items[i].label) < strings.ToLower(items[j].label)
	})
	if len(items) > atMaxList {
		items = items[:atMaxList]
	}
	return items
}

// atScore ranks a path against the query (port of pi's scoreEntry).
func atScore(rel, q string, dir bool) int {
	lrel := strings.ToLower(rel)
	name := lrel
	if i := strings.LastIndex(lrel, "/"); i >= 0 {
		name = lrel[i+1:]
	}
	var s int
	switch {
	case name == q:
		s = 100
	case strings.HasPrefix(name, q):
		s = 80
	case strings.Contains(name, q):
		s = 50
	case strings.Contains(lrel, q):
		s = 30
	default:
		return 0
	}
	if dir {
		s += 10
	}
	return s
}

// atFuzzy recursively matches basenames under base (stdlib stand-in for fd).
func (m *Model) atFuzzy(base, query string, quoted bool) []atItem {
	q := strings.ToLower(query)
	type scored struct {
		rel   string
		dir   bool
		score int
		depth int
	}
	var out []scored
	count := 0
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil || rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		depth := strings.Count(rel, "/")
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if depth > 6 || count > 8000 {
				return filepath.SkipDir
			}
		} else {
			count++
			if count > 8000 {
				return nil
			}
		}
		if s := atScore(rel, q, d.IsDir()); s > 0 {
			out = append(out, scored{rel: rel, dir: d.IsDir(), score: s, depth: depth})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		if out[i].depth != out[j].depth {
			return out[i].depth < out[j].depth
		}
		if len(out[i].rel) != len(out[j].rel) {
			return len(out[i].rel) < len(out[j].rel)
		}
		return out[i].rel < out[j].rel
	})
	if len(out) > atMaxFuzzy {
		out = out[:atMaxFuzzy]
	}
	items := make([]atItem, 0, len(out))
	for _, s := range out {
		rel := s.rel
		if s.dir {
			rel += "/"
		}
		label := filepath.Base(strings.TrimSuffix(rel, "/"))
		if s.dir {
			label += "/"
		}
		items = append(items, atItem{value: atValue(rel, quoted), label: label, desc: rel, dir: s.dir})
	}
	return items
}

// atCandidates lists suggestions for the raw text after @.
func (m *Model) atCandidates(raw string, quoted bool) []atItem {
	base := m.cwd
	if base == "" {
		base = "."
	}
	if raw == "" {
		return atListDir(base, "", "", quoted)
	}
	if raw == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return atListDir(home, "~/", "", quoted)
		}
		return nil
	}
	if i := strings.LastIndex(raw, "/"); i >= 0 {
		dirPart, filePart := raw[:i+1], raw[i+1:]
		var searchDir string
		switch {
		case strings.HasPrefix(dirPart, "~/"):
			home, err := os.UserHomeDir()
			if err != nil {
				return nil
			}
			searchDir = filepath.Join(home, dirPart[2:])
		case filepath.IsAbs(dirPart):
			searchDir = dirPart
		default:
			searchDir = filepath.Join(base, dirPart)
		}
		return atListDir(searchDir, dirPart, filePart, quoted)
	}
	return m.atFuzzy(base, raw, quoted)
}

// refreshAt recomputes the @ popup from the token before the cursor.
func (m *Model) refreshAt() {
	row, col := m.cursorPos()
	lines := strings.Split(m.ta.Value(), "\n")
	var line []rune
	if row >= 0 && row < len(lines) {
		line = []rune(lines[row])
	}
	prefix, start, ok := atToken(line, col)
	if !ok {
		m.closeAt()
		return
	}
	raw := strings.TrimPrefix(prefix, "@")
	quoted := false
	if strings.HasPrefix(raw, "\"") {
		quoted = true
		raw = raw[1:]
	}
	items := m.atCandidates(raw, quoted)
	if len(items) == 0 {
		m.closeAt()
		return
	}
	if prefix != m.atPrefix {
		m.atCursor, m.atOffset = 0, 0
	}
	m.atOpen = true
	m.atPrefix = prefix
	m.atStart = start
	m.atRow = row
	m.atItems = items
	if m.atCursor >= len(items) {
		m.atCursor = 0
	}
	m.ensureAtVisible()
	m.applyPopupH()
}

// closeAt hides the @ popup (no-op when already closed).
func (m *Model) closeAt() {
	if m.atOpen {
		m.atOpen = false
		m.applyPopupH()
	}
}

// ensureAtVisible keeps atCursor inside the scroll window.
func (m *Model) ensureAtVisible() {
	if m.atCursor < m.atOffset {
		m.atOffset = m.atCursor
	}
	if m.atCursor >= m.atOffset+atWin {
		m.atOffset = m.atCursor - atWin + 1
	}
	if m.atOffset < 0 {
		m.atOffset = 0
	}
}

// handleAtKey handles keys while the @ popup is open. true = consumed.
func (m *Model) handleAtKey(km tea.KeyMsg) bool {
	switch km.Type {
	case tea.KeyUp:
		if m.atCursor > 0 {
			m.atCursor--
		} else {
			m.atCursor = len(m.atItems) - 1
		}
		m.ensureAtVisible()
		return true
	case tea.KeyDown:
		if m.atCursor < len(m.atItems)-1 {
			m.atCursor++
		} else {
			m.atCursor = 0
		}
		m.ensureAtVisible()
		return true
	case tea.KeyTab:
		m.completeAt()
		return true
	case tea.KeyEnter:
		m.completeAt() // complete, don't send — Enter again sends
		return true
	case tea.KeyEsc:
		m.closeAt()
		m.Refresh()
		return true
	}
	return false
}

// completeAt replaces the @ token with the selected path. Dirs keep the
// popup open (no trailing space) so completion continues, like pi.
func (m *Model) completeAt() {
	if !m.atOpen || len(m.atItems) == 0 {
		return
	}
	if m.atCursor < 0 || m.atCursor >= len(m.atItems) {
		m.atCursor = 0
	}
	it := m.atItems[m.atCursor]
	lines := strings.Split(m.ta.Value(), "\n")
	row, col := m.cursorPos()
	if row != m.atRow || row < 0 || row >= len(lines) {
		m.refreshAt() // cursor wandered off; resync, don't corrupt text
		return
	}
	line := []rune(lines[row])
	if m.atStart > len(line) || col < m.atStart || col > len(line) {
		m.refreshAt()
		return
	}
	ins := it.value
	if !it.dir {
		ins += " "
	}
	lines[row] = string(line[:m.atStart]) + ins + string(line[col:])
	m.ta.SetValue(strings.Join(lines, "\n"))
	if row == len(lines)-1 {
		m.ta.SetCursor(m.atStart + len([]rune(ins)))
	}
	m.refreshAt()
	m.Refresh()
}

func (m *Model) atPopupH() int {
	if !m.atOpen {
		return 0
	}
	n := len(m.atItems)
	if n > atWin {
		n = atWin
	}
	extra := 0
	if m.atOffset > 0 {
		extra++
	}
	if m.atOffset+atWin < len(m.atItems) {
		extra++
	}
	return n + extra + 3 // rows + hints + footer + border
}

func (m Model) renderAtPopup() string {
	mainW := m.mainW()
	var b strings.Builder
	end := m.atOffset + atWin
	if end > len(m.atItems) {
		end = len(m.atItems)
	}
	if m.atOffset > 0 {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d above)", m.atOffset)) + "\n")
	}
	for i := m.atOffset; i < end; i++ {
		it := m.atItems[i]
		row := it.label
		if it.desc != "" && it.desc != it.label {
			row += " — " + it.desc
		}
		row = Short(row, mainW-8)
		if i == m.atCursor {
			b.WriteString("▸ " + cmdHiStyle.Render(row) + "\n")
		} else {
			b.WriteString("  " + statusBarStyle.Render(row) + "\n")
		}
	}
	if end < len(m.atItems) {
		b.WriteString("  " + toolStyle.Render(fmt.Sprintf("…(+%d below)", len(m.atItems)-end)) + "\n")
	}
	b.WriteString(toolStyle.Render(fmt.Sprintf("(%d/%d) Tab complete · Enter complete · Esc close", m.atCursor+1, len(m.atItems))))
	return cmdPopStyle.Width(mainW).Render(strings.TrimRight(b.String(), "\n"))
}
