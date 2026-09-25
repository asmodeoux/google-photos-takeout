//go:build !windows

package media

import (
	"errors"
	"io/fs"
	"os"
	"syscall"
)

// renameNoReplace links dst to src and then removes src. A link fails when dst
// exists, which a plain rename would silently overwrite.
func renameNoReplace(src, dst string) error {
	err := os.Link(src, dst)
	if err == nil {
		// dst is in place. A src that cannot be removed is only a leftover;
		// reporting failure here would make the caller move the file again.
		_ = os.Remove(src)
		return nil
	}
	if errors.Is(err, fs.ErrExist) {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: fs.ErrExist}
	}
	if !linkUnsupported(err) {
		return err
	}
	// exFAT, FAT and some network mounts have no hard links. takeout is the
	// only writer in the results folder, so check-then-rename is safe there.
	if _, err := os.Lstat(dst); err == nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dst)
}

func linkUnsupported(err error) bool {
	for _, e := range []error{syscall.EPERM, syscall.ENOTSUP, syscall.EOPNOTSUPP, syscall.EXDEV, syscall.EMLINK, syscall.ENOSYS} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}
