package app

// The vectors below are lifted from plannotator-tui's own Rust tests
// (datadir.rs, export.rs). Pitago and plannotator-tui must agree byte for byte on
// the data-dir keying and on the feedback text, so these cases are the contract.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func TestReviewDataDirFollowsPlannotatorsOrder(t *testing.T) {
	home := "/home/u"
	legacyExists := func(path string) bool { return path == "/home/u/.plannotator" }
	nothing := func(string) bool { return false }

	cases := []struct {
		name   string
		env    map[string]string
		exists func(string) bool
		want   string
	}{
		{"PLANNOTATOR_DATA_DIR wins and expands ~", map[string]string{"PLANNOTATOR_DATA_DIR": "~/relocated", "XDG_DATA_HOME": "/x"}, legacyExists, "/home/u/relocated"},
		{"PLANNOTATOR_DATA_DIR absolute", map[string]string{"PLANNOTATOR_DATA_DIR": "/custom"}, nothing, "/custom"},
		{"existing legacy dir beats XDG", map[string]string{"XDG_DATA_HOME": "/xdg"}, legacyExists, "/home/u/.plannotator"},
		{"XDG when absolute and legacy absent", map[string]string{"XDG_DATA_HOME": "/xdg"}, nothing, "/xdg/plannotator"},
		{"relative XDG ignored", map[string]string{"XDG_DATA_HOME": "relative"}, nothing, "/home/u/.plannotator"},
		{"blank XDG ignored", map[string]string{"XDG_DATA_HOME": "  "}, nothing, "/home/u/.plannotator"},
		{"nothing set", map[string]string{}, nothing, "/home/u/.plannotator"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reviewDataDirFor(envOf(tc.env), home, tc.exists); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestReviewSanitizeTagMatchesPlannotator(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"My Repo_Name!", "my-repo-name", true},
		{"--a--b--", "a-b", true},
		{"a-very-long-repository-name-that-goes-on", "a-very-long-repository-name-th", true},
	}
	for _, tc := range cases {
		got, ok := reviewSanitizeTag(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("sanitizeTag(%q) = (%q, %v) want (%q, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
	// A single character is not a usable tag: the caller falls through to the next
	// candidate, which is what makes "x" a directory name fall back to its parent.
	if _, ok := reviewSanitizeTag("x"); ok {
		t.Fatal("a one-character tag must be rejected")
	}
}

func TestReviewProjectNameFallsThroughOnShortCandidates(t *testing.T) {
	if got := reviewProjectName("/work/herdr", "/work/herdr/docs"); got != "herdr" {
		t.Fatalf("got %q want herdr", got)
	}
	if got := reviewProjectName("/work/x", "/work/x/notes"); got != "notes" {
		t.Fatalf("got %q want notes", got)
	}
	if got := reviewProjectName("", "/"); got != "_unknown" {
		t.Fatalf("got %q want _unknown", got)
	}
}

func TestReviewHistorySlugMatchesPlannotator(t *testing.T) {
	path := "/Users/ramos/notes/Plan Draft.md"
	slug := reviewHistorySlug(path)
	if !strings.HasPrefix(slug, "annotate-plan-draft-md-") {
		t.Fatalf("slug %q lacks the expected prefix", slug)
	}
	if len(slug) != len("annotate-plan-draft-md-")+8 {
		t.Fatalf("slug %q has the wrong length", slug)
	}
	if reviewHistorySlug("/a/B.md") == reviewHistorySlug("/a/b.md") {
		t.Fatal("the hash input is the path as given, so case must matter")
	}
	if got, want := reviewHistorySlug("/x/"), reviewHistorySlug("/x/"); got != want {
		t.Fatalf("trailing separator should fall back to document: got %q want %q", got, want)
	}
	if !strings.HasPrefix(reviewHistorySlug("/x/"), "annotate-document-") {
		t.Fatalf("trailing separator should use the document base, got %q", reviewHistorySlug("/x/"))
	}
}

func TestReviewFeedbackMatchesTheAgentFacingShape(t *testing.T) {
	source := "# Title\n\nShip the login page by Friday.\n\nDrop the `legacy` path.\n"
	at := func(quote string) (int, int) {
		start := strings.Index(source, quote)
		return start, start + len(quote)
	}
	commentStart, commentEnd := at("login page")
	deleteStart, deleteEnd := at("Drop the `legacy` path.")
	items := []reviewItem{
		{annotation: reviewAnnotation{Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "comment"}}, Body: "Which page?\nBe specific."}, start: commentStart, end: commentEnd},
		{annotation: reviewAnnotation{Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "delete"}}}, start: deleteStart, end: deleteEnd},
	}
	out := reviewFeedback(source, "plan.md", items)
	want := "# Annotations on plan.md\n\n" +
		"## Annotation 1 (line 3)\nComment on: \"login page\"\n> Which page?\n> Be specific.\n\n" +
		"## Annotation 2 (line 5)\nRemove this:\n```\nDrop the `legacy` path.\n```\n> I don't want this.\n\n"
	if out != want {
		t.Fatalf("feedback mismatch\n got: %q\nwant: %q", out, want)
	}
}

func TestReviewFenceGrowsPastEmbeddedBackticks(t *testing.T) {
	if !strings.HasPrefix(reviewFence("has ``` inside"), "````\n") {
		t.Fatalf("fence did not grow: %q", reviewFence("has ``` inside"))
	}
}

func TestReviewFeedbackWithNoAnnotations(t *testing.T) {
	if got := reviewFeedback("body", "plan.md", nil); got != "No annotations." {
		t.Fatalf("got %q", got)
	}
}

