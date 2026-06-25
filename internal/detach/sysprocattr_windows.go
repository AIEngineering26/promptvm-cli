//go:build windows

package detach

import "syscall"

const (
	// Windows process creation flags for a fully detached background process.
	detachedProcess    = 0x00000008
	createNewProcGroup = 0x00000200
)

// sysProcAttr detaches the child on Windows (no console, new process group),
// the cross-platform equivalent of the unix Setsid path.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcGroup}
}
