package builtin

import (
	"encoding/json"
	"strings"
	"testing"

	"pitago/src/pirpc"
)

func testStats() pirpc.Stats {
	return pirpc.Stats{
		SessionID: "sess-1", SessionFile: "/tmp/s.json",
		TotalMessages: 10, UserMsgs: 4, AsstMsgs: 4,
		ToolCalls: 5, ToolResults: 5, Cost: 0.123,
		In: 8000, Out: 2000, CacheRead: 1500, CacheWrite: 500,
		TokensTotal: 12000,
	}
}

// Full block mirrors pi: Name/File/ID, Messages, Tokens with the
// cached split, Cost with per-model breakdown.
func TestSessionTextFull(t *testing.T) {
	br := []pirpc.CostBreak{
		{Key: "anthropic/claude-opus", Cost: 0.1, Tokens: 8500},
		{Key: "Tools/summaries", Cost: 0.023, Tokens: 3500},
	}
	got := sessionText("demo", "/tmp/s.json", testStats(), br)
	for _, want := range []string{
		"Session Info", "Name: demo", "File: /tmp/s.json", "ID: sess-1",
		"Messages", "Total: 10", "User: 4", "Assistant: 4",
		"Tools: 5 calls, 5 results",
		"Tokens", "Input: 10,000",
		"Cached: 1,500 (15.0%)",
		"Uncached: 8,500 (500 written to cache)",
		"Output: 2,000", "Total: 12,000",
		"Cost", "Total: $0.123",
		"anthropic/claude-opus: $0.100 (8.5k tokens)",
		"Tools/summaries: $0.023 (3.5k tokens)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// No cache activity, no cost, no name: pi omits those lines.
func TestSessionTextMinimal(t *testing.T) {
	s := testStats()
	s.In, s.CacheRead, s.CacheWrite, s.Cost = 100, 0, 0, 0
	got := sessionText("", "In-memory", s, nil)
	for _, want := range []string{"File: In-memory", "Input: 100"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, nope := range []string{"Name:", "Cached:", "Uncached:", "Cost", "written to cache"} {
		if strings.Contains(got, nope) {
			t.Errorf("must omit %q in:\n%s", nope, got)
		}
	}
}

// Breakdown groups like pi: assistant by responseModel, tool summaries
// apart, zero rows dropped, cost desc.
func TestUsageBreakdown(t *testing.T) {
	raw := `[
		{"type":"message","message":{"role":"assistant","provider":"anthropic","model":"auto","responseModel":"claude-opus","usage":{"input":8000,"output":100,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.1}}}},
		{"type":"usage","provider":"openai","model":"gpt","usage":{"input":10,"output":5,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.05}}},
		{"type":"message","message":{"role":"toolResult","usage":{"input":100,"output":0,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.01}}}},
		{"type":"branch_summary","usage":{"input":50,"output":0,"cacheRead":0,"cacheWrite":0,"cost":{"total":0.013}}},
		{"type":"usage","provider":"x","model":"y","usage":{"input":0,"output":0,"cacheRead":0,"cacheWrite":0,"cost":{"total":0}}}
	]`
	var entries []pirpc.SessionEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		t.Fatal(err)
	}
	br := pirpc.UsageBreakdown(entries)
	if len(br) != 3 {
		t.Fatalf("want 3 rows, got %+v", br)
	}
	if br[0].Key != "anthropic/claude-opus" || br[1].Key != "openai/gpt" || br[2].Key != "Tools/summaries" {
		t.Fatalf("bad order/keys: %+v", br)
	}
	if br[2].Tokens != 150 {
		t.Fatalf("summaries tokens = %d, want 150", br[2].Tokens)
	}
}
