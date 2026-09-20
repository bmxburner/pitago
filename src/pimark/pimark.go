// Package pimark renders markdown/code with pi's OWN renderer (marked +
// highlight.js, dark theme) over a persistent node bridge. openpi draws the
// TUI chrome itself; chat content looks exactly like pi.
//
// Bridge protocol is JSONL on the node's stdin/stdout (see bridge.mjs,
// embedded below so the Go binary is self-contained). One node process per
// app run, lazy-started on first use; every entry point falls back to an
// error so callers can keep their old plain render. Stdlib only.
package pimark

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed bridge.mjs
var bridgeJS string

// Kind selects pi's message style: assistant text, or its thinking block
// (thinkingText + italic, like pi's AssistantMessageComponent).
type Kind string

const (
	Assistant Kind = "assistant"
	Thinking  Kind = "thinking"
)

type request struct {
	ID    uint64 `json:"id"`
	Op    string `json:"op"`
	Text  string `json:"text,omitempty"`
	Width int    `json:"width,omitempty"`
	Kind  Kind   `json:"kind,omitempty"`
	Code  string `json:"code,omitempty"`
	Lang  string `json:"lang,omitempty"`
}

type response struct {
	ID    uint64   `json:"id"`
	Lines []string `json:"lines,omitempty"`
	OK    bool     `json:"ok,omitempty"`
	Error string   `json:"error,omitempty"`
}

var (
	mu      sync.Mutex // guards proc state + pending + cache
	wmu     sync.Mutex // serializes stdin writes
	proc    *exec.Cmd
	stdin   io.Writer
	pending = map[uint64]chan response{}
	seq     uint64
	started bool
	// startErr is sticky: node/pi won't appear mid-run, so don't respawn
	// every render when the machine simply lacks them.
	startErr error
	done     chan struct{}
	cache    = map[string]string{}
)

// nodeBin finds node for the bridge.
func nodeBin() (string, error) {
	if p, err := exec.LookPath("node"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("pimark: node not in PATH")
}

// piRoot finds pi's dist dir (holds modes/interactive/theme/theme.js).
// Override with PIMARK_PI_ROOT for dev/test.
func piRoot() (string, error) {
	if r := os.Getenv("PIMARK_PI_ROOT"); r != "" {
		if okRoot(r) {
			return r, nil
		}
		return "", fmt.Errorf("pimark: bad PIMARK_PI_ROOT %q", r)
	}
	bin := os.Getenv("PI_BIN")
	if bin == "" {
		bin, _ = exec.LookPath("pi")
	}
	if bin != "" {
		if p, err := filepath.EvalSymlinks(bin); err == nil {
			// Homebrew layout: .../pi-coding-agent/dist/bundle/cli.js
			if filepath.Base(p) == "cli.js" && filepath.Base(filepath.Dir(p)) == "bundle" {
				if r := filepath.Dir(filepath.Dir(p)); okRoot(r) {
					return r, nil
				}
			}
		}
	}
	if out, err := exec.Command("npm", "root", "-g").Output(); err == nil {
		if r := filepath.Join(strings.TrimSpace(string(out)), "@earendil-works", "pi-coding-agent", "dist"); okRoot(r) {
			return r, nil
		}
	}
	return "", fmt.Errorf("pimark: pi dist not found (need pi in PATH)")
}

func okRoot(r string) bool {
	st, err := os.Stat(filepath.Join(r, "modes", "interactive", "theme", "theme.js"))
	return err == nil && !st.IsDir()
}

// ensure starts the bridge once. Callers must not hold mu.
func ensure() error {
	mu.Lock()
	if started {
		err := startErr
		mu.Unlock()
		return err
	}
	started = true
	mu.Unlock()

	err := start()
	mu.Lock()
	startErr = err
	mu.Unlock()
	return err
}

func start() error {
	node, err := nodeBin()
	if err != nil {
		return err
	}
	root, err := piRoot()
	if err != nil {
		return err
	}
	f, err := os.CreateTemp("", "pimark-*.mjs")
	if err != nil {
		return err
	}
	name := f.Name()
	if _, err := f.WriteString(bridgeJS); err != nil {
		f.Close()
		os.Remove(name)
		return err
	}
	f.Close()
	defer os.Remove(name) // node reads it at spawn; safe to unlink after

	cmd := exec.Command(node, name, "--pi-root", root, "--theme", "dark")
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = nil // bridge is silent; failures surface as timeouts/errors
	if err := cmd.Start(); err != nil {
		return err
	}
	d := make(chan struct{})
	mu.Lock()
	proc, stdin, done = cmd, stdinPipe, d
	mu.Unlock()
	go readLoop(stdoutPipe)
	go func() {
		cmd.Wait()
		mu.Lock()
		if done == d {
			close(d)
			proc = nil
		}
		mu.Unlock()
	}()
	// Ping so a broken bridge fails fast here, not on first render.
	if _, err := call(request{Op: "ping"}, 10*time.Second); err != nil {
		kill()
		return err
	}
	return nil
}

func readLoop(out io.Reader) {
	br := bufio.NewReaderSize(out, 1<<20)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		var resp response
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		mu.Lock()
		ch := pending[resp.ID]
		delete(pending, resp.ID)
		mu.Unlock()
		if ch != nil {
			select {
			case ch <- resp:
			default: // caller timed out; drop
			}
		}
	}
}

