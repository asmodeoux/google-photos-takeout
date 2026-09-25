package proc

import (
	"testing"

	"golang.org/x/sys/windows"
)

func alive(pid int) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
}

func killPid(pid int) {
	if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid)); err == nil {
		windows.TerminateProcess(h, 1)
		windows.CloseHandle(h)
	}
}

// exiftool.exe starts perl.exe. When takeout ends, however it ends, the job
// object from KillTreeOnExit must end perl.exe too, or it keeps files open.
func TestJobObjectEndsGrandchildren(t *testing.T) {
	cmd, grandchild := startHelper(t, "TAKEOUT_PROC_JOB=1")
	cmd.Process.Kill()
	cmd.Wait()
	waitGone(t, grandchild)
}
