//go:build !windows

package detach

import "syscall"

// sysProcAttr detaches the child into its own session on unix (Setsid), so it
// survives the parent exiting and the controlling terminal closing. This is the
// syscall, not the absent `setsid` binary.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
