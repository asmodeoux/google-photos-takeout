//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
	"time"
)

// Isolate puts cmd in its own process group, so Ctrl+C in the terminal reaches
// only takeout, which then finishes the files in flight. Call before Start.
func Isolate(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// Kill stops cmd and every process in its group.
func Kill(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
}

// KillTree stops cmd and everything it started. On Unix the process group
// holds them all, even after cmd itself has exited.
func KillTree(cmd *exec.Cmd, started time.Time) { Kill(cmd) }

// KillOrphans stops what cmd started after cmd itself has exited and been
// waited for, such as the real ExifTool behind a wrapper script. Call it
// right after Wait: while any member lives, the group id cannot be reused.
func KillOrphans(cmd *exec.Cmd, started time.Time) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// KillTreeOnExit is a no-op on Unix: children in their own group see stdin
// close when takeout exits, and ffmpeg is stopped through its context.
func KillTreeOnExit() {}

// Alive reports whether a process with this id exists.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
