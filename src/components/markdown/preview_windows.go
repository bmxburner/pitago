//go:build windows

package markdown

import "syscall"

func detachedProcAttr() *syscall.SysProcAttr {
	return nil
}
