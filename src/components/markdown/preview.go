// Preview ports the concept of pi-compositor/src/app/markdown-preview.ts:
// write the markdown to a temp file and open it in a host viewer. Pitago
// itself is a Bubble Tea alt-screen app, so glow/mdcat run in a NEW
// terminal window instead of inline (which would clobber the chat screen).
package markdown

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Preview writes md to a temp file and opens it with the first available
// host viewer. Returns (host, ok). Hosts, in order:
//
//  1. $PITAGO_PREVIEW_CMD — a shell command; the temp file path is
//     appended as its final argument.
//  2. macOS Terminal + glow/mdcat (osascript) — a new Terminal window.
//  3. Nothing — ok=false, caller shows the "install glow" toast.
func Preview(md string, width int) (string, bool) {
	if strings.TrimSpace(md) == "" {
		return "", false
	}
	f, err := os.CreateTemp("", "pitago-preview-*.md")
	if err != nil {
		return "", false
	}
	name := f.Name()
	if _, err := f.WriteString(md); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", false
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", false
	}

	if cmd := os.Getenv("PITAGO_PREVIEW_CMD"); cmd != "" {
		if runDetached(shCmd(cmd + " " + shellQuote(name))) {
			return "PITAGO_PREVIEW_CMD", true
		}
	}

	if runtime.GOOS == "darwin" {
		viewer := ""
		for _, bin := range []string{"glow", "mdcat"} {
			if p, err := exec.LookPath(bin); err == nil {
				viewer = shellQuote(p) + " -p -w " + fmt.Sprint(width) + " " + shellQuote(name)
				break
			}
		}
		if viewer == "" {
			viewer = "cat " + shellQuote(name) // Terminal always has cat
		}
		script := "tell application \"Terminal\" to do script " + shellQuote(viewer)
		if runDetached(exec.Command("osascript", "-e", script)) {
			return "Terminal" + markdownHostSuffix(viewer), true
		}
	}
	_ = os.Remove(name)
	return "", false
}

func markdownHostSuffix(viewer string) string {
	if strings.Contains(viewer, "glow") {
		return " (glow)"
	}
	if strings.Contains(viewer, "mdcat") {
		return " (mdcat)"
	}
	return ""
}

func shCmd(cmd string) *exec.Cmd {
	return exec.Command("sh", "-c", cmd)
}

func runDetached(cmd *exec.Cmd) bool {
	if cmd == nil {
		return false
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err == nil {
		cmd.Stdout = devnull
		cmd.Stderr = devnull
	}
	cmd.Stdin = nil
	// Detach from our process group/terminal; caller does not wait.
	cmd.SysProcAttr = detachedProcAttr()
	return cmd.Start() == nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
