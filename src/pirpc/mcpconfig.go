package pirpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Order-preserving reader/writer for ~/.pi/agent/mcp.json.
//
// mcp.json is a file pi reads at startup and humans edit by hand. Two
// constraints shape everything here:
//
//   - Key order is information the user sees in git diffs. A plain
//     map[string]any round-trip would alphabetise mcpServers and shuffle
//     every unrelated top-level key (imports, settings) on each save, so
//     the panel would produce noisy diffs for edits it did not make.
//     jsonObj keeps the original order and appends new keys at the end.
//
//   - The file holds secrets (env tokens). It is written through the
//     same atomic temp+rename path as auth.json, never truncated in
//     place, and unknown top-level sections are copied through as raw
//     bytes so a pi-side schema addition survives a pitago edit.

// jsonObj is a JSON object that remembers its key order.
type jsonObj struct {
	keys []string
	vals map[string]json.RawMessage
}

func parseJSONObj(data []byte) (*jsonObj, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	o := &jsonObj{vals: map[string]json.RawMessage{}}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := kt.(string)
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o.Set(key, raw)
	}
	if _, err := dec.Token(); err != nil && err != io.EOF {
		return nil, err
	}
	// Trailing content means the file is not what it claims to be (a
	// half-merged edit, two objects back to back). Parsing it "successfully"
	// and then saving would delete the second half, so refuse instead.
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing content after the JSON object")
		}
		return nil, err
	}
	return o, nil
}

