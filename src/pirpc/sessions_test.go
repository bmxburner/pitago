package pirpc

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func writeSession(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	proj := t.TempDir() // stored cwd must exist (pi can't resume missing dirs)
	sess := func(id, extra string) string {
		return `{"type":"session","id":"` + id + `","timestamp":"2026-09-20T08:00:00.000Z","cwd":` + strconv.Quote(proj) + `}` + "\n" + extra
	}
	writeSession(t, dir, "2026-09-20T08-00-00-000Z_aaa.jsonl",
		sess("aaa",
			`{"type":"message","id":"m1","timestamp":"2026-09-20T08:01:00.000Z","message":{"role":"user","content":"fix the login bug"}}`+"\n"+
				`{"type":"message","id":"m2","timestamp":"2026-09-20T08:02:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"done"}]}}`+"\n"))
	writeSession(t, dir, "2026-09-21T08-00-00-000Z_bbb.jsonl",
		sess("bbb",
			`{"type":"session_info","id":"n1","timestamp":"2026-09-21T08:05:00.000Z","name":"Refactor auth"}`+"\n"+
				`{"type":"message","id":"m1","timestamp":"2026-09-21T08:01:00.000Z","message":{"role":"user","content":"hello"}}`+"\n"))
	writeSession(t, dir, "junk.jsonl", "not json\n")

	got := ListSessions(dir, proj, 20, true)
	if len(got) != 2 {
		t.Fatalf("got %d sessions, want 2", len(got))
	}
	if got[0].ID != "bbb" { // newest activity first
		t.Fatalf("first = %s, want bbb", got[0].ID)
	}
	if got[0].Title() != "Refactor auth" {
		t.Fatalf("title = %q", got[0].Title())
	}
	if got[1].Title() != "fix the login bug" {
		t.Fatalf("title = %q", got[1].Title())
	}
	if got[1].MessageCount != 2 {
		t.Fatalf("count = %d, want 2", got[1].MessageCount)
	}

	// other cwd filtered out when sameCwdOnly
	if got := ListSessions(dir, "/elsewhere", 20, true); len(got) != 0 {
		t.Fatalf("got %d, want 0 (cwd filter)", len(got))
	}
	if got := ListSessions(dir, "/elsewhere", 20, false); len(got) != 2 {
		t.Fatalf("got %d, want 2 (no filter)", len(got))
	}
	if got := ListSessions("/nonexistent", "/work", 20, true); len(got) != 0 {
		t.Fatal("missing dir should give empty")
	}
}

// Sessions whose stored cwd is gone are unrestorable (pi exits instead),
// so the picker must skip them; empty-cwd entries stay (pi falls back to
// the process cwd).
func TestListSkipsMissingCwd(t *testing.T) {
	dir := t.TempDir()
	writeSession(t, dir, "2026-09-20T08-00-00-000Z_gone.jsonl",
		`{"type":"session","id":"gone","timestamp":"2026-09-20T08:00:00.000Z","cwd":"/definitely/not/here"}`+"\n"+
			`{"type":"message","id":"m1","timestamp":"2026-09-20T08:01:00.000Z","message":{"role":"user","content":"ghost"}}`+"\n")
	writeSession(t, dir, "2026-09-20T09-00-00-000Z_nocwd.jsonl",
		`{"type":"session","id":"nocwd","timestamp":"2026-09-20T09:00:00.000Z"}`+"\n"+
			`{"type":"message","id":"m1","timestamp":"2026-09-20T09:01:00.000Z","message":{"role":"user","content":"keeper"}}`+"\n")

	got := ListSessions(dir, "/work", 20, false)
	if len(got) != 1 || got[0].ID != "nocwd" {
		t.Fatalf("got %+v, want only nocwd", got)
	}
	if got := ListAllSessions(dir, 20); len(got) != 1 || got[0].ID != "nocwd" {
		t.Fatalf("all: got %+v, want only nocwd", got)
	}
}

func TestSessionDirFor(t *testing.T) {
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "/tmp/sess")
	if got := SessionDirFor("/work/proj"); got != "/tmp/sess" {
		t.Fatalf("env override = %q", got)
	}
	t.Setenv("PI_CODING_AGENT_SESSION_DIR", "")
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	got := SessionDirFor("/work/proj")
	want := filepath.Join(os.Getenv("PI_CODING_AGENT_DIR"), "sessions", "--work-proj--")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestListAllSessions(t *testing.T) {
	root := t.TempDir()
	projA := filepath.Join(root, "--work-projA--")
	projB := filepath.Join(root, "--work-projB--")
	if err := os.MkdirAll(projA, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(projB, 0o700); err != nil {
		t.Fatal(err)
	}
	writeSession(t, projA, "2026-09-20T08-00-00-000Z_aaa.jsonl",
		`{"type":"session","id":"aaa","timestamp":"2026-09-20T08:00:00.000Z","cwd":`+strconv.Quote(t.TempDir())+`}`+"\n"+
			`{"type":"message","id":"m1","timestamp":"2026-09-20T08:01:00.000Z","message":{"role":"user","content":"task A"}}`+"\n")
	writeSession(t, projB, "2026-09-21T08-00-00-000Z_bbb.jsonl",
		`{"type":"session","id":"bbb","timestamp":"2026-09-21T08:00:00.000Z","cwd":`+strconv.Quote(t.TempDir())+`}`+"\n"+
			`{"type":"message","id":"m1","timestamp":"2026-09-21T08:01:00.000Z","message":{"role":"user","content":"task B"}}`+"\n")

	got := ListAllSessions(root, 50)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0].ID != "bbb" || got[1].ID != "aaa" {
		t.Fatalf("order = %s,%s, want bbb,aaa", got[0].ID, got[1].ID)
	}
	if got := ListAllSessions(filepath.Join(root, "missing"), 50); len(got) != 0 {
		t.Fatal("missing root should give empty")
	}
}

func TestShorten(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	if got := Shorten(filepath.Join(home, "a", "b")); got != "~/a/b" {
		t.Fatalf("got %q", got)
	}
	if got := Shorten("/tmp/x"); got != "/tmp/x" {
		t.Fatalf("got %q", got)
	}
}
