package media

import (
	"io"
	"os"
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

// Clone copies bytes. On APFS it shares disk blocks.
func Clone(src, dst string) error {
	if err := cloneFile(src, dst); err == nil {
		return nil
	}
	return copyFile(src, dst)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}
