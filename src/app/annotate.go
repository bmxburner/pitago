package app

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/components/clipboard"
)

type ReviewKind string

const (
	ReviewFile        ReviewKind = "file"
	ReviewFolder      ReviewKind = "folder"
	ReviewLastMessage ReviewKind = "last-message"
	ReviewSelection   ReviewKind = "selection"
)

type ReviewSurface string

const (
	ReviewSurfaceAuto      ReviewSurface = "auto"
	ReviewSurfaceTUI       ReviewSurface = "tui"
	ReviewSurfaceClipboard ReviewSurface = "clipboard"
)

type ReviewRequest struct {
	Kind          ReviewKind
	Target        string
	Content       string
	CWD           string
	SessionID     string
	Preferred     ReviewSurface
	Placement     string
	TUIExecutable string
}

type ReviewError struct {
	Code string
	Err  error
}

func (e *ReviewError) Error() string {
	if e == nil || e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}
func (e *ReviewError) Unwrap() error { return e.Err }

var (
	ErrReviewUnavailable   = errors.New("review surface unavailable")
	ErrReviewInvalidResult = errors.New("review returned an invalid result")
)

type ReviewCapabilities struct {
	TUI     string
	Browser bool
	Pi      bool
}

func DetectReviewCapabilities(req ReviewRequest) ReviewCapabilities {
	caps := ReviewCapabilities{}
	tuiPath, tuiDisabled := TUIExecutablePref(req.TUIExecutable)
	if !tuiDisabled {
		if tuiPath != "" {
			if st, err := os.Stat(tuiPath); err == nil && !st.IsDir() {
				caps.TUI = tuiPath
			}
		}
		if caps.TUI == "" {
			if path, err := exec.LookPath("plannotator-tui"); err == nil {
				caps.TUI = path
			}
		}
		if caps.TUI == "" {
			for _, path := range knownTUIPaths() {
				if st, err := os.Stat(path); err == nil && !st.IsDir() {
					caps.TUI = path
					break
				}
			}
		}
	}
	caps.Browser = browserAvailable()
	caps.Pi = req.CWD != "" || req.SessionID != ""
	return caps
}

// knownTUIPaths is a var so tests can pin TUI detection instead of depending on
// whether the machine running them has plannotator-tui installed.
var knownTUIPaths = func() []string {
	paths := []string{"/opt/homebrew/bin/plannotator-tui", "/usr/local/bin/plannotator-tui"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".local", "bin", "plannotator-tui"))
	}
	return paths
}

func browserAvailable() bool {
	for _, bin := range []string{"open", "xdg-open", "cmd"} {
		if _, err := exec.LookPath(bin); err == nil {
			return true
		}
	}
	return false
}

func SelectReviewSurface(req ReviewRequest, caps ReviewCapabilities) (ReviewSurface, error) {
	preferred := req.Preferred
	if preferred == "" {
		preferred = ReviewSurfaceAuto
	}
	switch preferred {
	case ReviewSurfaceClipboard:
		return ReviewSurfaceClipboard, nil
	case ReviewSurfaceTUI:
		if caps.TUI == "" {
			if _, disabled := TUIExecutablePref(req.TUIExecutable); disabled {
				return "", &ReviewError{Code: "tui disabled by PITAGO_ANNOTATE_TUI", Err: ErrReviewUnavailable}
			}
			return "", &ReviewError{Code: "tui unavailable", Err: ErrReviewUnavailable}
		}
		return ReviewSurfaceTUI, nil
	case ReviewSurfaceAuto:
		if caps.TUI != "" {
			return ReviewSurfaceTUI, nil
		}
		return ReviewSurfaceClipboard, nil
	default:
		return "", &ReviewError{Code: "unknown review surface", Err: fmt.Errorf("%q", preferred)}
	}
}

func (r ReviewRequest) validate() error {
	if r.Kind == "" || r.CWD == "" {
		return &ReviewError{Code: "invalid review request", Err: ErrReviewInvalidResult}
	}
	if r.Kind == ReviewFile || r.Kind == ReviewFolder {
		if strings.TrimSpace(r.Target) == "" {
			return &ReviewError{Code: "review target is required", Err: ErrReviewInvalidResult}
		}
	}
	if r.Kind == ReviewSelection && strings.TrimSpace(r.Content) == "" {
		return &ReviewError{Code: "selection is empty", Err: ErrReviewInvalidResult}
	}
	return nil
}

