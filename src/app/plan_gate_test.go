package app

import (
	"encoding/json"
	"testing"

	"pitago/src/extension"
)

func planGateRaw(method, title string, options ...string) []byte {
	req := struct {
		ID      string   `json:"id"`
		Method  string   `json:"method"`
		Title   string   `json:"title"`
		Options []string `json:"options"`
	}{ID: "req-1", Method: method, Title: title, Options: options}
	out, _ := json.Marshal(req)
	return out
}

func TestIsPlanGateMatchesOnlyTheSentinel(t *testing.T) {
	cases := []struct {
		name string
		raw  []byte
		want bool
	}{
		{"sentinel select", planGateRaw("select", extension.PlanGateTitle, "{}"), true},
		{"sentinel with payload", planGateRaw("select", extension.PlanGateTitle, `{"path":"/tmp/p.md"}`), true},
		// A user-facing picker that merely talks about plans must not be hijacked.
		{"ordinary title", planGateRaw("select", "Plan review"), false},
		{"empty title", planGateRaw("select", ""), false},
		{"sentinel on wrong method", planGateRaw("confirm", extension.PlanGateTitle), false},
		{"sentinel as a prefix", planGateRaw("select", extension.PlanGateTitle+"x"), false},
		{"garbage", []byte("not json"), false},
	}
	for _, tc := range cases {
		if got := extension.IsPlanGate(tc.raw); got != tc.want {
			t.Errorf("%s: IsPlanGate = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The gate borrows the select transport, so it must not be parked in the dialog
// queue: a parked request is promoted by drainQueuedDialogs, which cannot
// return the tea.Cmd the terminal handoff needs.
func TestPlanGateIsNotQueuedAsADialog(t *testing.T) {
	if extension.IsDialogRequest(planGateRaw("select", extension.PlanGateTitle, "{}")) {
		t.Error("a plan gate must not be treated as a dialog request")
	}
	// Real dialogs still queue.
	for _, m := range []string{"select", "confirm", "input", "editor"} {
		if !extension.IsDialogRequest(planGateRaw(m, "Pick one", "a")) {
			t.Errorf("%s should still be a dialog request", m)
		}
	}
	// A non-dialog method with a normal title stays non-dialog.
	if extension.IsDialogRequest(planGateRaw("notify", "hi")) {
		t.Error("notify must not be a dialog request")
	}
}

// A malformed gate must answer rather than leave the extension blocked forever.
func TestPlanGateRejectsBadPayloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"no options", planGateRaw("select", extension.PlanGateTitle)},
		{"options not json", planGateRaw("select", extension.PlanGateTitle, "not json")},
	} {
		pi, lines := spawnFakePi(t, nil)
		m := New(nil, t.TempDir())
		m.Pi = pi
		m = m.planGateRequest(tc.raw)
		if m.planGateID != "" {
			t.Errorf("%s: parked a request id for an unusable payload", tc.name)
		}
		if _, ok := lastPlanGateResponse(t, waitForCmds(t, lines, 1)); !ok {
			t.Errorf("%s: nothing was answered; the extension would block until timeout", tc.name)
		}
	}
}

func TestPlanGateResultRoundTrips(t *testing.T) {
	encoded, err := json.Marshal(PlanGateResult{Approved: true, Feedback: "# ok"})
	if err != nil {
		t.Fatal(err)
	}
	var back PlanGateResult
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if !back.Approved || back.Feedback != "# ok" {
		t.Errorf("round trip lost data: %+v", back)
	}
}

// Only an all-looks-good send is an approval; anything else is a denial with
// feedback. The gate must never report approval for a dismissed review.
func TestAnswerPlanGateClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		capture ReviewCapture
		want    PlanGateResult
	}{
		{"approved", ReviewCapture{Outcome: reviewOutcomeApproved}, PlanGateResult{Approved: true}},
		{"annotated", ReviewCapture{Outcome: reviewOutcomeAnnotated, Feedback: "# notes"}, PlanGateResult{Feedback: "# notes"}},
		{"dismissed", ReviewCapture{Outcome: reviewOutcomeDismissed}, PlanGateResult{Dismissed: true}},
	} {
		pi, lines := spawnFakePi(t, nil)
		m := &Model{planGateID: "req-1"}
		m.Pi = pi
		if !m.answerPlanGate(tc.capture, nil) {
			t.Fatalf("%s: answerPlanGate reported no parked gate", tc.name)
		}
		got, ok := lastPlanGateResponse(t, waitForCmds(t, lines, 1))
		if !ok {
			t.Fatalf("%s: no response was sent", tc.name)
		}
		if got.Approved != tc.want.Approved || got.Feedback != tc.want.Feedback || got.Dismissed != tc.want.Dismissed {
			t.Errorf("%s: got %+v, want %+v", tc.name, got, tc.want)
		}
		if m.planGateID != "" {
			t.Errorf("%s: gate id was not cleared; a second review would answer a stale request", tc.name)
		}
	}
}

// The parked id is one-shot: a second review with nothing parked must not fire.
func TestAnswerPlanGateIsNoOpWithoutAGate(t *testing.T) {
	pi, lines := spawnFakePi(t, nil)
	m := &Model{}
	m.Pi = pi
	if m.answerPlanGate(ReviewCapture{Outcome: reviewOutcomeApproved}, nil) {
		t.Error("answerPlanGate fired with no gate parked")
	}
	if _, ok := lastPlanGateResponse(t, waitForCmds(t, lines, 1)); ok {
		t.Error("a stray response was sent for an ordinary /annotate review")
	}
}

func TestAnswerPlanGateReportsLaunchErrors(t *testing.T) {
	pi, lines := spawnFakePi(t, nil)
	m := &Model{planGateID: "req-1"}
	m.Pi = pi
	if !m.answerPlanGate(ReviewCapture{}, errTest) {
		t.Fatal("expected the gate to be answered")
	}
	got, ok := lastPlanGateResponse(t, waitForCmds(t, lines, 1))
	if !ok {
		t.Fatal("no response was sent")
	}
	if got.Error == "" {
		t.Error("a failed review must carry an error, not look like a denial")
	}
}