func TestReviewOutcomeClassification(t *testing.T) {
	looksGood := []reviewItem{{annotation: reviewAnnotation{Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "looks_good"}}}}}
	comment := []reviewItem{{annotation: reviewAnnotation{Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "comment"}}}}}
	if got := reviewOutcome(nil); got != reviewOutcomeDismissed {
		t.Fatalf("empty review should be dismissed, got %q", got)
	}
	if got := reviewOutcome(looksGood); got != reviewOutcomeApproved {
		t.Fatalf("all-approval review should be approved, got %q", got)
	}
	if got := reviewOutcome(comment); got != reviewOutcomeAnnotated {
		t.Fatalf("a comment review should be annotated, got %q", got)
	}
}

// sentSince must ignore deliveries from an earlier session and pick annotations out
// of the archive, which is where a sent annotation ends up.
func TestReviewSentSinceUsesArchiveAndIgnoresOlderSends(t *testing.T) {
	launched := time.Date(2026, 9, 25, 19, 0, 0, 0, time.UTC)
	record := &reviewRecord{
		Deliveries: []reviewDelivery{
			{At: "2026-09-25T18:00:00.000Z", Target: "clipboard", AnnotationIDs: []string{"old"}},
			{At: "2026-09-25T19:00:05.000Z", Target: "clipboard", AnnotationIDs: []string{"a", "b"}},
			{At: "2026-09-25T19:00:09.000Z", Target: "clipboard", AnnotationIDs: []string{"a"}},
		},
		Archived: []reviewAnnotation{
			{ID: "a", Body: "first", Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "comment"}}},
			{ID: "b", Body: "second", Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "delete"}}},
		},
		Annotations: []reviewAnnotation{{ID: "unsent", Body: "pending", Anchor: reviewAnchor{TUI: &reviewTUIAnchor{Kind: "comment"}}}},
	}
	items, deliveredAt, targets := reviewCaptureRecord(record, "some source", "plan.md", launched)
	if len(items) != 2 {
		t.Fatalf("expected the two sent annotations, got %d", len(items))
	}
	if items[0].annotation.ID != "a" || items[1].annotation.ID != "b" {
		t.Fatalf("sent order not preserved: %q then %q", items[0].annotation.ID, items[1].annotation.ID)
	}
	if deliveredAt != "2026-09-25T19:00:09.000Z" {
		t.Fatalf("deliveredAt = %q", deliveredAt)
	}
	if strings.Join(targets, ",") != "clipboard,clipboard" {
		t.Fatalf("targets = %v", targets)
	}
}

// A record the TUI never wrote means nothing was annotated: dismissed, not an error.
func TestCaptureReviewMissingRecordIsDismissed(t *testing.T) {
	t.Setenv("PLANNOTATOR_DATA_DIR", t.TempDir())
	req := ReviewRequest{Kind: ReviewFile, Target: "plan.md", CWD: t.TempDir()}
	capture, err := captureReview(req, time.Now(), "")
	if err != nil {
		t.Fatalf("a missing record must not be an error: %v", err)
	}
	if capture.Outcome != reviewOutcomeDismissed {
		t.Fatalf("outcome = %q", capture.Outcome)
	}
}

// End-to-end against a record on disk: the capture is annotated and the feedback
// quotes the document text that was actually reviewed.
func TestCaptureReviewReadsSentFeedbackFromDisk(t *testing.T) {
	dataDir := t.TempDir()
	docDir := t.TempDir()
	doc := filepath.Join(docDir, "plan.md")
	source := "# Plan\n\nShip it by Friday.\n"
	if err := os.WriteFile(doc, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	record := &reviewRecord{
		Path: doc,
		Deliveries: []reviewDelivery{
			{At: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), Target: "clipboard", AnnotationIDs: []string{"a"}},
		},
		Archived: []reviewAnnotation{{
			ID:        "a",
			Body:      "too tight",
			UpdatedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000Z"),
			Anchor: reviewAnchor{Quote: "Ship it by Friday.", TUI: &reviewTUIAnchor{
				Kind:   "comment",
				Quote:  "Ship it by Friday.",
				Source: reviewSourceRange{Start: strings.Index(source, "Ship it by Friday."), End: strings.Index(source, "Ship it by Friday.") + len("Ship it by Friday.")},
			}},
		}},
	}
	dir := reviewAnnotationsDir(dataDir, "plan-proj", doc)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeReviewRecord(filepath.Join(dir, "annotations.json"), record); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PLANNOTATOR_DATA_DIR", dataDir)
	// The project name is derived from the document's folder; look the record up the
	// same way the TUI wrote it.
	project := reviewProjectNameFor(docDir)
	moved := filepath.Join(reviewAnnotationsDir(dataDir, project, doc), "annotations.json")
	if moved != filepath.Join(dir, "annotations.json") {
		if err := os.MkdirAll(filepath.Dir(moved), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := writeReviewRecord(moved, record); err != nil {
			t.Fatal(err)
		}
	}

	capture, err := captureReview(ReviewRequest{Kind: ReviewFile, Target: doc}, time.Now().Add(-time.Minute), "")
	if err != nil {
		t.Fatal(err)
	}
	if capture.Outcome != reviewOutcomeAnnotated {
		t.Fatalf("outcome = %q", capture.Outcome)
	}
	if capture.AnnotationCount != 1 {
		t.Fatalf("annotation count = %d", capture.AnnotationCount)
	}
	if !strings.Contains(capture.Feedback, "Comment on: \"Ship it by Friday.\"") {
		t.Fatalf("feedback did not quote the document: %q", capture.Feedback)
	}
	if !strings.Contains(capture.Feedback, "> too tight") {
		t.Fatalf("feedback lost the note body: %q", capture.Feedback)
	}
	if capture.Target != "clipboard" {
		t.Fatalf("target = %q", capture.Target)
	}
}
