package app

// Dispatch-side tests: what happens after a review surface returns. These cover the
// three normalized outcomes and the recoverable delivery failure.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHandleReviewDoneNormalizesDismissedWhenNothingWasSent(t *testing.T) {
	docDir := t.TempDir()
	t.Setenv("PLANNOTATOR_DATA_DIR", t.TempDir())
	m := &Model{cwd: docDir}
	cmd := m.handleReviewDone(reviewDoneMsg{
		Request:    ReviewRequest{Kind: ReviewFile, Target: "plan.md", CWD: docDir},
		Surface:    ReviewSurfaceTUI,
		LaunchedAt: time.Now(),
	})
	if cmd != nil {
		t.Fatal("a dismissed review has nothing to deliver")
	}
	if m.PendingReview != nil {
		t.Fatal("a dismissed review must not leave feedback pending")
	}
	if !strings.Contains(m.lastNotice(), "nothing was sent") {
		t.Fatalf("expected a dismissed notice, got %q", m.lastNotice())
	}
}

func TestHandleReviewDoneNormalizesApprovedAndKeepsFeedbackPending(t *testing.T) {
	dataDir := t.TempDir()
	docDir := t.TempDir()
	t.Setenv("PLANNOTATOR_DATA_DIR", dataDir)

	doc := filepath.Join(docDir, "plan.md")
	source := "# Plan\n\nShip it.\n"
	if err := os.WriteFile(doc, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Add(-time.Minute)
	start := strings.Index(source, "Ship it.")
	writeSentRecord(t, dataDir, doc, source, []sentAnnotation{{
		ID:      "a",
		Kind:    "looks_good",
		Body:    "",
		Start:   start,
		End:     start + len("Ship it."),
		Deliver: now,
	}})

	m := &Model{cwd: docDir}
	cmd := m.handleReviewDone(reviewDoneMsg{
		Request:    ReviewRequest{Kind: ReviewFile, Target: doc, CWD: docDir},
		Surface:    ReviewSurfaceTUI,
		LaunchedAt: now,
	})
	if m.PendingReview == nil {
		t.Fatal("an approved review must be kept for delivery")
	}
	if m.PendingReview.Outcome != reviewOutcomeApproved {
		t.Fatalf("outcome = %q, want approved", m.PendingReview.Outcome)
	}
	if m.PendingReview.AnnotationCount != 1 {
		t.Fatalf("annotation count = %d", m.PendingReview.AnnotationCount)
	}
	if !strings.Contains(m.PendingReview.Feedback, "Looks good:") {
		t.Fatalf("feedback not rendered: %q", m.PendingReview.Feedback)
	}
	// Pi is a concrete client, so delivery itself needs the live RPC. With no
	// connection the feedback must stay pending and say how to recover.
	if cmd != nil {
		t.Fatal("no Pi connection means no delivery command")
	}
	if !strings.Contains(m.lastNotice(), "/annotate retry") {
		t.Fatalf("the notice should name the recovery command, got %q", m.lastNotice())
	}
}

func TestHandleReviewDeliveredKeepsFeedbackRecoverableOnFailure(t *testing.T) {
	capture := ReviewCapture{Outcome: reviewOutcomeAnnotated, AnnotationCount: 2, Feedback: "# Annotations on plan.md"}
	m := &Model{PendingReview: &capture}
	m.handleReviewDelivered(reviewDeliveredMsg{Capture: capture, Err: errors.New("pi exited")})
	if m.PendingReview == nil {
		t.Fatal("a failed delivery must keep the feedback for retry")
	}
	if !strings.Contains(m.lastNotice(), "/annotate retry") {
		t.Fatalf("the notice should name the recovery command, got %q", m.lastNotice())
	}
}

func TestHandleReviewDeliveredClearsPendingOnSuccess(t *testing.T) {
	capture := ReviewCapture{Outcome: reviewOutcomeAnnotated, AnnotationCount: 1, Feedback: "# Annotations on plan.md"}
	m := &Model{PendingReview: &capture}
	m.handleReviewDelivered(reviewDeliveredMsg{Capture: capture})
	if m.PendingReview != nil {
		t.Fatal("a delivered review must not stay pending")
	}
	if !strings.Contains(m.lastNotice(), "delivered to Pi") {
		t.Fatalf("notice = %q", m.lastNotice())
	}
}

func TestRetryReviewWithoutPendingFeedbackIsHonest(t *testing.T) {
	m := &Model{}
	if cmd := m.RetryReview(); cmd != nil {
		t.Fatal("nothing to retry means no command")
	}
	if !strings.Contains(m.lastNotice(), "no review feedback is waiting") {
		t.Fatalf("notice = %q", m.lastNotice())
	}
}

func TestHandleReviewDoneReportsSurfaceFailure(t *testing.T) {
	m := &Model{}
	if cmd := m.handleReviewDone(reviewDoneMsg{Surface: ReviewSurfaceTUI, Err: errors.New("exit status 1")}); cmd != nil {
		t.Fatal("a failed review has nothing to deliver")
	}
	if !strings.Contains(m.lastNotice(), "exit status 1") {
		t.Fatalf("notice = %q", m.lastNotice())
	}
}

type sentAnnotation struct {
	ID      string
	Kind    string
	Body    string
	Start   int
	End     int
	Deliver time.Time
}

// writeSentRecord writes the record plannotator-tui would have written after a send:
// the annotation archived, with a delivery covering its id.
func writeSentRecord(t *testing.T, dataDir, doc, source string, sent []sentAnnotation) {
	t.Helper()
	now := time.Now().Format("2006-01-02T15:04:05.000Z")
	record := &reviewRecord{Path: doc}
	var ids []string
	for _, s := range sent {
		ids = append(ids, s.ID)
		record.Archived = append(record.Archived, reviewAnnotation{
			ID:   s.ID,
			Body: s.Body,
			Anchor: reviewAnchor{Quote: source[s.Start:s.End], TUI: &reviewTUIAnchor{
				Kind:   s.Kind,
				Quote:  source[s.Start:s.End],
				Source: reviewSourceRange{Start: s.Start, End: s.End},
			}},
			Replies: nil,
		})
	}
	for _, s := range sent {
		record.Deliveries = append(record.Deliveries, reviewDelivery{
			At: s.Deliver.Format("2006-01-02T15:04:05.000Z"), Target: "clipboard", AnnotationIDs: []string{s.ID},
		})
	}
	_ = now
	path := reviewRecordPath(dataDir, reviewProjectNameFor(filepath.Dir(doc)), doc)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeReviewRecord(path, record); err != nil {
		t.Fatal(err)
	}
}

// lastNotice returns the most recent toast, which is where notices are routed.
func (m *Model) lastNotice() string {
	if len(m.toasts) == 0 {
		return ""
	}
	return m.toasts[len(m.toasts)-1].Text
}
