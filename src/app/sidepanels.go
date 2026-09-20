package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"pitago/src/pirpc"
)

// Sidebar panels mirroring pi-sidebar-tui (MCP Servers + Todos), fed over
// pi's RPC surface: todos are tracked from todo-tool calls (any tool with
// "todo" in the name), MCP servers are read from the pi agent dir files.

type TodoStatus string

const (
	TodoPending    TodoStatus = "pending"
	TodoInProgress TodoStatus = "in_progress"
	TodoCompleted  TodoStatus = "completed"
)

type TodoItem struct {
	ID       string
	Content  string
	Status   TodoStatus
	SubAct   string // optional sub-action shown after in-progress items
}

type McpServer struct {
	Name      string
	Direct    int
	Total     int
	Tokens    int
	Connected bool
	Disabled  bool
}

const todosShowMax = 10 // cap like pi-sidebar-tui's default todosMax

func isTodoTool(name string) bool {
	return strings.Contains(strings.ToLower(name), "todo")
}

// parseTodos extracts a todo list from a todo tool's payload. Handles the
// two shapes pi-sidebar-tui handles: the full list in the tool input
// (bare array, or object with todos/items/list key) and the list in the
// tool result details (e.g. {todos:[{text,done}]}). Returns ok=false when
// no todo array is present (action-only payloads), so callers can tell
// "no list" apart from an empty list.
func parseTodos(raw json.RawMessage) ([]TodoItem, bool) {
	t := strings.TrimSpace(string(raw))
	if t == "" || t == "null" {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, false
	}
	var arr []any
	switch x := v.(type) {
	case []any:
		arr = x
	case map[string]any:
		for _, k := range []string{"todos", "items", "list"} {
			if a, ok := x[k].([]any); ok {
				arr = a
				break
			}
		}
		if arr == nil {
			return nil, false
		}
	default:
		return nil, false
	}
	out := make([]TodoItem, 0, len(arr))
	for i, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		var content string
		if s, ok := m["content"].(string); ok && s != "" {
			content = s
		} else if s, ok := m["text"].(string); ok && s != "" {
			content = s
		} else {
			continue
		}
		id := fmt.Sprint(i)
		if s, ok := m["id"].(string); ok && s != "" {
			id = s
		} else if f, ok := m["id"].(float64); ok {
			id = strings.TrimSuffix(fmt.Sprintf("%v", f), ".0")
		}
		sub, _ := m["subAction"].(string)
		out = append(out, TodoItem{ID: id, Content: content, Status: normTodoStatus(m), SubAct: sub})
	}
	return out, true
}

func normTodoStatus(m map[string]any) TodoStatus {
	if s, ok := m["status"].(string); ok {
		switch s {
		case "in_progress", "active":
			return TodoInProgress
		case "completed", "done":
			return TodoCompleted
		default:
			return TodoPending
		}
	}
	for _, k := range []string{"done", "completed"} {
		if b, ok := m[k].(bool); ok && b {
			return TodoCompleted
		}
	}
	for _, k := range []string{"in_progress", "active"} {
		if b, ok := m[k].(bool); ok && b {
			return TodoInProgress
		}
	}
	return TodoPending
}

// restoreTodos rebuilds the todo list from get_messages history: the state
// is the LAST todo-tool payload in the branch (mirrors pi-sidebar-tui's
// reconstructTodosFromBranch; RPC has no details field, so tool inputs
// and tool-result text that parses as a list both count).
func restoreTodos(msgs []pirpc.AgentMessage) []TodoItem {
	var last []TodoItem
	found := false
	for _, msg := range msgs {
		switch msg.Role {
		case "assistant":
			for _, b := range pirpc.BlocksOf(msg.Content) {
				if b.Type == "toolCall" && isTodoTool(b.Name) && len(b.Arguments) > 0 {
					if t, ok := parseTodos(b.Arguments); ok {
						last, found = t, true
					}
				}
			}
		case "toolResult":
			if !isTodoTool(msg.ToolName) {
				continue
			}
			if t, ok := parseTodos(msg.Content); ok {
				last, found = t, true
				continue
			}
			for _, b := range pirpc.BlocksOf(msg.Content) {
				if b.Type == "text" {
					if t, ok := parseTodos(json.RawMessage(strings.TrimSpace(b.Text))); ok {
						last, found = t, true
					}
				}
			}
		}
	}
	if !found {
		return nil
	}
	return last
}

