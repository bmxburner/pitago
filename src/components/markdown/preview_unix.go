//go:build !windows

package markdown

import "syscall"

// detachedProcAttr detaches preview viewers from our process group so they
// survive pitago exiting and never receive our SIGINT/SIGWINCH.
func detachedProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
