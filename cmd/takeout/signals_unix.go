//go:build !windows

package main

import (
	"os"
	"syscall"
)

var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