// updateTodosFromRaw tries each candidate payload in order and keeps the
// first one that contains a todo list.
func (m *Model) updateTodosFromRaw(cands ...json.RawMessage) {
	for _, c := range cands {
		if len(c) == 0 {
			continue
		}
		if t, ok := parseTodos(c); ok {
			m.Todos = t
			return
		}
	}
}

// rawField pulls one top-level field out of an event envelope
// (e.g. details/result/input that typed structs drop).
func rawField(raw json.RawMessage, key string) json.RawMessage {
	var env map[string]json.RawMessage
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	return env[key]
}

// MCP servers ---------------------------------------------------------------

func piAgentDir() string {
	for _, k := range []string{"PI_CODING_AGENT_DIR", "PI_AGENT_DIR"} {
		if d := os.Getenv(k); d != "" {
			if strings.HasPrefix(d, "~/") {
				if h, err := os.UserHomeDir(); err == nil {
					return filepath.Join(h, d[2:])
				}
			} else {
				return d
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".pi", "agent")
	}
	return ""
}

func readJSONFile(path string) map[string]any {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func estimateMcpTokens(name, desc string, schemaLen int) int {
	return (len(name) + len(desc) + schemaLen) / 4 + 10
}

// readMcpServers lists configured MCP servers with tool counts, same source
// files as pi-sidebar-tui: <agentDir>/{mcp.json,.mcp.json} for config +
// mcp-cache.json for the live tool snapshot.
func readMcpServers(dir string) []McpServer {
	if dir == "" {
		return nil
	}
	configured := map[string]any{}
	for _, f := range []string{"mcp.json", ".mcp.json"} {
		if cfg := readJSONFile(filepath.Join(dir, f)); cfg != nil {
			if ms, ok := cfg["mcpServers"].(map[string]any); ok {
				for k, v := range ms {
					if _, seen := configured[k]; !seen {
						configured[k] = v
					}
				}
			}
		}
	}
	// global directTools filter (same precedence as pi-sidebar-tui)
	var globalDirect any
	for _, f := range []string{"mcp.json", ".mcp.json"} {
		if cfg := readJSONFile(filepath.Join(dir, f)); cfg != nil {
			if s, ok := cfg["settings"].(map[string]any); ok && s["directTools"] != nil {
				globalDirect = s["directTools"]
				break
			}
		}
	}
	cached := map[string]any{}
	if c := readJSONFile(filepath.Join(dir, "mcp-cache.json")); c != nil {
		if s, ok := c["servers"].(map[string]any); ok {
			cached = s
		}
	}
	var names []string
	if len(configured) > 0 {
		for n := range configured {
			names = append(names, n)
		}
	} else {
		for n := range cached {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	// stable order (TS uses config insertion order; maps need sorting)
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	out := make([]McpServer, 0, len(names))
	for _, n := range names {
		def, _ := configured[n].(map[string]any)
		if def == nil {
			def = map[string]any{}
		}
		if dis, ok := def["disabled"].(bool); ok && dis {
			out = append(out, McpServer{Name: n, Disabled: true})
			continue
		}
		var tools []any
		if srv, ok := cached[n].(map[string]any); ok {
			tools, _ = srv["tools"].([]any)
		}
		var filter any
		if def["directTools"] != nil {
			filter = def["directTools"]
		} else if globalDirect != nil {
			filter = globalDirect
		}
		excl := map[string]bool{}
		if e, ok := def["excludeTools"].([]any); ok {
			for _, x := range e {
				if s, ok := x.(string); ok {
					excl[s] = true
				}
			}
		}
		srv := McpServer{Name: n, Connected: len(tools) > 0}
		for _, t := range tools {
			tm, ok := t.(map[string]any)
			if !ok {
				continue
			}
			tn, _ := tm["name"].(string)
			if excl[tn] {
				continue
			}
			srv.Total++
			direct := false
			switch f := filter.(type) {
			case bool:
				direct = f
			case []any:
				for _, x := range f {
					if s, ok := x.(string); ok && s == tn {
						direct = true
						break
					}
				}
			}
			if direct {
				srv.Direct++
				desc, _ := tm["description"].(string)
				schLen := 2
				if sch, ok := tm["inputSchema"]; ok {
					if b, err := json.Marshal(sch); err == nil {
						schLen = len(b)
					}
				}
				srv.Tokens += estimateMcpTokens(tn, desc, schLen)
			}
		}
		out = append(out, srv)
	}
	return out
}

var (
	mcpCacheData []McpServer
	mcpCacheAt   time.Time
)

const mcpCacheTTL = 1500 * time.Millisecond

// getMcpServers returns the cached server list (1.5s TTL, like pi-sidebar-tui).
func getMcpServers() []McpServer {
	if time.Since(mcpCacheAt) < mcpCacheTTL {
		return mcpCacheData
	}
	mcpCacheData = readMcpServers(piAgentDir())
	mcpCacheAt = time.Now()
	return mcpCacheData
}

// rendering (same monochrome sidebar language as renderSidebar) -------------

func (m Model) renderMcpSection(inner int) string {
	var b strings.Builder
	b.WriteString(sideTitleStyle.Render("MCP Servers") + "\n")
	for _, s := range m.MCP {
		var dot string
		switch {
		case s.Disabled:
			dot = statusBarStyle.Render("⊘")
		case s.Connected:
			if s.Total > 0 && s.Direct == s.Total {
				dot = okStyle.Render("●")
			} else if s.Connected {
				dot = lipgloss.NewStyle().Foreground(cText).Render("◐")
			} else {
				dot = statusBarStyle.Render("○")
			}
		default:
			dot = statusBarStyle.Render("○")
		}
		count := ""
		if s.Total > 0 {
			count = fmt.Sprintf("%d/%d", s.Direct, s.Total)
		}
		tok := ""
		if s.Direct > 0 {
			tok = "  ~" + fmtComma(s.Tokens)
		}
		suffix := statusBarStyle.Render("  " + count + tok)
		suffixLen := 2 + len(count)
		if s.Direct > 0 {
			suffixLen += 2 + len(fmtComma(s.Tokens)) + 1
		}
		nameMax := inner - 3 - suffixLen
		if nameMax < 0 {
			nameMax = 0
		}
		name := Short(s.Name, nameMax)
		b.WriteString(statusBarStyle.Render(" ") + dot + " " +
			lipgloss.NewStyle().Foreground(cText).Render(name) + suffix + "\n")
	}
	b.WriteString(sep() + "\n")
	return b.String()
}

func selectTodos(todos []TodoItem, max int) []TodoItem {
	if max <= 0 || len(todos) <= max {
		return todos
	}
	var chosen []TodoItem
	for _, t := range todos {
		if t.Status == TodoInProgress {
			chosen = append(chosen, t)
			if len(chosen) >= max {
				return chosen
			}
		}
	}
	// most recent (highest list position) fill the rest
	need := max - len(chosen)
	if need > 0 {
		var rest []TodoItem
		for _, t := range todos {
			if t.Status != TodoInProgress {
				rest = append(rest, t)
			}
		}
		if len(rest) > need {
			rest = rest[len(rest)-need:]
		}
		chosen = append(chosen, rest...)
	}
	return chosen
}

func (m Model) renderTodosSection(inner int) string {
	var b strings.Builder
	done := 0
	for _, t := range m.Todos {
		if t.Status == TodoCompleted {
			done++
		}
	}
	b.WriteString(sideTitleStyle.Render(fmt.Sprintf("Todos (%d/%d)", done, len(m.Todos))) + "\n")
	if len(m.Todos) == 0 {
		b.WriteString(toolStyle.Render(" (no todos)") + "\n")
	} else {
		shown := selectTodos(m.Todos, todosShowMax)
		for _, t := range shown {
			var glyph string
			switch t.Status {
			case TodoCompleted:
				glyph = okStyle.Render("✓")
			case TodoInProgress:
				glyph = lipgloss.NewStyle().Foreground(cText).Render("◐")
			default:
				glyph = statusBarStyle.Render("○")
			}
			line := t.Content
			if t.Status == TodoInProgress && t.SubAct != "" {
				full := t.Content + " (" + t.SubAct + ")"
				if lipgloss.Width(full) <= inner-3 {
					b.WriteString(statusBarStyle.Render(" ")+glyph+" "+
						lipgloss.NewStyle().Foreground(cText).Render(t.Content)+
						toolStyle.Render(" ("+Short(t.SubAct, inner)+")")+"\n")
					continue
				}
				line = Short(t.Content, inner-7)
			} else {
				line = Short(line, inner-3)
			}
			b.WriteString(statusBarStyle.Render(" ") + glyph + " " +
				lipgloss.NewStyle().Foreground(cText).Render(line) + "\n")
		}
		if hidden := len(m.Todos) - len(shown); hidden > 0 {
			b.WriteString(toolStyle.Render(fmt.Sprintf(" … +%d more", hidden)) + "\n")
		}
	}
	b.WriteString(sep() + "\n")
	return b.String()
}
