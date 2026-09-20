package pirpc

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeystore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.json")
	if got := LoadKeys(path); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
	if err := SaveKey(path, "OPENROUTER_API_KEY", "sk-test"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 600", fi.Mode().Perm())
	}
	keys := LoadKeys(path)
	if keys["OPENROUTER_API_KEY"] != "sk-test" {
		t.Fatalf("got %v", keys)
	}
	if LookupEnv("openrouter") != "OPENROUTER_API_KEY" {
		t.Fatal("lookup fail")
	}
	if LookupEnv("nope") != "" {
		t.Fatal("lookup should be empty")
	}
	if err := DeleteKey(path, "OPENROUTER_API_KEY"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if len(LoadKeys(path)) != 0 {
		t.Fatal("expected empty after delete")
	}
}
