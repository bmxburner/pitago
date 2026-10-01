package pirpc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleMcp = `{
  "mcpServers": {
    "vault-tools": {
      "command": "/opt/homebrew/bin/bun",
      "args": ["run", "/src/index.ts"],
      "env": { "VAULT_ROOT": "/Volumes/Tars" }
    },
    "remote": {
      "type": "http",
      "url": "https://example.test/mcp",
      "disabled": true,
      "custom": { "keep": true }
    }
  },
  "imports": ["claude-code"],
  "settings": { "directTools": ["mcp"] }
}`

func mustParse(t *testing.T, s string) *McpDoc {
	t.Helper()
	doc, err := ParseMcpJSON([]byte(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return doc
}

// parseErr returns the parse error for s (nil when it parses cleanly).
func parseErr(t *testing.T, s string) error {
	t.Helper()
	_, err := ParseMcpJSON([]byte(s))
	return err
}

func TestParseKeepsServerOrderAndFields(t *testing.T) {
	doc := mustParse(t, sampleMcp)
	got, want := doc.Servers(), []string{"vault-tools", "remote"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("servers = %v, want %v", got, want)
	}
	stdio := doc.Get("vault-tools")
	if stdio.Transport() != "stdio" {
		t.Errorf("transport = %q, want stdio", stdio.Transport())
	}
	if stdio.Command != "/opt/homebrew/bin/bun" || len(stdio.Args) != 2 ||
		stdio.Env["VAULT_ROOT"] != "/Volumes/Tars" {
		t.Errorf("stdio def = %+v", stdio)
	}
	http := doc.Get("remote")
	if http.URL != "https://example.test/mcp" || !http.Disabled {
		t.Errorf("http def = %+v", http)
	}
}

func TestToggleDisabledPreservesOrderAndSiblings(t *testing.T) {
	doc := mustParse(t, sampleMcp)
	on, err := doc.ToggleDisabled("vault-tools")
	if err != nil || !on {
		t.Fatalf("toggle = %v, %v", on, err)
	}
	out := string(doc.Bytes())
	// Top-level order untouched: mcpServers first, then imports/settings.
	iSrv, iImp, iSet := strings.Index(out, `"mcpServers"`), strings.Index(out, `"imports"`), strings.Index(out, `"settings"`)
	if !(iSrv < iImp && iImp < iSet) {
		t.Errorf("top-level order scrambled:\n%s", out)
	}
	// Server order untouched (file order, not alphabetical).
	if strings.Index(out, `"vault-tools"`) > strings.Index(out, `"remote"`) {
		t.Errorf("server order scrambled:\n%s", out)
	}
	// Unrelated keys survive verbatim: the sibling's unknown "custom"
	// blob, and the untouched server's env.
	if !strings.Contains(out, `"custom"`) || !strings.Contains(out, `"VAULT_ROOT"`) {
		t.Errorf("unrelated keys lost:\n%s", out)
	}
	// The already-disabled sibling stays disabled (two "disabled" keys).
	if strings.Count(out, `"disabled": true`) != 2 {
		t.Errorf("expected both servers disabled:\n%s", out)
	}
	var back map[string]any
	if err := json.Unmarshal(doc.Bytes(), &back); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	// Toggling back clears the key again (no leftover false).
	if _, err := doc.ToggleDisabled("vault-tools"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(doc.Bytes()), `"disabled"`) != 1 {
		t.Errorf("re-enable left a stale key:\n%s", doc.Bytes())
	}
}

func TestPutAddsServerAndKeepsFieldOrder(t *testing.T) {
	doc := mustParse(t, sampleMcp)
	doc.Put(McpDef{Name: "new-one", Command: "foo", Args: []string{"a"},
		Env: map[string]string{"B": "2", "A": "1"}})
	out := string(doc.Bytes())
	if !strings.Contains(out, `"new-one"`) {
		t.Fatalf("new server missing:\n%s", out)
	}
	// A new server is appended at the end, not sorted in.
	if strings.Index(out, `"new-one"`) < strings.Index(out, `"remote"`) {
		t.Errorf("new server not appended:\n%s", out)
	}
	// env keys sorted for a stable diff.
	if strings.Index(out, `"A": "1"`) > strings.Index(out, `"B": "2"`) {
		t.Errorf("env keys not sorted:\n%s", out)
	}
	// Existing server keeps its own field order: command, args, env.
	gi := strings.Index(out, `"vault-tools"`)
	if gi < 0 {
		t.Fatalf("vault-tools vanished:\n%s", out)
	}
	part := out[gi:]
	if len(part) > 240 {
		part = part[:240]
	}
	ic, ia, ie := strings.Index(part, `"command"`), strings.Index(part, `"args"`), strings.Index(part, `"env"`)
	if !(ic < ia && ia < ie) {
		t.Errorf("field order changed (cmd %d args %d env %d):\n%s", ic, ia, ie, part)
	}
}

func TestDeleteAndMissing(t *testing.T) {
	doc := mustParse(t, sampleMcp)
	if !doc.Delete("remote") {
		t.Fatal("delete remote = false")
	}
	if doc.Delete("nope") {
		t.Error("delete of a missing server = true")
	}
	if strings.Contains(string(doc.Bytes()), `"remote"`) {
		t.Errorf("remote still present:\n%s", doc.Bytes())
	}
	if _, err := doc.ToggleDisabled("nope"); err == nil {
		t.Error("toggle of a missing server should error")
	}
	if doc.Get("nope").Command != "" {
		t.Error("Get of a missing server should be the zero def")
	}
}

func TestEmptyAndBrokenInput(t *testing.T) {
	doc := mustParse(t, "")
	if len(doc.Servers()) != 0 {
		t.Errorf("empty doc has servers: %v", doc.Servers())
	}
	doc.Put(McpDef{Name: "x", Command: "y"})
	if len(doc.Servers()) != 1 {
		t.Errorf("put on an empty doc failed: %v", doc.Servers())
	}
	for _, bad := range []string{"not json", `["a"]`, `{"mcpServers": 3}`} {
		if _, err := ParseMcpJSON([]byte(bad)); err == nil {
			t.Errorf("ParseMcpJSON(%q) should error", bad)
		}
	}
	// A doc with mcpServers of the wrong type must error, not be
	// silently rewritten to {} (that would delete the user's value).
	if err := parseErr(t, `{"mcpServers": 3}`); err == nil {
		t.Error("bad mcpServers should error")
	}
}

func TestSaveRoundTripAndBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	if err := os.WriteFile(path, []byte(sampleMcp), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Path != path || len(doc.Servers()) != 2 {
		t.Fatalf("load = %+v", doc)
	}
	if _, err := doc.ToggleDisabled("vault-tools"); err != nil {
		t.Fatal(err)
	}
	if err := BackupFile(path, []byte(sampleMcp)); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(path); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(saved), `"disabled": true`) != 2 {
		t.Errorf("save lost the toggle:\n%s", saved)
	}
	back, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != sampleMcp {
		t.Errorf("backup is not the previous content:\n%s", back)
	}
	// A missing file loads as an empty doc, not an error.
	d2, err := LoadMcpConfig(filepath.Join(dir, "gone.json"))
	if err != nil || len(d2.Servers()) != 0 {
		t.Errorf("missing file: doc=%v err=%v", d2, err)
	}
	// A broken file errors, so the panel can say "unreadable".
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadMcpConfig(bad); err == nil {
		t.Error("broken file should error")
	}
	// Save with no path refuses instead of writing somewhere unknown.
	if err := (&McpDoc{}).Save(""); err == nil {
		t.Error("Save(\"\") should error")
	}
}

func TestNonStringEnvAndArgsSurviveAToggle(t *testing.T) {
	doc := mustParse(t, `{
  "mcpServers": {
    "odd": { "command": "c", "args": [1, 2], "env": { "PORT": 8080 } }
  }
}`)
	def := doc.Get("odd")
	if def.Args != nil || def.Env != nil {
		t.Fatalf("unparseable shapes should not fake a value: %+v", def)
	}
	if _, err := doc.ToggleDisabled("odd"); err != nil {
		t.Fatal(err)
	}
	out := string(doc.Bytes())
	if !strings.Contains(out, `"args"`) || !strings.Contains(out, `8080`) {
		t.Errorf("toggle dropped an unmodelled args/env:\n%s", out)
	}
	// A deliberate clear (explicit empty, non-nil) still removes the key.
	def = doc.Get("odd")
	def.Args = []string{}
	def.Env = map[string]string{}
	doc.Put(def)
	if out := string(doc.Bytes()); strings.Contains(out, `"args"`) || strings.Contains(out, `"env"`) {
		t.Errorf("explicit clear did not remove the keys:\n%s", out)
	}
}

func TestPutKeepsUnmodelledCollectionsWhenLeftNil(t *testing.T) {
	doc := mustParse(t, `{"mcpServers":{
		"odd":{"command":"c","args":[1,2],"env":{"N":3},"headers":{"H":7}}}}`)
	def := doc.Get("odd") // unparseable collections parse to nil
	if def.Args != nil || def.Env != nil || def.Headers != nil {
		t.Fatalf("expected nil collections: %+v", def)
	}
	def.Command = "c2"
	doc.Put(def)
	out := string(doc.Bytes())
	for _, want := range []string{`"c2"`, `"args"`, `1`, `"N": 3`, `"H": 7`} {
		if !strings.Contains(out, want) {
			t.Errorf("nil collection was written over (%s missing):\n%s", want, out)
		}
	}
	// Explicit empties clear them.
	def.Args, def.Env, def.Headers = []string{}, map[string]string{}, map[string]string{}
	doc.Put(def)
	if out := string(doc.Bytes()); strings.Contains(out, `"args"`) ||
		strings.Contains(out, `"env"`) || strings.Contains(out, `"headers"`) {
		t.Errorf("explicit clear failed:\n%s", out)
	}
}

func TestSaveRefusesWhenFileChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	orig := `{"mcpServers":{"a":{"command":"x"}}}`
	if err := os.WriteFile(path, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadMcpConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := doc.ToggleDisabled("a"); err != nil {
		t.Fatal(err)
	}
	// Someone else writes between our read and our write.
	other := `{"mcpServers":{"a":{"command":"x"}},"imports":["claude"]}`
	if err := os.WriteFile(path, []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := doc.SaveIfUnchanged(path, []byte(orig)); err == nil {
		t.Error("SaveIfUnchanged should refuse to clobber a newer file")
	}
	now, _ := os.ReadFile(path)
	if string(now) != other {
		t.Errorf("file was overwritten: %s", now)
	}
	// With the current bytes as the expectation it goes through.
	if err := doc.SaveIfUnchanged(path, []byte(other)); err != nil {
		t.Errorf("matching expectation should save: %v", err)
	}
	// nil expectation = unconditional write (Save).
	doc2 := mustParse(t, orig)
	if err := doc2.Save(path); err != nil {
		t.Errorf("Save should be unconditional: %v", err)
	}
}

func TestTrailingContentIsRejected(t *testing.T) {
	if _, err := ParseMcpJSON([]byte(`{"mcpServers":{}} {"mcpServers":{"x":{}}}`)); err == nil {
		t.Error("two objects back to back should error, not parse as the first")
	}
}

func TestMcpConfigPathsOrder(t *testing.T) {
	dir := t.TempDir()
	got := McpConfigPaths(dir)
	want := []string{filepath.Join(dir, "mcp.json"), filepath.Join(dir, ".mcp.json")}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("paths = %v, want %v", got, want)
	}
	if McpConfigPaths("") != nil {
		t.Error("no dir should give no paths")
	}
}