func reviewTarget(req ReviewRequest) (string, error) {
	if req.Kind == ReviewLastMessage || req.Kind == ReviewSelection {
		return "", nil
	}
	target := req.Target
	if !filepath.IsAbs(target) {
		target = filepath.Join(req.CWD, target)
	}
	return filepath.Clean(target), nil
}

// lastAssistantText is the most recent assistant block, which is what a
// "review the last reply" request means in this session.
func lastAssistantText(blocks []Block) string {
	for i := len(blocks) - 1; i >= 0; i-- {
		if blocks[i].Kind == "assistant" && strings.TrimSpace(blocks[i].Text) != "" {
			return blocks[i].Text
		}
	}
	return ""
}

func reviewClipboardText(req ReviewRequest) string {
	target, _ := reviewTarget(req)
	if req.Kind == ReviewLastMessage {
		target = "the latest assistant reply"
	} else if req.Kind == ReviewSelection {
		target = "the current Pitago chat selection"
	}
	return fmt.Sprintf("# Plannotator review target\n\n- cwd: %s\n- target: %s\n\nOpen this target with your preferred Plannotator review surface, then send the numbered feedback back to Pitago.", req.CWD, target)
}

func (m *Model) transientTUICommand(caps ReviewCapabilities, request ReviewRequest) (*exec.Cmd, string, error) {
	file, err := os.CreateTemp("", "pitago-annotate-*.md")
	if err != nil {
		return nil, "", err
	}
	path := file.Name()
	if _, err := io.WriteString(file, request.Content); err != nil {
		file.Close()
		os.Remove(path)
		return nil, "", err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return nil, "", err
	}
	cmd := exec.Command(caps.TUI, path)
	cmd.Dir = request.CWD
	// The TUI keys its record on this path, so the host needs the same string back.
	return cmd, path, nil
}

type reviewDoneMsg struct {
	Request    ReviewRequest
	Surface    ReviewSurface
	Err        error
	LaunchedAt time.Time
	SourcePath string
}

// reviewDeliveredMsg reports the outcome of sending captured feedback to Pi.
type reviewDeliveredMsg struct {
	Capture ReviewCapture
	Err     error
}

func (m *Model) OpenAnnotate(arg string) tea.Cmd {
	arg = strings.TrimSpace(arg)
	if arg == "doctor" {
		m.AnnotateDoctor()
		return nil
	}
	if strings.HasPrefix(arg, "surface ") {
		m.setAnnotateSurface(strings.TrimSpace(strings.TrimPrefix(arg, "surface ")))
		return nil
	}
	request, err := m.annotateRequest(arg)
	if err != nil {
		m.AddBlock(Block{Kind: "notice", Text: err.Error(), Err: true})
		return nil
	}
	caps := DetectReviewCapabilities(request)
	surface, err := SelectReviewSurface(request, caps)
	if err != nil {
		// A preference the machine cannot honor must name the reason and the way out.
		text := err.Error()
		m.AddBlock(Block{Kind: "notice", Text: text, Err: true})
		m.Refresh()
		return nil
	}
	m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("annotate · %s · %s", request.Kind, surface)})
	m.Refresh()
	launchedAt := time.Now()

	switch surface {
	case ReviewSurfaceTUI:
		if request.Kind == ReviewLastMessage {
			request.Content = lastAssistantText(m.blocks)
			if strings.TrimSpace(request.Content) == "" {
				m.AddBlock(Block{Kind: "notice", Text: "annotate last unavailable: no assistant message in this Pitago session", Err: true})
				return nil
			}
		}
		if request.Kind == ReviewLastMessage || request.Kind == ReviewSelection {
			cmd, tempPath, tempErr := m.transientTUICommand(caps, request)
			if tempErr != nil {
				m.AddBlock(Block{Kind: "notice", Text: "annotate TUI handoff failed: " + tempErr.Error(), Err: true})
				return nil
			}
			return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
				_ = os.Remove(tempPath)
				return reviewDoneMsg{Request: request, Surface: surface, Err: runErr, LaunchedAt: launchedAt, SourcePath: tempPath}
			})
		}
		target, _ := reviewTarget(request)
		cmd := exec.Command(caps.TUI, target)
		cmd.Dir = request.CWD
		cmd.Env = append(os.Environ(), "PLANNOTATOR_TUI_PLACEMENT="+request.Placement, "PLANNOTATOR_TUI_CWD="+request.CWD)
		return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
			return reviewDoneMsg{Request: request, Surface: surface, Err: runErr, LaunchedAt: launchedAt, SourcePath: target}
		})
	default:
		status := clipboard.Write(reviewClipboardText(request))
		if status.Err != nil || status.Channel == clipboard.None {
			if status.Err == nil {
				status.Err = errors.New("no clipboard backend available")
			}
			m.AddBlock(Block{Kind: "notice", Text: "annotate unavailable · clipboard copy failed: " + status.Err.Error(), Err: true})
		} else {
			m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("annotate target copied (%d chars · %s) · no review UI detected", status.Chars, status.Channel)})
		}
		m.Refresh()
		return nil
	}
}

