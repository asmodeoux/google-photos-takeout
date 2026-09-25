package pipeline

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

// Two different files that happen to share size and CRC32 must not be merged.
func TestConfirmDuplicatesSplitsCRCCollisions(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "takeout-20200101T000000Z-1-001.zip")
	writeZip(t, z, map[string][]byte{
		"Takeout/Google Photos/Photos from 2019/a.jpg": []byte("AAAA-first-photo"),
		"Takeout/Google Photos/Trip/a.jpg":             []byte("AAAA-first-photo"),
		"Takeout/Google Photos/Photos from 2019/b.jpg": []byte("BBBB-other-photo"),
	})
	idx, err := zipindex.Open([]string{z})
	if err != nil {
		t.Fatal(err)
	}
	var items []member
	for _, e := range idx.Entries {
		items = append(items, member{Entry: e})
	}
	// Pretend b.jpg collides with a.jpg on size and CRC32.
	crc := items[0].CRC32
	for i := range items {
		items[i].CRC32 = crc
	}
	groups := groupBy(items)
	if len(groups) != 1 {
		t.Fatalf("setup: %d groups", len(groups))
	}
	readers, err := openZips([]string{z})
	if err != nil {
		t.Fatal(err)
	}
	defer closeZips(readers)
	got, err := confirmDuplicates(context.Background(), readers, groups)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 groups, got %d", len(got))
	}
	if got[0].id != groups[0].id || got[1].id == got[0].id {
		t.Fatalf("ids %q %q", got[0].id, got[1].id)
	}
	sizes := map[int]bool{len(got[0].members): true, len(got[1].members): true}
	if !sizes[2] || !sizes[1] {
		t.Fatalf("members %d %d", len(got[0].members), len(got[1].members))
	}
}
