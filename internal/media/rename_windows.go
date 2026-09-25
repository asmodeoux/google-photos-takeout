//go:build windows

package media

import (
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// renameNoReplace uses MoveFileEx without MOVEFILE_REPLACE_EXISTING. Newly
// written files are often held for a moment by Defender or the search indexer,
// so sharing and access errors are retried with a short backoff.
func renameNoReplace(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	wait := 50 * time.Millisecond
	for attempt := 0; ; attempt++ {
		err = windows.MoveFileEx(from, to, 0)
		if err == nil {
			return nil
		}
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: fs.ErrExist}
		}
		if attempt >= 6 || !(errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)) {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
		}
		RenameRetries.Add(1)
		time.Sleep(wait)
		wait *= 2
	}
}
