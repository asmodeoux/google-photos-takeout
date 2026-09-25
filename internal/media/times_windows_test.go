package media

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func birthTime(t *testing.T, p string) (time.Time, bool) {
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	d := st.Sys().(*syscall.Win32FileAttributeData)
	return time.Unix(0, d.CreationTime.Nanoseconds()), true
}
