package state

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset puts the locked byte range far past the text in the file, so
// another run can still read who holds the lock.
const lockOffset = 1 << 30

func lockFile(f *os.File) error {
	ol := windows.Overlapped{Offset: lockOffset}
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
}

func heldElsewhere(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

func unlockFile(f *os.File) {
	ol := windows.Overlapped{Offset: lockOffset}
	_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol)
}
