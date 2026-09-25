package media

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// SetTimes sets modification time and, on macOS, birth time (Content Created).
func SetTimes(path string, t time.Time) error {
	if err := os.Chtimes(path, t, t); err != nil {
		return err
	}
	return setBirth(path, t)
}

// FS is the filesystem the results directory sits on.
type FS struct {
	Type string
	Free uint64
	APFS bool
}

// Clone copies bytes. On APFS it shares disk blocks. It never replaces dst.
func Clone(src, dst string) error {
	if err := cloneFile(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

// Copy writes a full copy of src to a new file dst. It never replaces dst: an
// existing dst returns an error that matches fs.ErrExist.
func Copy(src, dst string) error {
	return copyFile(src, dst)
}

// copyFile writes a hidden sibling first and moves it into place without
// replacing anything, so a crash never leaves a truncated file under the
// final name, where a resumed run would take it as complete.
func copyFile(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return &os.LinkError{Op: "copy", Old: src, New: dst, Err: fs.ErrExist}
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	// A short fixed-length name: dst may already be at the 255-byte limit.
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	tmp := filepath.Join(filepath.Dir(dst), ".takeout-"+hex.EncodeToString(rnd[:])+".partial")
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if serr := out.Sync(); err == nil {
		err = serr
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = Rename(tmp, dst)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}
