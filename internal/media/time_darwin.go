//go:build darwin

package media

import (
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

func setBirth(path string, t time.Time) error {
	ts := unix.NsecToTimespec(t.UnixNano())
	buf := unsafe.Slice((*byte)(unsafe.Pointer(&ts)), int(unsafe.Sizeof(ts)))
	al := &unix.Attrlist{
		Bitmapcount: unix.ATTR_BIT_MAP_COUNT,
		Commonattr:  unix.ATTR_CMN_CRTIME,
	}
	return unix.Setattrlist(path, al, buf, 0)
}
