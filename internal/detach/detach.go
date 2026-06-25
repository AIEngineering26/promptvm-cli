// Package detach re-execs the current binary as a detached background child so
// a hook-invoked command can return control to Claude Code immediately and
// never block the session (FR-3 / HOOK-3 / DX-6).
//
// We deliberately do NOT shell out to `setsid` (absent on macOS and Windows Git
// Bash). Instead the parent re-execs itself with stdio detached and an env
// marker set; the OS-specific SysProcAttr (Setsid on unix, DETACHED_PROCESS on
// Windows) fully detaches the child. Self-detach is the primary non-blocking
// guarantee, independent of Claude Code honoring the hook `async` field.
package detach

import (
	"os"
)

// EnvMarker is set on the detached child's environment. The command checks it
// to know it is the background worker and must not re-detach (avoiding a fork
// bomb).
const EnvMarker = "PROMPTVM_SYNC_DETACHED"

// IsChild reports whether the current process is the detached background child.
func IsChild() bool {
	return os.Getenv(EnvMarker) == "1"
}

// Reexec launches a detached copy of this binary with the same arguments and a
// copy of stdin redirected from the provided file (so the child can still read
// the hook payload). It returns the child PID. The caller (parent) should exit
// 0 immediately after a successful Reexec.
//
// stdinPath, when non-empty, is opened read-only and wired to the child's
// stdin; this lets the parent persist the hook's stdin to a temp file and hand
// it to the detached child, since the original stdin pipe closes when the
// parent returns.
func Reexec(stdinPath string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}

	var stdin *os.File
	if stdinPath != "" {
		stdin, err = os.Open(stdinPath)
		if err != nil {
			return 0, err
		}
		defer stdin.Close()
	} else {
		stdin, _ = os.Open(os.DevNull)
		if stdin != nil {
			defer stdin.Close()
		}
	}

	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer devnull.Close()

	env := append(os.Environ(), EnvMarker+"=1")

	attr := &os.ProcAttr{
		Env:   env,
		Files: []*os.File{stdin, devnull, devnull},
		Sys:   sysProcAttr(),
	}

	proc, err := os.StartProcess(exe, os.Args, attr)
	if err != nil {
		return 0, err
	}
	pid := proc.Pid
	// Release so the parent doesn't wait on the child.
	_ = proc.Release()
	return pid, nil
}
