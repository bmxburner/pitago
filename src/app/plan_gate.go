package app

// plan_gate.go — the host half of the plannotator plan-gate hand-off.
//
// The plannotator extension's `plannotator_submit_plan` needs to show a plan in
// plannotator-tui. It cannot: it runs inside pi's child process, while pitago
// owns the TTY, and two programs both drawing the terminal cancel each other
// out. Pi's extension API exposes no generic host call — `ctx.ui` is a closed
// set of methods with no arbitrary dispatch — so the extension borrows the
// `select` transport with a sentinel title (extension.PlanGateTitle) and awaits
// the response. Pitago answers by doing the only thing it can: running the
// review through tea.ExecProcess, which is where the alt-screen handoff lives.
//
// This is a channel overload, and it is deliberate. The alternatives were worse:
// spawning the TUI from the extension does not render at all, and a file-based
// request/response would add a poll loop and a tick dependency to the host for
// one caller. The contract is versioned and lives in one place on each side.
//
// The response is the review outcome as JSON. The extension then returns the
// same shape openPlanReviewBrowser resolves to, so the approval path in
// plannotator's index.ts runs unchanged.

import (
	"encoding/json"
	"os"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"pitago/src/extension"
	"pitago/src/pirpc"
)

// PlanGateRequest is the payload the extension sends as the sole select option.
type PlanGateRequest struct {
	// Path is the file the TUI should open. The extension writes a temp copy:
	// the plan is rewritten in place on denial and must not be mutated.
	Path string `json:"path"`
	// PlanName labels the feedback heading only (the path can be a temp name).
	PlanName string `json:"planName"`
}

// PlanGateResult is what pitago answers with.
type PlanGateResult struct {
	Approved  bool   `json:"approved"`
	Feedback  string `json:"feedback,omitempty"`
	Dismissed bool   `json:"dismissed,omitempty"`
	Error     string `json:"error,omitempty"`
}

// handlePlanGate runs one review for an extension and returns the command that
// starts it. The request id is parked on the model so handleReviewDone can
// answer it when the TUI exits.
func (m *Model) handlePlanGate(req PlanGateRequest, id string) tea.Cmd {
	if req.Path == "" {
		m.fireUI(planGateError(id, "plan gate request is missing a path"))
		return nil
	}
	caps := DetectReviewCapabilities(ReviewRequest{CWD: m.cwd})
	if caps.TUI == "" {
		m.fireUI(planGateError(id, "plannotator-tui was not found on PATH"))
		return nil
	}

	// Reuse the /annotate path wholesale so the handoff, placement env and
	// capture stay in one implementation.
	m.planGateID = id
	m.planGateName = req.PlanName
	return m.openPlanGateReview(req.Path, caps)
}

// openPlanGateReview is the TUI branch of OpenAnnotate with delivery removed:
// it launches the review and reports back through reviewDoneMsg like any other
// surface. SourcePath is the reviewed file, which is what captureReview keys on.
func (m *Model) openPlanGateReview(path string, caps ReviewCapabilities) tea.Cmd {
	request := ReviewRequest{
		Kind:      ReviewFile,
		Target:    path,
		CWD:       m.cwd,
		NoDeliver: true,
	}
	m.AddBlock(Block{Kind: "notice", Text: "annotate · plan gate · tui"})
	m.Refresh()
	launchedAt := time.Now()

	// Same shape as the /annotate file branch: placement env so the TUI knows
	// where it is, and SourcePath is the reviewed file, which is what
	// captureReview keys its record lookup on.
	cmd := exec.Command(caps.TUI, path)
	cmd.Dir = request.CWD
	cmd.Env = append(os.Environ(), "PLANNOTATOR_TUI_PLACEMENT=popup", "PLANNOTATOR_TUI_CWD="+request.CWD)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return reviewDoneMsg{
			Request:    request,
			Surface:    ReviewSurfaceTUI,
			Err:        err,
			LaunchedAt: launchedAt,
			SourcePath: path,
		}
	})
}

func planGateError(id, message string) pirpc.Command {
	return planGateResponse(id, PlanGateResult{Error: message})
}

func planGateResponse(id string, result PlanGateResult) pirpc.Command {
	encoded, err := json.Marshal(result)
	if err != nil {
		encoded = []byte(`{"error":"failed to encode plan gate result"}`)
	}
	return extension.TextResponse(id, string(encoded), false)
}

// planGateRequest decodes a gate request and starts the review. It is the value
// receiver's counterpart of handlePlanGate: handleUIRequest has a value receiver
// and must hand back the parked id with the model.
func (m Model) planGateRequest(raw []byte) Model {
	var req pirpc.UIRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		if id, ok := looseUIRequestID(raw); ok {
			m.AddBlock(Block{Kind: "notice", Text: "plan gate request could not be read — cancelled: " + id, Err: true})
			m.fireUI(planGateError(id, "plan gate request could not be read"))
			m.Refresh()
		}
		return m
	}
	if len(req.Options) == 0 {
		m.fireUI(planGateError(req.ID, "plan gate request carried no payload"))
		return m
	}
	var gate PlanGateRequest
	if err := json.Unmarshal([]byte(req.Options[0]), &gate); err != nil {
		m.fireUI(planGateError(req.ID, "plan gate payload is not valid JSON"))
		return m
	}
	m.planGateCmd = m.handlePlanGate(gate, req.ID)
	return m
}

// takePlanGateCmd hands out the pending hand-off command exactly once. It is
// drained at the top of Update because handleUIRequest cannot return a command,
// and the extension is blocked on the response until this runs.
func (m *Model) takePlanGateCmd() tea.Cmd {
	cmd := m.planGateCmd
	m.planGateCmd = nil
	return cmd
}

// answerPlanGate turns a finished review into the extension's response and
// clears the parked id. Returns false when no gate is parked, so the ordinary
// /annotate delivery path stays untouched.
func (m *Model) answerPlanGate(capture ReviewCapture, err error) bool {
	if m.planGateID == "" {
		return false
	}
	id, name := m.planGateID, m.planGateName
	m.planGateID, m.planGateName = "", ""

	switch {
	case err != nil:
		m.fireUI(planGateResponse(id, PlanGateResult{Error: err.Error()}))
	case capture.Outcome == reviewOutcomeDismissed:
		// Closed without pressing Send: not an approval, and no feedback to
		// return. The extension maps this to its "closed before a decision".
		m.fireUI(planGateResponse(id, PlanGateResult{Dismissed: true}))
	default:
		m.fireUI(planGateResponse(id, PlanGateResult{
			Approved: capture.Outcome == reviewOutcomeApproved,
			Feedback: capture.Feedback,
		}))
	}
	_ = name
	return true
}
