package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDir(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// no args → current directory
	if got, err := resolveDir("", nil); err != nil || got != cwd {
		t.Errorf("empty = %q, %v; want %q", got, err, cwd)
	}
	// explicit dir, relative resolves against launch cwd
	tmp := t.TempDir()
	rel, err := filepath.Rel(cwd, tmp)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveDir("", []string{rel}); err != nil || got != tmp {
		t.Errorf("relative = %q, %v; want %q", got, err, tmp)
	}
	// --cwd wins over positional
	other := t.TempDir()
	if got, err := resolveDir(other, []string{tmp}); err != nil || got != other {
		t.Errorf("--cwd = %q, %v; want %q", got, err, other)
	}
	// ~ expands to home
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := resolveDir("~", nil); err != nil || got != home {
		t.Errorf("~ = %q, %v; want %q", got, err, home)
	}
	// missing path errors
	if _, err := resolveDir("", []string{filepath.Join(tmp, "nope")}); err == nil {
		t.Error("missing dir should error")
	}
	// file (not dir) errors
	f := filepath.Join(tmp, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveDir(f, nil); err == nil {
		t.Error("file path should error")
	}
}
