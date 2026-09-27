//go:build !windows

package fakeexif

import "syscall"

func alive(pid int) bool {
	var ws syscall.WaitStatus
	// Reap it if it is our zombie; otherwise ask whether it exists.
	syscall.Wait4(pid, &ws, syscall.WNOHANG, nil)
	return syscall.Kill(pid, 0) == nil
}
