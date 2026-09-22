// Package update checks GitHub releases for a newer pitago and replaces
// the running binary. Stdlib only; no imports from the rest of pitago
// (leaf package, safe for app/builtin/main to share).
package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Repo coordinates.
const (
	Owner = "cavaldos"
	Repo  = "pitago"
)

// CheckTTL guards the GitHub API (60 req/h unauthenticated).
const CheckTTL = 24 * time.Hour

// ErrNeedSudo means the binary dir isn't writable (e.g. /usr/local/bin).
// Callers show Manual() instead. ErrDevBinary means there is no real
// binary to replace (go run / test binary).
var (
	ErrNeedSudo  = errors.New("binary dir not writable (need sudo)")
	ErrDevBinary = errors.New("dev binary has no install to replace")
)

// Normalize strips whitespace + a leading "v" ("v0.0.3" → "0.0.3").
func Normalize(v string) string {
	v = strings.TrimSpace(v)
	return strings.TrimPrefix(v, "v")
}

// IsRelease reports whether v looks like vX.Y.Z (ldflags-built binary).
// Source builds ("dev", commit hashes) return false.
func IsRelease(v string) bool {
	_, ok := parse(Normalize(v))
	return ok
}

func parse(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// NeedsUpdate reports latest > current. Unparsable currents ("dev",
// commit hash, "") always return true when latest parses — the manual
// /update and --update paths offer the install; the TUI auto-check
// additionally gates on IsRelease so source developers aren't nagged.
func NeedsUpdate(current, latest string) bool {
	lv, ok := parse(Normalize(latest))
	if !ok {
		return false
	}
	cv, ok := parse(Normalize(current))
	if !ok {
		return true
	}
	for i := range cv {
		if cv[i] != lv[i] {
			return cv[i] < lv[i]
		}
	}
	return false
}

// AssetName maps GOOS/GOARCH to the release alias built by release.yml
// (pitago-<os>-<arch>[.exe]).
func AssetName(goos, goarch string) string {
	name := "pitago-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// CurrentAsset is the alias for this process.
func CurrentAsset() string { return AssetName(runtime.GOOS, runtime.GOARCH) }

// LatestURL is the stable "latest" download link for an asset alias
// (same links README install uses — no API auth, no asset-ID lookup).
func LatestURL(asset string) string {
	return "https://github.com/" + Owner + "/" + Repo + "/releases/latest/download/" + asset
}

// Manual returns the copy-paste fallback when auto-replace can't write
// (system dir) or is skipped (Windows lock, dev binary). No-sudo first:
// ~/.local/bin is user-writable so future `pitago --update` needs no sudo.
func Manual(asset string) string {
	url := LatestURL(asset)
	if strings.HasSuffix(asset, ".exe") {
		return "Invoke-WebRequest " + url + " -OutFile pitago.exe  # replace the exe on your PATH, then restart"
	}
	return "mkdir -p ~/.local/bin && curl -L -o ~/.local/bin/pitago " + url + " && chmod +x ~/.local/bin/pitago"
}

// FetchLatest resolves the newest release tag ("v0.0.5"). Primary path
// reads the redirect of .../releases/latest (plain github.com, no auth,
// no API rate limit); the api.github.com lookup is only a fallback.
// Each attempt gets its own budget: sharing one ctx starves the fallback
// (first timeout eats the whole deadline, second fails instantly).
func FetchLatest(ctx context.Context) (string, error) {
	ctx1, cancel1 := context.WithTimeout(ctx, 10*time.Second)
	tag, redirectErr := latestViaRedirect(ctx1)
	cancel1()
	if redirectErr == nil {
		return tag, nil
	}
	ctx2, cancel2 := context.WithTimeout(ctx, 10*time.Second)
	defer cancel2()
	tag, err := fetchLatestAPI(ctx2)
	if err != nil {
		return "", fmt.Errorf("update check failed (%v; api fallback: %v)", redirectErr, err)
	}
	return tag, nil
}

// latestViaRedirect follows nothing: github.com answers
// /releases/latest with a 302 to /releases/tag/<tag>.
func latestViaRedirect(ctx context.Context) (string, error) {
	client := http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://github.com/"+Owner+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "pitago-update")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("releases/latest: %s", resp.Status)
	}
	return tagFromLocation(resp.Header.Get("Location"))
}

// tagFromLocation pulls the tag off ".../releases/tag/v0.0.5".
func tagFromLocation(loc string) (string, error) {
	i := strings.LastIndex(loc, "/tag/")
	if i < 0 {
		return "", fmt.Errorf("no /tag/ in redirect %q", loc)
	}
	tag := strings.TrimSpace(loc[i+len("/tag/"):])
	if _, ok := parse(Normalize(tag)); !ok {
		return "", fmt.Errorf("bad tag %q", tag)
	}
	return tag, nil
}

func fetchLatestAPI(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://api.github.com/repos/"+Owner+"/"+Repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "pitago-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden {
			return "", fmt.Errorf("github api rate-limited (403) — try again later")
		}
		return "", fmt.Errorf("github releases: %s", resp.Status)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	if _, ok := parse(Normalize(r.Tag)); !ok {
		return "", fmt.Errorf("github releases: bad tag %q", r.Tag)
	}
	return r.Tag, nil
}

// CachePath is ~/.config/pitago/update.json: {last_check, latest_tag}.
func CachePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "pitago", "update.json")
}

// Cache remembers the last successful check.
type Cache struct {
	LastCheck time.Time `json:"last_check"`
	LatestTag string    `json:"latest_tag"`
}

// LoadCache reads the cache (missing/corrupt → zero, no error).
func LoadCache(path string) Cache {
	var c Cache
	if path == "" {
		return c
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(raw, &c)
	return c
}

// SaveCache writes the cache (dir 0700; failure returns err, non-fatal).
func SaveCache(path, tag string) error {
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(Cache{LastCheck: time.Now(), LatestTag: tag})
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// Fresh reports whether the cache was written within ttl.
func Fresh(c Cache, ttl time.Duration) bool {
	if c.LatestTag == "" || c.LastCheck.IsZero() {
		return false
	}
	return time.Since(c.LastCheck) < ttl
}

// Install downloads url and atomically replaces the running binary.
// Returns ErrDevBinary for go-run/test binaries, ErrNeedSudo when the
// install dir isn't writable (caller prints Manual instead).
func Install(ctx context.Context, url string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if p, err := filepath.EvalSymlinks(exe); err == nil {
		exe = p
	}
	if isDevBinary(exe) {
		return ErrDevBinary
	}
	if runtime.GOOS == "windows" {
		// A running .exe is locked — rename can't replace it.
		return fmt.Errorf("windows: close pitago, then %s", Manual(CurrentAsset()))
	}
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".pitago-update-*")
	if err != nil {
		if os.IsPermission(err) {
			return ErrNeedSudo
		}
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if err := download(ctx, url, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpName, exe); err != nil {
		if os.IsPermission(err) {
			return ErrNeedSudo
		}
		return err
	}
	return nil
}

func download(ctx context.Context, url string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pitago-update")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// isDevBinary smells out go-run/test binaries, which have no install
// to replace (os.Executable points into /tmp, go-build cache, …).
func isDevBinary(exe string) bool {
	low := strings.ToLower(exe)
	for _, s := range []string{"go-build", "/tmp/", "\\temp\\", ".test", ".test.exe"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}
