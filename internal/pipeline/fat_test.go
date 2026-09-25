package pipeline

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

func TestTooBigForFAT(t *testing.T) {
	g := func(name string, size uint64) group {
		return group{members: []member{{Entry: zipindex.Entry{Name: name, Size: size}}}}
	}
	groups := []group{g("a.mp4", fatMax+1), g("b.webm", 3<<30), g("c.jpg", 10)}
	if n := tooBigForFAT("FAT32", groups); n != 2 {
		t.Fatalf("FAT32: %d", n)
	}
	for _, typ := range []string{"msdos", "vfat"} {
		if n := tooBigForFAT(typ, groups); n != 2 {
			t.Fatalf("%s: %d", typ, n)
		}
	}
	if n := tooBigForFAT("exFAT", groups); n != 0 {
		t.Fatalf("exFAT: %d", n)
	}
}

func TestTimeLogCountsFailures(t *testing.T) {
	var l timeLog
	l.set(filepath.Join(t.TempDir(), "missing.jpg"), timeNowForTest())
	if l.count != 1 || len(l.paths) != 1 {
		t.Fatalf("count %d paths %v", l.count, l.paths)
	}
}

func timeNowForTest() time.Time { return time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC) }
