//go:build windows

package proc

import (
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// KillTree stops cmd and every process it started. exiftool.exe runs its
// script in a perl.exe child that keeps the pipes open, so killing only
// exiftool.exe would leave perl.exe holding files until takeout exits.
//
// The descendants are found from a process snapshot. Windows reuses process
// ids, so a process whose parent id matches is killed only if it was created
// after cmd was started (started comes from the caller, taken at Start).
func KillTree(cmd *exec.Cmd, started time.Time) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	kids := descendants(uint32(cmd.Process.Pid), started)
	_ = cmd.Process.Kill()
	for _, pid := range kids {
		if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid); err == nil {
			_ = windows.TerminateProcess(h, 1)
			windows.CloseHandle(h)
		}
	}
}

// descendants lists the processes below root created at or after started.
func descendants(root uint32, started time.Time) []uint32 {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snap)
	children := map[uint32][]uint32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		children[e.ParentProcessID] = append(children[e.ParentProcessID], e.ProcessID)
	}
	var out []uint32
	queue := []uint32{root}
	seen := map[uint32]bool{root: true}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			if seen[c] || !createdSince(c, started) {
				continue
			}
			seen[c] = true
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

func createdSince(pid uint32, started time.Time) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return false
	}
	// A second of slack for clock granularity; a reused id belongs to a
	// process created long before.
	return time.Unix(0, created.Nanoseconds()).After(started.Add(-time.Second))
}

// Alive reports whether a process with this id is running.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, 0)
	return ev == uint32(windows.WAIT_TIMEOUT)
}