func (m *Model) annotateRequest(arg string) (ReviewRequest, error) {
	arg = strings.TrimSpace(arg)
	// The input layer's @ picker leaves the completed token in the command
	// argument. Resolve it here instead of sending a literal "@file" to the
	// review binary. Quoted paths arrive as @"a b.md".
	if strings.HasPrefix(arg, "@") {
		arg = strings.TrimPrefix(arg, "@")
		if strings.HasPrefix(arg, `"`) {
			if unquoted, err := strconv.Unquote(arg); err == nil {
				arg = unquoted
			}
		}
	}
	kind := ReviewFolder
	target := "."
	if arg == "last" {
		kind = ReviewLastMessage
		target = ""
	} else if arg != "" {
		target = arg
		if st, err := os.Stat(arg); err == nil && st.IsDir() {
			kind = ReviewFolder
		} else {
			kind = ReviewFile
		}
	}
	prefs := LoadPrefs(m.prefsPath)
	return ReviewRequest{
		Kind:          kind,
		Target:        target,
		CWD:           m.cwd,
		SessionID:     m.session,
		Preferred:     ReviewSurface(prefs.AnnotateSurface),
		Placement:     prefs.AnnotatePlacement,
		TUIExecutable: prefs.AnnotateTUIExecutable,
	}, nil
}

func (m *Model) setAnnotateSurface(value string) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != string(ReviewSurfaceClipboard) {
		req, _ := m.annotateRequest("")
		caps := DetectReviewCapabilities(req)
		if caps.TUI == "" {
			m.AddBlock(Block{Kind: "notice", Text: "annotate preference not changed: no Plannotator TUI or web command is available", Err: true})
			m.Refresh()
			return
		}
	}
	switch ReviewSurface(value) {
	case ReviewSurfaceAuto, ReviewSurfaceTUI, ReviewSurfaceClipboard:
		prefs := LoadPrefs(m.prefsPath)
		prefs.AnnotateSurface = value
		if err := SavePrefs(m.prefsPath, prefs); err != nil {
			m.AddBlock(Block{Kind: "notice", Text: "annotate preference not saved: " + err.Error(), Err: true})
		} else {
			m.AddBlock(Block{Kind: "notice", Text: "annotate surface → " + value})
		}
	default:
		m.AddBlock(Block{Kind: "notice", Text: "annotate surface must be auto, tui, or clipboard", Err: true})
	}
	m.Refresh()
}

func (m *Model) OpenAnnotateSelection() tea.Cmd {
	if strings.TrimSpace(m.LastSelection) == "" {
		m.AddBlock(Block{Kind: "notice", Text: "annotate selection unavailable: select chat text first", Err: true})
		return nil
	}
	req, err := m.annotateRequest("")
	if err != nil {
		m.AddBlock(Block{Kind: "notice", Text: err.Error(), Err: true})
		return nil
	}
	req.Kind = ReviewSelection
	req.Content = m.LastSelection
	caps := DetectReviewCapabilities(req)
	if caps.TUI == "" {
		m.AddBlock(Block{Kind: "notice", Text: "annotate selection unavailable: plannotator-tui is not installed; selection remains on the clipboard", Err: true})
		return nil
	}
	cmd, tempPath, tempErr := m.transientTUICommand(caps, req)
	if tempErr != nil {
		m.AddBlock(Block{Kind: "notice", Text: "annotate TUI handoff failed: " + tempErr.Error(), Err: true})
		return nil
	}
	m.AddBlock(Block{Kind: "notice", Text: "annotate · selection · tui"})
	m.Refresh()
	launchedAt := time.Now()
	return tea.ExecProcess(cmd, func(runErr error) tea.Msg {
		_ = os.Remove(tempPath)
		return reviewDoneMsg{Request: req, Surface: ReviewSurfaceTUI, Err: runErr, LaunchedAt: launchedAt, SourcePath: tempPath}
	})
}

