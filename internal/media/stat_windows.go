//go:build windows

package media

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// Stat reports the filesystem type (NTFS, exFAT, FAT32, ReFS) and the free
// bytes available to this user on the volume that holds path.
func Stat(path string) (FS, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return FS{}, err
	}
	p, err := windows.UTF16PtrFromString(abs)
	if err != nil {
		return FS{}, err
	}
	vol := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(p, &vol[0], uint32(len(vol))); err != nil {
		return FS{}, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(&vol[0], &free, &total, &totalFree); err != nil {
		return FS{}, err
	}
	name := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(&vol[0], nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		return FS{Type: "unknown", Free: free}, nil
	}
	return FS{Type: windows.UTF16ToString(name), Free: free}, nil
}
