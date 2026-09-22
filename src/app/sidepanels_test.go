package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTodosShapes(t *testing.T) {
	// list-in-args with content+status
	in := `{"todos":[{"id":"1","content":"Write code","status":"in_progress"},{"id":"2","content":"Test","status":"pending"}]}`
	got, ok := parseTodos(json.RawMessage(in))
	if !ok || len(got) != 2 {
		t.Fatalf("parse list-in-args = %v,%v", got, ok)
	}
	if got[0].Status != TodoInProgress || got[1].Status != TodoPending {
		t.Fatalf("statuses = %v %v", got[0].Status, got[1].Status)
	}
	// pi-todo style: text+done boolean in details
	in = `{"todos":[{"text":"Done thing","done":true},{"text":"Next"}]}`
	got, ok = parseTodos(json.RawMessage(in))
	if !ok || len(got) != 2 || got[0].Status != TodoCompleted || got[1].Status != TodoPending {
		t.Fatalf("parse done-boolean = %v,%v", got, ok)
	}
	// bare array + items key
	got, ok = parseTodos(json.RawMessage(`[{"content":"a","status":"completed"}]`))
	if !ok || len(got) != 1 || got[0].Status != TodoCompleted {
		t.Fatalf("parse bare array = %v,%v", got, ok)
	}
	// action-only payload: no list → ok=false
	if _, ok = parseTodos(json.RawMessage(`{"action":"update","text":"x"}`)); ok {
		t.Fatal("action-only payload must not parse")
	}
	// empty list is a valid empty state
	got, ok = parseTodos(json.RawMessage(`{"todos":[]}`))
	if !ok || len(got) != 0 {
		t.Fatalf("empty list = %v,%v", got, ok)
	}
}

func TestReadMcpServers(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"mcpServers":{"alpha":{"command":"x"},"beta":{"disabled":true}},"settings":{"directTools":true}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := `{"version":1,"servers":{"alpha":{"tools":[{"name":"t1","description":"d","inputSchema":{"type":"object"}}]}}}`
	if err := os.WriteFile(filepath.Join(dir, "mcp-cache.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readMcpServers(dir)
	if len(got) != 2 {
		t.Fatalf("servers = %+v", got)
	}
	// sorted by name: alpha first, connected via cache
	if got[0].Name != "alpha" || !got[0].Connected || got[0].Direct != 1 || got[0].Total != 1 {
		t.Fatalf("alpha = %+v", got[0])
	}
	if got[1].Name != "beta" || !got[1].Disabled {
		t.Fatalf("beta = %+v", got[1])
	}
	if n := readMcpServers(t.TempDir()); len(n) != 0 {
		t.Fatalf("empty dir must yield no servers, got %+v", n)
	}
}

func TestReadPlugins(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"packages":["npm:pi-lens","npm:@narumitw/pi-plan-mode","git:github.com/sting8k/pi-themes"]}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	got := readPlugins(dir)
	if len(got) != 3 {
		t.Fatalf("plugins = %+v", got)
	}
	// order follows settings.json, names strip the source prefix
	if got[0].Name != "pi-lens" || got[0].Spec != "npm:pi-lens" {
		t.Fatalf("first = %+v", got[0])
	}
	if got[1].Name != "@narumitw/pi-plan-mode" {
		t.Fatalf("scoped = %+v", got[1])
	}
	if got[2].Name != "github.com/sting8k/pi-themes" {
		t.Fatalf("git = %+v", got[2])
	}
	if n := readPlugins(t.TempDir()); len(n) != 0 {
		t.Fatalf("empty dir must yield no plugins, got %+v", n)
	}
}

func TestPluginsSectionToggle(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Side = map[string]bool{SidePlugins: true} // hidden by default
	m.Plugins = []Plugin{{Spec: "npm:pi-lens", Name: "pi-lens"}, {Spec: "npm:pi-foo", Name: "pi-foo"}}
	if !m.showPlugins {
		t.Fatal("PLUGINS must start expanded")
	}
	out := m.buildSidebarContent()
	for _, want := range []string{"PLUGINS (2)", "pi-lens", "pi-foo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expanded sidebar missing %q", want)
		}
	}
	m.TogglePlugins()
	out = m.buildSidebarContent()
	if !strings.Contains(out, "PLUGINS (2)") {
		t.Fatal("collapsed sidebar must keep the header")
	}
	for _, want := range []string{"pi-lens", "pi-foo"} {
		if strings.Contains(out, want) {
			t.Fatalf("collapsed sidebar must hide %q", want)
		}
	}
	m.TogglePlugins()
	if !strings.Contains(m.buildSidebarContent(), "pi-lens") {
		t.Fatal("second toggle must expand again")
	}
}

func TestSidebarHasMcpTodos(t *testing.T) {
	m := New(nil, t.TempDir())
	m.Side = map[string]bool{SideMCP: true} // hidden by default
	m.MCP = []McpServer{{Name: "alpha", Direct: 1, Total: 2, Tokens: 1234, Connected: true}}
	m.Todos = []TodoItem{{ID: "1", Content: "Write code", Status: TodoInProgress}}
	out := m.buildSidebarContent()
	for _, want := range []string{"MCP Servers", "alpha", "Todos (0/1)", "Write code"} {
		if !strings.Contains(out, want) {
			t.Fatalf("sidebar missing %q", want)
		}
	}
	// empty state mirrors pi-sidebar-tui
	m.MCP, m.Todos = nil, nil
	out = m.buildSidebarContent()
	for _, want := range []string{"MCP Servers", "Todos (0/0)", "(no todos)"} {
		if !strings.Contains(out, want) {
			t.Fatalf("empty sidebar missing %q", want)
		}
	}
}
