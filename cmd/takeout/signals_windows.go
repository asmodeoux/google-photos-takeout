//go:build windows

package main

import (
	"os"
	"syscall"
)

// Go delivers closing the console window, logoff and shutdown as SIGTERM.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
