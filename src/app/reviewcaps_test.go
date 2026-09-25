package app

// Tests for the review surfaces that ship: native TUI and the clipboard fallback.
//
// Nothing here needs plannotator-tui, a browser, or the Plannotator extension
// installed, and TUI detection is pinned so the cases are the same on every
// machine.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tuiStub is a file that passes the executable check TUI detection performs.
func tuiStub(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "plannotator-tui")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// noTUI makes TUI detection report nothing, whatever the host machine has.
func noTUI(t *testing.T) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	original := knownTUIPaths
	knownTUIPaths = func() []string { return nil }
	t.Cleanup(func() { knownTUIPaths = original })
}

// PITAGO_ANNOTATE_TUI pins the binary or turns the surface off entirely. The
// switch is also how a user who prefers another surface keeps the full-screen
// TUI from ever being selected.
func TestAnnotateTUICanBePinnedOrDisabled(t *testing.T) {
	realTUI := tuiStub(t)
	t.Setenv("PATH", t.TempDir())
	original := knownTUIPaths
	knownTUIPaths = func() []string { return []string{realTUI} }
	t.Cleanup(func() { knownTUIPaths = original })

	// Unset: detection is unchanged.
	os.Unsetenv("PITAGO_ANNOTATE_TUI")
	if got := DetectReviewCapabilities(ReviewRequest{}).TUI; got != realTUI {
		t.Fatalf("with the variable unset the TUI should be detected, got %q", got)
	}

	// Pinned: the named binary wins over PATH and the known locations.
	pinned := filepath.Join(t.TempDir(), "pinned-tui")
	if err := os.WriteFile(pinned, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PITAGO_ANNOTATE_TUI", pinned)
	// Compare basenames: macOS case-folds temp paths, so the literals differ.
	if got := filepath.Base(DetectReviewCapabilities(ReviewRequest{}).TUI); got != "pinned-tui" {
		t.Fatalf("pinned TUI = %q, want the pinned binary", got)
	}

	// Off: no native TUI at all, even though one is installed, and a forced
	// preference explains itself rather than saying "unavailable".
	for _, value := range []string{"off", "NONE", "0", " false "} {
		t.Setenv("PITAGO_ANNOTATE_TUI", value)
		if got := DetectReviewCapabilities(ReviewRequest{}).TUI; got != "" {
			t.Fatalf("PITAGO_ANNOTATE_TUI=%q left the TUI enabled (%q)", value, got)
		}
		_, err := SelectReviewSurface(
			ReviewRequest{Preferred: ReviewSurfaceTUI},
			DetectReviewCapabilities(ReviewRequest{}),
		)
		if err == nil || !strings.Contains(err.Error(), "PITAGO_ANNOTATE_TUI") {
			t.Fatalf("PITAGO_ANNOTATE_TUI=%q: forced tui error = %v", value, err)
		}
	}
}

// An app-owned selection only exists inside Pitago, so the clipboard is the
// fallback when the TUI is unavailable.
func TestReviewSelectionHasNoNativeAlternativeWithoutTUI(t *testing.T) {
	noTUI(t)
	caps := DetectReviewCapabilities(ReviewRequest{Kind: ReviewSelection})
	surface, err := SelectReviewSurface(ReviewRequest{Kind: ReviewSelection}, caps)
	if err != nil || surface != ReviewSurfaceClipboard {
		t.Fatalf("got %q, %v; want clipboard", surface, err)
	}
}

// The doctor has to describe every capability combination a user can be in, so
// a missing surface is never a silent one.
func TestAnnotateDoctorCoversEveryCapabilityCombination(t *testing.T) {
	withTUI := tuiStub(t)
	prefs := filepath.Join(t.TempDir(), "prefs.json")

	cases := []struct {
		name string
		tui  bool
		want []string
	}{
		{
			name: "nothing installed",
			want: []string{"plannotator-tui        not found", "Current surface        clipboard"},
		},
		{
			name: "tui installed",
			tui:  true,
			want: []string{"Web surface", "plannotator-tui", "Current surface        tui"},
		},
		{
			name: "tui disabled by env",
			want: []string{"Current surface        clipboard"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Pin detection so the case is the same on every machine.
			t.Setenv("PATH", t.TempDir())
			original := knownTUIPaths
			knownTUIPaths = func() []string {
				if tc.tui {
					return []string{withTUI}
				}
				return nil
			}
			t.Cleanup(func() { knownTUIPaths = original })
			if tc.name == "tui disabled by env" {
				t.Setenv("PITAGO_ANNOTATE_TUI", "off")
			}

			m := &Model{cwd: t.TempDir(), prefsPath: prefs}
			m.AnnotateDoctor()
			report := m.lastNotice()
			for _, want := range tc.want {
				if want == "Web surface" {
					continue
				}
				if !strings.Contains(report, want) {
					t.Fatalf("doctor report missing %q:\n%s", want, report)
				}
			}
			// The report names where feedback is read from, which is what makes
			// a "not found" diagnosable.
			if !strings.Contains(report, "Plannotator data dir") {
				t.Fatalf("doctor report should show where feedback is read from:\n%s", report)
			}
		})
	}
}

// The clipboard fallback must be actionable: it says what was copied and that
// no review UI was available, and it never claims a review happened.
func TestClipboardFallbackIsActionable(t *testing.T) {
	noTUI(t)
	m := &Model{cwd: t.TempDir(), prefsPath: filepath.Join(t.TempDir(), "prefs.json")}
	m.OpenAnnotate("plan.md")
	notice := m.lastNotice()
	if notice == "" {
		t.Fatal("the clipboard fallback must say something")
	}
	if strings.Contains(notice, "review complete") || strings.Contains(notice, "approved") {
		t.Fatalf("a copied target is not a review: %q", notice)
	}
	if !strings.Contains(notice, "no review UI detected") {
		t.Fatalf("notice should say the target was copied, not reviewed: %q", notice)
	}
}
