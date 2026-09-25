package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pitago/src/pirpc"
)

func TestIsSubagentRowTool(t *testing.T) {
	for _, name := range []string{"subagent", "run_agent", "run_workflow", "SubAgent", " RUN_AGENT "} {
		if !isSubagentRowTool(name) {
			t.Errorf("%q should create rows", name)
		}
	}
	for _, name := range []string{"subagent_wait", "subagent_interrupt", "subagents_list", "subagent_resume", "read", "fork", "forklift", "", "my-subagent-tool"} {
		if isSubagentRowTool(name) {
			t.Errorf("%q should not create rows", name)
		}
	}
}

func TestIsSubagentTool(t *testing.T) {
	for _, name := range []string{"subagent", "subagent_interrupt", "subagent_wait", "subagents_list", "subagent_resume", "run_agent", "run_workflow"} {
		if !isSubagentTool(name) {
			t.Errorf("%q should be a subagent-family tool", name)
		}
	}
	if isSubagentTool("read") || isSubagentTool("fork") {
		t.Error("unrelated tools must not match")
	}
}

func TestParseSubagentArgs(t *testing.T) {
	raw := json.RawMessage(`{"name":"scout","agent":"explore","task":"map the auth flow","interactive":true,"mode":"worktree"}`)
	name, agent, task, interactive, mode := parseSubagentArgs(raw)
	if name != "scout" || agent != "explore" || task != "map the auth flow" || !interactive || mode != "worktree" {
		t.Fatalf("parsed = %q %q %q %v %q", name, agent, task, interactive, mode)
	}
	name, _, _, interactive, _ = parseSubagentArgs(json.RawMessage(`{}`))
	if name != "" || interactive {
		t.Fatalf("empty object should parse empty, got %q %v", name, interactive)
	}
	name, _, _, _, _ = parseSubagentArgs(json.RawMessage(`not json`))
	if name != "" {
		t.Fatalf("invalid json should parse empty, got %q", name)
	}
	name, _, _, _, _ = parseSubagentArgs(nil)
	if name != "" {
		t.Fatal("nil should parse empty")
	}
}

func TestFormatSubagentElapsed(t *testing.T) {
	cases := map[int64]string{0: "00:00", 59000: "00:59", 60000: "01:00", 125000: "02:05", -5: "00:00"}
	for ms, want := range cases {
		if got := formatSubagentElapsed(ms); got != want {
			t.Errorf("formatSubagentElapsed(%d) = %q, want %q", ms, got, want)
		}
	}
}

func TestSubagentStatusFromActivity(t *testing.T) {
	now := int64(1_000_000_000)
	start := now - 5_000
	if s, _ := subagentStatusFromActivity(subagentActivity{}, false, start, now); s != SubagentStarting {
		t.Errorf("missing activity should be starting, got %q", s)
	}
	if s, l := subagentStatusFromActivity(subagentActivity{}, false, now-120_000, now); s != SubagentStalled || l != "stalled" {
		t.Errorf("old missing activity should stall, got %q/%q", s, l)
	}
	if s, _ := subagentStatusFromActivity(subagentActivity{Phase: "done"}, true, start, now); s != SubagentDone {
		t.Errorf("done phase should be done, got %q", s)
	}
	if s, l := subagentStatusFromActivity(subagentActivity{Phase: "waiting"}, true, start, now); s != SubagentWaiting || l != "waiting" {
		t.Errorf("waiting phase mismatch, got %q/%q", s, l)
	}
	if s, l := subagentStatusFromActivity(subagentActivity{Phase: "active", ActiveScope: "tool", ToolName: "read", UpdatedAt: now - 1_000}, true, start, now); s != SubagentActive || l != "read" {
		t.Errorf("active tool scope mismatch, got %q/%q", s, l)
	}
	if s, l := subagentStatusFromActivity(subagentActivity{Phase: "active", ActiveScope: "turn", UpdatedAt: now - 1_000}, true, start, now); s != SubagentActive || l != "thinking" {
		t.Errorf("active turn scope mismatch, got %q/%q", s, l)
	}
	if s, l := subagentStatusFromActivity(subagentActivity{Phase: "active", ActiveScope: "tool", UpdatedAt: now - 400_000}, true, start, now); s != SubagentStalled || l != "stale" {
		t.Errorf("stale active should stall, got %q/%q", s, l)
	}
	if s, _ := subagentStatusFromActivity(subagentActivity{Phase: "bogus"}, true, start, now); s != SubagentStarting {
		t.Errorf("unknown phase should be starting, got %q", s)
	}
}

