package awake

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var setThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// The request belongs to the thread that made it, so one locked goroutine
// makes it and later clears it.
func hold() (func(), error) {
	if err := setThreadExecutionState.Find(); err != nil {
		return nil, err
	}
	started := make(chan error, 1)
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		defer close(finished)
		if r, _, err := setThreadExecutionState.Call(esContinuous | esSystemRequired); r == 0 {
			started <- fmt.Errorf("SetThreadExecutionState: %v", err)
			return
		}
		started <- nil
		<-done
		setThreadExecutionState.Call(esContinuous)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return func() { close(done); <-finished }, nil
}