// Set stores key, appending it if new (order preserved).
func (o *jsonObj) Set(key string, raw json.RawMessage) {
	if _, seen := o.vals[key]; !seen {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = raw
}

// Delete removes key if present.
func (o *jsonObj) Delete(key string) {
	if _, seen := o.vals[key]; !seen {
		return
	}
	delete(o.vals, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
}

// Get returns the raw value for key (nil when absent).
func (o *jsonObj) Get(key string) json.RawMessage { return o.vals[key] }

// Has reports whether key is present.
func (o *jsonObj) Has(key string) bool {
	_, ok := o.vals[key]
	return ok
}

// Obj returns key parsed as a nested object, or nil when absent/invalid.
func (o *jsonObj) Obj(key string) *jsonObj {
	raw, ok := o.vals[key]
	if !ok {
		return nil
	}
	nested, err := parseJSONObj(raw)
	if err != nil {
		return nil
	}
	return nested
}

func indentRaw(raw json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return raw
	}
	return buf.Bytes()
}

// Marshal renders the object as 2-space-indented JSON, preserving order.
func (o *jsonObj) Marshal() []byte {
	var b bytes.Buffer
	b.WriteString("{\n")
	for i, k := range o.keys {
		b.WriteString("  " + mustJSONString(k) + ": ")
		b.Write(indentRaw(o.vals[k]))
		if i < len(o.keys)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.Bytes()
}

func mustJSONString(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(raw)
}

// McpDef is one editable server definition from mcp.json's mcpServers.
// Put merges into the entry as parsed: keys pitago does not model (a
// future pi field, a per-server "custom" blob) survive an edit of another
// field, and so do args/env/headers the caller left nil — see Put.
type McpDef struct {
	Name     string
	Type     string // "" means stdio (pi's default)
	Command  string // stdio
	Args     []string
	Env      map[string]string // stdio
	URL      string            // http / sse
	Headers  map[string]string // http / sse
	Disabled bool
}

// Transport returns the effective transport label.
func (d McpDef) Transport() string {
	if d.Type == "" {
		return "stdio"
	}
	return d.Type
}

// McpDoc is a parsed mcp.json.
type McpDoc struct {
	root *jsonObj
	// Path is where the doc came from ("" for a doc parsed from bytes).
	Path string
	// expect is the exact file content this doc was parsed from. It is
	// the compare-and-swap guard for the eventual save: a write built
	// from this parse must not land on a file that changed in between.
	// Empty-but-non-nil means "the file did not exist / was empty", which
	// the CAS treats as "still must not exist with content".
	expect []byte
}

// Read returns the exact bytes the doc was parsed from, for use as the
// expectation of SaveIfUnchanged (Exported: the MCP panel writes through
// this guard so a concurrent hand edit is never renamed over).
func (d *McpDoc) Read() []byte { return d.expect }

// ParseMcpJSON parses mcp.json content. A missing file is not an error:
// it yields an empty doc with mcpServers already present.
func ParseMcpJSON(data []byte) (*McpDoc, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		o, _ := parseJSONObj([]byte("{}"))
		o.Set("mcpServers", json.RawMessage("{}"))
		return &McpDoc{root: o, expect: []byte{}}, nil
	}
	o, err := parseJSONObj(data)
	if err != nil {
		return nil, err
	}
	if o.Obj("mcpServers") == nil {
		// A present-but-not-an-object mcpServers is a hand-edit we must
		// not normalise away: overwriting it with {} on the next save
		// would delete whatever the user actually wrote there.
		if o.Has("mcpServers") {
			return nil, fmt.Errorf("mcpServers is not a JSON object")
		}
		o.Set("mcpServers", json.RawMessage("{}"))
	}
	return &McpDoc{root: o, expect: data}, nil
}

// LoadMcpConfig reads path (empty/missing file → empty doc).
func LoadMcpConfig(path string) (*McpDoc, error) {
	doc, err := ParseMcpJSON(nil)
	if err != nil {
		return nil, err
	}
	doc.Path = path
	if path == "" {
		return doc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, err
	}
	parsed, err := ParseMcpJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	parsed.Path = path
	parsed.expect = raw
	return parsed, nil
}

// McpConfigPaths lists the config files pi reads, in precedence order
// (same order as the sidebar snapshot reader).
func McpConfigPaths(dir string) []string {
	if dir == "" {
		return nil
	}
	return []string{filepath.Join(dir, "mcp.json"), filepath.Join(dir, ".mcp.json")}
}

// Servers returns every server name in file order.
func (d *McpDoc) Servers() []string {
	srv := d.root.Obj("mcpServers")
	if srv == nil {
		return nil
	}
	out := make([]string, 0, len(srv.keys))
	out = append(out, srv.keys...)
	return out
}

// Get returns the definition for name (zero McpDef when absent).
func (d *McpDoc) Get(name string) McpDef {
	srv := d.root.Obj("mcpServers")
	if srv == nil {
		return McpDef{}
	}
	var def McpDef
	def.Name = name
	if !srv.Has(name) {
		return def
	}
	raw := srv.Get(name)
	obj, err := parseJSONObj(raw)
	if err != nil {
		return def
	}
	def.Type = jsonString(obj.Get("type"))
	def.Command = jsonString(obj.Get("command"))
	def.URL = jsonString(obj.Get("url"))
	def.Args = jsonStringSlice(obj.Get("args"))
	def.Env = jsonStringMap(obj.Get("env"))
	def.Headers = jsonStringMap(obj.Get("headers"))
	var dis bool
	if err := json.Unmarshal(obj.Get("disabled"), &dis); err == nil {
		def.Disabled = dis
	}
	return def
}

// Put writes def into mcpServers, preserving the file's key order. An
// existing entry is merged field by field so a partial edit (toggling
// disabled) leaves everything else exactly as it was.
func (d *McpDoc) Put(def McpDef) {
	name := strings.TrimSpace(def.Name)
	if name == "" {
		return
	}
	srv := d.root.Obj("mcpServers")
	if srv == nil {
		srv, _ = parseJSONObj([]byte("{}"))
		d.root.Set("mcpServers", json.RawMessage("{}"))
	}
	cur := srv.Obj(name)
	if cur == nil {
		cur, _ = parseJSONObj([]byte("{}"))
	}
	setStr(cur, "type", def.Type)
	setStr(cur, "command", def.Command)
	setStr(cur, "url", def.URL)
	// Only touch the collection fields the caller expressed an opinion
	// about: a nil Args/Env/Headers means "keep what is stored", which is
	// what a form save of an untouched row (and a Toggle on a server whose
	// args/env hold a shape we cannot model) needs. A non-nil but empty
	// value is a deliberate clear.
	if def.Args != nil {
		setSlice(cur, "args", def.Args)
	}
	if def.Env != nil {
		setMap(cur, "env", def.Env)
	}
	if def.Headers != nil {
		setMap(cur, "headers", def.Headers)
	}
	if def.Disabled {
		cur.Set("disabled", json.RawMessage("true"))
	} else {
		cur.Delete("disabled")
	}
	srv.Set(name, cur.mustMarshal())
	d.root.Set("mcpServers", srv.mustMarshal())
}

// ToggleDisabled flips a server's disabled flag in the doc (call Save to
// persist). Reports the new state; a missing server is an error.
func (d *McpDoc) ToggleDisabled(name string) (bool, error) {
	srv := d.root.Obj("mcpServers")
	if srv == nil || !srv.Has(name) {
		return false, fmt.Errorf("no MCP server named %q", name)
	}
	def := d.Get(name)
	def.Disabled = !def.Disabled
	d.Put(def)
	return def.Disabled, nil
}

// Delete removes a server. Reports whether it existed.
func (d *McpDoc) Delete(name string) bool {
	srv := d.root.Obj("mcpServers")
	if srv == nil {
		return false
	}
	if !srv.Has(name) {
		return false
	}
	srv.Delete(name)
	d.root.Set("mcpServers", srv.mustMarshal())
	return true
}

// mustMarshal is Marshal without the trailing newline (nested value).
func (o *jsonObj) mustMarshal() json.RawMessage {
	return json.RawMessage(bytes.TrimRight(o.Marshal(), "\n"))
}

// Bytes renders the whole document.
func (d *McpDoc) Bytes() []byte { return d.root.Marshal() }

// Save writes the document atomically, creating the parent dir if needed.
func (d *McpDoc) Save(path string) error {
	return d.SaveIfUnchanged(path, nil)
}

// SaveIfUnchanged writes the document only if path still holds the bytes
// the caller read (expect; nil skips the check). pi writes MCP-adjacent
// state itself, so a read-modify-write that renames over a newer file
// would silently drop the user's edit; the CAS narrows that window to the
// microseconds before rename(2). On a mismatch nothing is written.
func (d *McpDoc) SaveIfUnchanged(path string, expect []byte) error {
	if path == "" {
		return fmt.Errorf("no mcp.json path")
	}
	d.Path = path
	swapped, err := writeFileAtomicCAS(path, expect, d.Bytes(), 0o600)
	if err != nil {
		return err
	}
	if !swapped && expect != nil {
		return fmt.Errorf("%s changed on disk — reopen the panel and retry", filepath.Base(path))
	}
	return nil
}

func setStr(o *jsonObj, key, val string) {
	if strings.TrimSpace(val) == "" {
		o.Delete(key)
		return
	}
	o.Set(key, json.RawMessage(mustJSONString(val)))
}

// setSlice writes a collection field; a non-nil empty value clears it.
func setSlice(o *jsonObj, key string, vals []string) {
	if len(vals) == 0 {
		o.Delete(key)
		return
	}
	out, err := json.Marshal(vals)
	if err != nil {
		return
	}
	o.Set(key, out)
}

// setMap writes an object field (env/headers); a non-nil empty value
// clears it. Keys are sorted for a stable diff — the enclosing object
// keeps its own order, and env keys never had a meaningful order.
func setMap(o *jsonObj, key string, vals map[string]string) {
	if len(vals) == 0 {
		o.Delete(key)
		return
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	// sorted for a stable diff; the surrounding object keeps order, and
	// env keys never had a meaningful order to preserve.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	var buf bytes.Buffer
	buf.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString(mustJSONString(k) + ":" + mustJSONString(vals[k]))
	}
	buf.WriteString("}")
	o.Set(key, json.RawMessage(buf.Bytes()))
}

func jsonString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func jsonStringSlice(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var v []string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}

func jsonStringMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 {
		return nil
	}
	var v map[string]string
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil
	}
	return v
}
