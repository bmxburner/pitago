package theme

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGet(t *testing.T) {
	if Get("").Name != "default" {
		t.Fatal("empty should fall back to default")
	}
	for _, n := range []string{"one-dark", "OneDark", "ONE_DARK", "one dark", "gruvbox", "Dracula", "tokyo-night", "nord"} {
		if Get(n).Name == "default" && n != "" {
			// only "" maps to default; named ones must resolve
			if n != "" && Get(n).Name == "default" {
				t.Fatalf("%q resolved to default", n)
			}
		}
	}
	if Get("nope").Name != "default" {
		t.Fatal("unknown should fall back to default")
	}
	for _, n := range Names() {
		if Get(n).Name != n {
			t.Fatalf("%q should resolve to itself", n)
		}
	}
	if len(Names()) < 20 {
		t.Fatalf("want a large theme set, got %d", len(Names()))
	}
}

func TestSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "theme.json")
	if got := Load(p); got != "" {
		t.Fatalf("missing file should load empty, got %q", got)
	}
	if err := Save(p, "OneDark"); err != nil {
		t.Fatal(err)
	}
	if got := Load(p); got != "one-dark" {
		t.Fatalf("want one-dark, got %q", got)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("want 0600, got %o", fi.Mode().Perm())
	}
}
