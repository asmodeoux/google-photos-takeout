package media

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func birthTime(t *testing.T, p string) (time.Time, bool) {
	var st unix.Stat_t
	if err := unix.Stat(p, &st); err != nil {
		t.Fatal(err)
	}
	return time.Unix(st.Btim.Unix()), true
}