func (m *Model) AnnotateDoctor() {
	req, _ := m.annotateRequest("")
	caps := DetectReviewCapabilities(req)
	selected, err := SelectReviewSurface(req, caps)
	lines := []string{
		"Pi RPC delivery       " + yesNo(caps.Pi),
		"plannotator-tui        " + valueOr(caps.TUI, "not found"),
		"Plannotator data dir   " + reviewDataDir(),
		"Browser                " + yesNo(caps.Browser),
	}
	if err != nil {
		lines = append(lines, "Current surface        unavailable: "+err.Error())
	} else {
		lines = append(lines, "Current surface        "+string(selected))
	}
	m.AddBlock(Block{Kind: "notice", Text: strings.Join(lines, "\n")})
	m.Refresh()
}

func yesNo(v bool) string {
	if v {
		return "available"
	}
	return "unavailable"
}
func valueOr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// restoreMouseCmd re-asserts the terminal's mouse mode after an external TUI
// handoff (tea.ExecProcess). Bubbletea's RestoreTerminal brings back the alt
// screen, bracketed paste, and focus reporting, but it never re-emits the
// mouse-enable sequence — so returning from a sub-TUI leaves mouse mode off
// and drag-select / right-click silently die until the user runs /mouse again.
func (m *Model) restoreMouseCmd() tea.Cmd {
	if m.Mouse && m.ready {
		return tea.EnableMouseCellMotion
	}
	return nil
}

// handleReviewDone is the dispatch step: surfaces only launch a review, and this is
// where a finished review becomes a result and then a Pi turn. Delivery lives here so
// no surface implementation has to know how feedback reaches the agent.
func (m *Model) handleReviewDone(msg reviewDoneMsg) tea.Cmd {
	if msg.Err != nil {
		m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("annotate %s failed: %s", msg.Surface, msg.Err), Err: true})
		m.Refresh()
		return nil
	}
	switch msg.Surface {
	case ReviewSurfaceTUI:
		capture, err := captureReview(msg.Request, msg.LaunchedAt, msg.SourcePath)
		if err != nil {
			m.AddBlock(Block{Kind: "notice", Text: "annotate could not read the review result: " + err.Error(), Err: true})
			m.Refresh()
			return nil
		}
		capture.Surface = msg.Surface
		if capture.Outcome == reviewOutcomeDismissed {
			m.AddBlock(Block{Kind: "notice", Text: "annotate tui closed · nothing was sent"})
			m.PendingReview = nil
			m.Refresh()
			return nil
		}
		m.PendingReview = &capture
		verb := "annotations"
		if capture.AnnotationCount == 1 {
			verb = "annotation"
		}
		m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("annotate tui sent %d %s · %s · delivering to Pi", capture.AnnotationCount, verb, capture.Outcome)})
		m.Refresh()
		return m.deliverPendingReview()
	default:
		m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("annotate %s closed · review target is on the clipboard", msg.Surface)})
		m.Refresh()
		return nil
	}
}

// deliverPendingReview sends the captured feedback as Pi's next user turn. The
// capture is kept until delivery succeeds, so a failed send stays recoverable.
func (m *Model) deliverPendingReview() tea.Cmd {
	capture := m.PendingReview
	if capture == nil {
		return nil
	}
	if m.Pi == nil {
		m.AddBlock(Block{Kind: "notice", Text: "annotate could not deliver: Pi RPC is not connected · review feedback is kept for /annotate retry", Err: true})
		m.Refresh()
		return nil
	}
	m.AddBlock(Block{Kind: "notice", Text: "annotate · sending review feedback to Pi…"})
	m.Refresh()
	feedback := capture.Feedback
	return func() tea.Msg {
		_, err := m.Pi.Prompt(feedback)
		return reviewDeliveredMsg{Capture: *capture, Err: err}
	}
}

// handleReviewDelivered reports the send and clears the pending capture on success.
func (m *Model) handleReviewDelivered(msg reviewDeliveredMsg) {
	if msg.Err != nil {
		m.AddBlock(Block{Kind: "notice", Text: "annotate delivery failed: " + msg.Err.Error() + " · feedback kept, retry with /annotate retry", Err: true})
		m.Refresh()
		return
	}
	m.PendingReview = nil
	m.AddBlock(Block{Kind: "notice", Text: fmt.Sprintf("annotate · %d annotation(s) delivered to Pi as the next turn", msg.Capture.AnnotationCount)})
	m.Refresh()
}

// RetryReview re-sends feedback that could not be delivered.
func (m *Model) RetryReview() tea.Cmd {
	if m.PendingReview == nil {
		m.AddBlock(Block{Kind: "notice", Text: "annotate retry: no review feedback is waiting"})
		m.Refresh()
		return nil
	}
	return m.deliverPendingReview()
}
