package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// startedFromExplorer reports a double-click: Explorer gives takeout a console
// of its own, so takeout is the only process attached to it.
func startedFromExplorer() bool {
	if getConsoleProcessList.Find() != nil {
		return false
	}
	var ids [2]uint32
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	return n == 1
}
