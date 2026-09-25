//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
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

// KillTreeOnExit is a no-op on Unix: children in their own group see stdin
// close when takeout exits, and ffmpeg is stopped through its context.
func KillTreeOnExit() {}
