package app

import "testing"

// --no-deliver has to survive a quoted path (a temp plan path can contain
// spaces) and must not swallow an ordinary target that merely starts with a dash.
func TestParseNoDeliver(t *testing.T) {
	cases := []struct {
		name     string
		arg      string
		wantPath string
		wantFlag bool
	}{
		{"plain", "--no-deliver plan.md", "plan.md", true},
		{"quoted with space", `--no-deliver "/tmp/a b/plan.md"`, "/tmp/a b/plan.md", true},
		{"extra whitespace", "--no-deliver    plan.md", "plan.md", true},
		{"no remainder", "--no-deliver", "", true},
		{"absent", "plan.md", "plan.md", false},
		{"absent, quoted target", `"plan.md"`, `"plan.md"`, false},
		{"target merely starts with dash", "--notes.md", "--notes.md", false},
	}
	for _, tc := range cases {
		gotPath, gotFlag := parseNoDeliver(tc.arg)
		if gotFlag != tc.wantFlag {
			t.Errorf("%s: flag = %v, want %v", tc.name, gotFlag, tc.wantFlag)
		}
		if gotPath != tc.wantPath {
			t.Errorf("%s: path = %q, want %q", tc.name, gotPath, tc.wantPath)
		}
	}
}
