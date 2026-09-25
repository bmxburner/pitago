package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reviewRequest(t *testing.T) ReviewRequest {
	t.Helper()
	return ReviewRequest{
		Kind:      ReviewFile,
		Target:    "plans/test.md",
		CWD:       t.TempDir(),
		Preferred: ReviewSurfaceAuto,
	}
}

func TestSelectReviewSurfaceAutoPrefersTUI(t *testing.T) {
	req := reviewRequest(t)
	got, err := SelectReviewSurface(req, ReviewCapabilities{TUI: "/usr/local/bin/plannotator-tui"})
	if err != nil || got != ReviewSurfaceTUI {
		t.Fatalf("got %q, %v; want tui", got, err)
	}
}

func TestSelectReviewSurfaceForcedUnavailable(t *testing.T) {
	req := reviewRequest(t)
	req.Preferred = ReviewSurfaceTUI
	if _, err := SelectReviewSurface(req, ReviewCapabilities{}); err == nil {
		t.Fatal("forced unavailable TUI should fail")
	}
}

func TestSelectReviewSurfaceFallsBackToClipboard(t *testing.T) {
	req := reviewRequest(t)
	got, err := SelectReviewSurface(req, ReviewCapabilities{})
	if err != nil || got != ReviewSurfaceClipboard {
		t.Fatalf("got %q, %v; want clipboard", got, err)
	}
}

func TestReviewRequestValidate(t *testing.T) {
	if err := (ReviewRequest{Kind: ReviewFile, CWD: "/tmp"}).validate(); err == nil {
		t.Fatal("file request without target should fail")
	}
	if err := (ReviewRequest{Kind: ReviewLastMessage, CWD: "/tmp"}).validate(); err != nil {
		t.Fatalf("last-message request should be valid: %v", err)
	}
}

func TestAnnotatePrefsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "prefs.json")
	want := Prefs{AnnotateSurface: "clipboard", AnnotatePlacement: "popup"}
	if err := SavePrefs(path, want); err != nil {
		t.Fatal(err)
	}
	got := LoadPrefs(path)
	if got.AnnotateSurface != want.AnnotateSurface || got.AnnotatePlacement != want.AnnotatePlacement {
		t.Fatalf("prefs not persisted: %+v", got)
	}
}

func TestReviewTargetIsResolvedInsideCwd(t *testing.T) {
	req := reviewRequest(t)
	req.CWD = t.TempDir()
	target, err := reviewTarget(req)
	if err != nil || !filepath.IsAbs(target) {
		t.Fatalf("target %q, err %v", target, err)
	}
	rel, err := filepath.Rel(req.CWD, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("target escaped cwd: %q", target)
	}
}

func TestDetectReviewCapabilitiesFindsExplicitTUI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "plannotator-tui")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	caps := DetectReviewCapabilities(ReviewRequest{TUIExecutable: bin})
	if caps.TUI != bin {
		t.Fatalf("unexpected capabilities: %+v", caps)
	}
}

func TestAnnotateLastAssistantText(t *testing.T) {
	blocks := []Block{{Kind: "assistant", Text: "old"}, {Kind: "tool", Text: "tool"}, {Kind: "assistant", Text: "new"}}
	if got := lastAssistantText(blocks); got != "new" {
		t.Fatalf("got %q, want new", got)
	}
	if got := lastAssistantText([]Block{{Kind: "tool"}}); got != "" {
		t.Fatalf("empty assistant result, got %q", got)
	}
}

func TestAnnotateRequestResolvesMentionPath(t *testing.T) {
	m := New(nil, t.TempDir())
	m.sessionFile = "/tmp/pi-session.jsonl"
	m.prefsPath = filepath.Join(t.TempDir(), "prefs.json")
	m.Cmds = nil
	req, err := m.annotateRequest(`@"plans/a b.md"`)
	if err != nil || req.Target != "plans/a b.md" {
		t.Fatalf("target %q, err %v", req.Target, err)
	}
}