func call(req request, timeout time.Duration) (response, error) {
	if err := ensure(); err != nil {
		return response{}, err
	}
	mu.Lock()
	seq++
	req.ID = seq
	ch := make(chan response, 1)
	pending[req.ID] = ch
	d := done
	mu.Unlock()

	raw, err := json.Marshal(req)
	if err != nil {
		return response{}, err
	}
	wmu.Lock()
	_, werr := stdin.Write(append(raw, '\n'))
	wmu.Unlock()
	if werr != nil {
		kill()
		reset()
		mu.Lock()
		delete(pending, req.ID)
		mu.Unlock()
		return response{}, werr
	}
	select {
	case resp := <-ch:
		if resp.Error != "" {
			return resp, fmt.Errorf("pimark: %s", resp.Error)
		}
		return resp, nil
	case <-time.After(timeout):
		mu.Lock()
		delete(pending, req.ID)
		mu.Unlock()
		return response{}, fmt.Errorf("pimark: %s timed out", req.Op)
	case <-d:
		reset()
		return response{}, fmt.Errorf("pimark: bridge exited")
	}
}

func kill() {
	mu.Lock()
	defer mu.Unlock()
	if proc != nil && proc.Process != nil {
		_ = proc.Process.Kill()
	}
}

// reset allows the next call to restart a dead bridge (write failure or
// exit). Startup failures stay sticky via startErr — only a dead bridge
// resets.
func reset() {
	mu.Lock()
	defer mu.Unlock()
	proc = nil
	started = false
}

// Close kills the bridge; the next call restarts it (startup failures stay
// sticky via startErr, but an explicit Close always allows a fresh start).
func Close() {
	kill()
	mu.Lock()
	proc = nil
	started = false
	startErr = nil
	mu.Unlock()
}

// Prewarm starts the bridge in the background so the first message doesn't
// pay node startup (~200ms).
func Prewarm() {
	go func() { _ = ensure() }()
}

// Available reports whether pi rendering works here (node + pi present and
// the bridge answers). Used by tests to skip instead of fail.
func Available() bool {
	_, err := call(request{Op: "ping"}, 10*time.Second)
	return err == nil
}

func cached(key string) (string, bool) {
	mu.Lock()
	defer mu.Unlock()
	s, ok := cache[key]
	return s, ok
}

func store(key, val string) {
	mu.Lock()
	defer mu.Unlock()
	if len(cache) > 512 {
		cache = map[string]string{}
	}
	cache[key] = val
}

// Render runs text through pi's Markdown, wrapped to width. Returns lines
// joined by "\n" (pi pads lines to width; the caller trims trailing pad).
func Render(text string, width int, kind Kind) (string, error) {
	if width < 20 {
		width = 80
	}
	key := string(kind) + "\x00" + fmt.Sprint(width) + "\x00" + text
	if s, ok := cached(key); ok {
		return s, nil
	}
	resp, err := call(request{Op: "md", Text: text, Width: width, Kind: kind}, 15*time.Second)
	if err != nil {
		return "", err
	}
	out := strings.Join(resp.Lines, "\n")
	store(key, out)
	return out, nil
}

// Highlight runs code through pi's highlightCode (highlight.js + pi theme).
// No auto-detect: unknown/empty lang falls back to pi's mdCodeBlock color.
func Highlight(code, lang string) (string, error) {
	key := "hl\x00" + lang + "\x00" + code
	if s, ok := cached(key); ok {
		return s, nil
	}
	resp, err := call(request{Op: "hl", Code: code, Lang: lang}, 15*time.Second)
	if err != nil {
		return "", err
	}
	out := strings.Join(resp.Lines, "\n")
	store(key, out)
	return out, nil
}
