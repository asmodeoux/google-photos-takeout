//go:build windows

package media

import (
	"time"

	"golang.org/x/sys/windows"
)

// setBirth sets the creation time that Explorer shows as "Date created".
func setBirth(path string, t time.Time) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	ft := windows.NsecToFiletime(t.UnixNano())
	return windows.SetFileTime(h, &ft, nil, nil)
}