func TestReadSubagentActivity(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.json")
	content := `{"version":1,"phase":"active","activeScope":"tool","toolName":"bash","updatedAt":12345}`
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	a, ok := readSubagentActivity(p)
	if !ok || a.Phase != "active" || a.ToolName != "bash" || a.UpdatedAt != 12345 {
		t.Fatalf("activity = %+v %v", a, ok)
	}
	if _, ok := readSubagentActivity(filepath.Join(dir, "missing.json")); ok {
		t.Error("missing file should not parse")
	}
	if err := os.WriteFile(p, []byte(`{broken`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readSubagentActivity(p); ok {
		t.Error("invalid json should not parse")
	}
	if err := os.WriteFile(p, []byte(`{"phase":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readSubagentActivity(p); ok {
		t.Error("empty phase should not parse")
	}
}

func TestHasSubagentShutdownMarker(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(p, []byte("{\"type\":\"message\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if hasSubagentShutdownMarker(p) {
		t.Error("no marker should be false")
	}
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	_, _ = f.WriteString("{\"type\":\"custom\",\"customType\":\"session_shutdown\"}\n")
	_ = f.Close()
	if !hasSubagentShutdownMarker(p) {
		t.Error("marker should be true")
	}
	if hasSubagentShutdownMarker(filepath.Join(dir, "missing.jsonl")) {
		t.Error("missing file should be false")
	}
}

func TestSubagentArtifactDir(t *testing.T) {
	got := subagentArtifactDir("/tmp/sess/2026-01-01_abc123.jsonl")
	want := filepath.Join("/tmp/sess", "artifacts", "abc123")
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
	if subagentArtifactDir("/tmp/sess/noid.jsonl") != "" {
		t.Error("basename without id should yield empty dir")
	}
	if subagentArtifactDir("") != "" {
		t.Error("empty session file should yield empty dir")
	}
}

func toolCallMsg(id, name, args string) pirpc.AgentMessage {
	return pirpc.AgentMessage{
		Role:    "assistant",
		Content: json.RawMessage(`[{"type":"toolCall","id":"` + id + `","name":"` + name + `","arguments":` + args + `}]`),
	}
}

func TestRestoreSubagentsFromMessages(t *testing.T) {
	msgs := []pirpc.AgentMessage{
		toolCallMsg("c1", "subagent", `{"name":"scout","agent":"explore","task":"map auth"}`),
		{Role: "toolResult", ToolCallID: "c1", ToolName: "subagent", Content: json.RawMessage(`[{"type":"text","text":"done: mapped"}]`)},
		toolCallMsg("c2", "read", `{"path":"x"}`),
		toolCallMsg("c3", "subagent", `{"task":"still running"}`),
	}
	rows := restoreSubagentsFromMessages(msgs)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0].Name != "scout" || rows[0].Status != SubagentDone || !strings.Contains(rows[0].Result, "mapped") {
		t.Errorf("row0 = %+v", rows[0])
	}
	if rows[1].Status != SubagentActive || !strings.HasPrefix(rows[1].Name, "subagent-") {
		t.Errorf("row1 = %+v", rows[1])
	}
	if len(restoreSubagentsFromMessages(nil)) != 0 {
		t.Error("nil messages should yield no rows")
	}
	// error result → error status
	msgs = []pirpc.AgentMessage{
		toolCallMsg("e1", "run_agent", `{"name":"w1"}`),
		{Role: "toolResult", ToolCallID: "e1", ToolName: "run_agent", IsError: true, Content: json.RawMessage(`[{"type":"text","text":"boom"}]`)},
	}
	rows = restoreSubagentsFromMessages(msgs)
	if len(rows) != 1 || rows[0].Status != SubagentError || !rows[0].IsError {
		t.Fatalf("error row = %+v", rows)
	}
}

func TestScanSubagentArtifacts(t *testing.T) {
	now := time.Now()
	agentDir := t.TempDir()
	sessDir := t.TempDir()
	sessFile := filepath.Join(sessDir, "2026-01-01_parent1.jsonl")
	if err := os.WriteFile(sessFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artDir := filepath.Join(sessDir, "artifacts", "parent1")
	actDir := filepath.Join(artDir, "subagent-activity")
	if err := os.MkdirAll(actDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// live pane spawn with fresh activity
	if err := os.WriteFile(filepath.Join(artDir, "subagent-ab12.jsonl"), []byte("{\"type\":\"session\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	actContent := map[string]any{"version": 1, "phase": "active", "activeScope": "tool", "toolName": "bash", "updatedAt": now.UnixMilli()}
	raw, _ := json.Marshal(actContent)
	if err := os.WriteFile(filepath.Join(actDir, "ab12.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	// finished spawn with shutdown marker, no activity → skipped (history covers it)
	if err := os.WriteFile(filepath.Join(artDir, "subagent-cd34.jsonl"), []byte("{\"type\":\"custom\",\"customType\":\"session_shutdown\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// stale run from another session (old mtime, no marker) → skipped
	stale := filepath.Join(artDir, "subagent-ef56.jsonl")
	if err := os.WriteFile(stale, []byte("{\"type\":\"session\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-72 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	// in-process session file
	if err := os.MkdirAll(filepath.Join(agentDir, "subagent-sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "subagent-sessions", "ip-1.jsonl"), []byte("{\"type\":\"session\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := scanSubagentArtifacts(sessFile, agentDir, now)
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %+v", len(rows), rows)
	}
	byID := map[string]SubagentRow{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	live, ok := byID["disk-ab12"]
	if !ok || live.Status != SubagentActive || live.StatusLabel != "bash" {
		t.Errorf("live row = %+v %v", live, ok)
	}
	if _, ok := byID["disk-cd34"]; ok {
		t.Error("shutdown-marked file should be skipped")
	}
	if _, ok := byID["disk-ef56"]; ok {
		t.Error("stale file should be skipped")
	}
	// empty session file → no rows
	if rows := scanSubagentArtifacts("", "", now); len(rows) != 0 {
		t.Errorf("empty inputs should yield no rows, got %d", len(rows))
	}
}

func TestMergeSubagentDisk(t *testing.T) {
	live := []SubagentRow{{ID: "c1", Name: "scout", SessionFile: "/a/subagent-x.jsonl"}}
	disk := []SubagentRow{
		{ID: "disk-x", Name: "subagent-x", SessionFile: "/a/subagent-x.jsonl"},
		{ID: "disk-y", Name: "subagent-y", SessionFile: "/a/subagent-y.jsonl"},
	}
	merged := mergeSubagentDisk(live, disk)
	if len(merged) != 2 || merged[1].ID != "disk-y" {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestSideSubagentsAutoVisibility(t *testing.T) {
	var m Model
	if m.SideVisible(SideSubagents) {
		t.Error("empty should hide")
	}
	m.Subagents = []SubagentRow{{ID: "a", Name: "scout", Status: SubagentActive}}
	if !m.SideVisible(SideSubagents) {
		t.Error("rows should reveal")
	}
	m.Side = map[string]bool{SideSubagents: false}
	if m.SideVisible(SideSubagents) {
		t.Error("explicit toggle wins over auto")
	}
}

func TestRenderSubagentsSection(t *testing.T) {
	now := time.Now()
	done := now.Add(-time.Minute)
	m := Model{Subagents: []SubagentRow{
		{ID: "a", Name: "scout", Status: SubagentActive, StatusLabel: "read", StartedAt: now.Add(-90 * time.Second)},
		{ID: "b", Name: "builder", Status: SubagentDone, StartedAt: now.Add(-5 * time.Minute), DoneAt: &done},
	}}
	out := m.renderSubagentsSection(40)
	for _, want := range []string{"Subagents", "scout", "builder", "read", "01:30"} {
		if !strings.Contains(out, want) {
			t.Errorf("section should contain %q, got:\n%s", want, out)
		}
	}
}

func TestUpsertSubagentRow(t *testing.T) {
	rows := upsertSubagentRow(nil, SubagentRow{ID: "a"})
	rows = upsertSubagentRow(rows, SubagentRow{ID: "b"})
	rows = upsertSubagentRow(rows, SubagentRow{ID: "a", Name: "n"})
	if len(rows) != 2 || rows[1].ID != "a" || rows[1].Name != "n" {
		t.Fatalf("rows = %+v", rows)
	}
}
