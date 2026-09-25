//go:build !windows

package proc

import (
	"syscall"
	"testing"
)

func alive(pid int) bool {
	var ws syscall.WaitStatus
	// Reap it if it became our zombie; otherwise ask whether it exists.
	syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
	return syscall.Kill(pid, 0) == nil
}

func killPid(pid int) { syscall.Kill(pid, syscall.SIGKILL) }

// Kill stops ExifTool and anything it started, such as a helper process.
func TestKillStopsTheWholeGroup(t *testing.T) {
	cmd, grandchild := startHelper(t)
	Kill(cmd)
	cmd.Wait()
	waitGone(t, grandchild)
}
