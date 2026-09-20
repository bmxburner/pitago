// Package mention implements @file autocomplete lookup (pi parity):
// typing @ opens a fuzzy file finder over the working directory.
//
// Pi completes @path with fd (respects .gitignore) and sends the raw "@path"
// text to the agent. Divergence: stdlib walk instead of fd (no binary
// dependency), skips .git and node_modules, no .gitignore parsing. Scoped
// "dir/" prefixes list that directory directly; a bare query fuzzy-matches
// basenames recursively.
//
// Everything here is pure (no TUI state): the popup wiring lives in src/app.
package mention

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Item is one file suggestion.
type Item struct {
	Value string // completion incl @ + quotes, e.g. @src/app/ or @"my dir/x.go"
	Label string // basename (+ / for dirs)
	Desc  string // display path
	Dir   bool
}

const (
	Win      = 10 // visible rows, same window style as the command palette
	MaxFuzzy = 20 // fuzzy results (matches pi's top-20)
	MaxList  = 30 // direct directory listing cap
)

func isDelim(r rune) bool {
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

// Token extracts the @file token ending at col (port of pi's
// extractAtPrefix). ok=false for plain text, emails (a@b) and non-@
// quoted paths.
func Token(line []rune, col int) (prefix string, start int, ok bool) {
	if col < 0 {
		col = 0
	}
	if col > len(line) {
		col = len(line)
	}
	text := line[:col]
	if q := unclosedQuote(text); q >= 0 {
		if q > 0 && text[q-1] == '@' && (q-1 == 0 || isDelim(text[q-2])) {
			return string(text[q-1:]), q - 1, true
		}
		return "", 0, false
	}
	tok := 0
	for i := len(text) - 1; i >= 0; i-- {
		if isDelim(text[i]) {
			tok = i + 1
			break
		}
	}
	if tok >= len(text) || text[tok] != '@' {
		return "", 0, false
	}
	if tok > 0 && !isDelim(text[tok-1]) {
		return "", 0, false // email-like: a@b
	}
	return string(text[tok:]), tok, true
}

// Value builds the completion text: @"..." when quoted or spaced.
func Value(rel string, quoted bool) string {
	if !quoted && !strings.Contains(rel, " ") {
		return "@" + rel
	}
	return "@\"" + rel + "\""
}

// ListDir lists one directory filtered by a filename prefix.
func ListDir(searchDir, displayBase, filePart string, quoted bool) []Item {
	ents, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}
	fl := strings.ToLower(filePart)
	items := make([]Item, 0, len(ents))
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
		items = append(items, Item{Value: Value(rel, quoted), Label: label, Desc: rel, Dir: dir})
	}
	sort.Slice(items, func(i, j int) bool { // dirs first, then alpha (pi order)
		if items[i].Dir != items[j].Dir {
			return items[i].Dir
		}
		return strings.ToLower(items[i].Label) < strings.ToLower(items[j].Label)
	})
	if len(items) > MaxList {
		items = items[:MaxList]
	}
	return items
}

// Score ranks a path against the query (port of pi's scoreEntry).
func Score(rel, q string, dir bool) int {
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

// Fuzzy recursively matches basenames under base (stdlib stand-in for fd).
func Fuzzy(base, query string, quoted bool) []Item {
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
		if s := Score(rel, q, d.IsDir()); s > 0 {
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
	if len(out) > MaxFuzzy {
		out = out[:MaxFuzzy]
	}
	items := make([]Item, 0, len(out))
	for _, s := range out {
		rel := s.rel
		if s.dir {
			rel += "/"
		}
		label := filepath.Base(strings.TrimSuffix(rel, "/"))
		if s.dir {
			label += "/"
		}
		items = append(items, Item{Value: Value(rel, quoted), Label: label, Desc: rel, Dir: s.dir})
	}
	return items
}

// Candidates lists suggestions for the raw text after @ under baseDir
// ("" means "."). Kept here so the lookup is testable without TUI state.
func Candidates(baseDir, raw string, quoted bool) []Item {
	base := baseDir
	if base == "" {
		base = "."
	}
	if raw == "" {
		return ListDir(base, "", "", quoted)
	}
	if raw == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return ListDir(home, "~/", "", quoted)
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
		return ListDir(searchDir, dirPart, filePart, quoted)
	}
	return Fuzzy(base, raw, quoted)
}
