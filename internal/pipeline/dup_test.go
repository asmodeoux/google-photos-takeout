package pipeline

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
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

	// The journal is keyed by id, so the same content must get the same id
	// when the zips list the files in another order.
	idOf := func(gs []group) map[string]string {
		m := map[string]string{}
		for _, g := range gs {
			for _, mem := range g.members {
				m[mem.EntryName] = g.id
			}
		}
		return m
	}
	reversed := groupBy(items)
	ms := reversed[0].members
	for i, j := 0, len(ms)-1; i < j; i, j = i+1, j-1 {
		ms[i], ms[j] = ms[j], ms[i]
	}
	got2, err := confirmDuplicates(context.Background(), readers, reversed)
	if err != nil {
		t.Fatal(err)
	}
	a, b := idOf(got), idOf(got2)
	for name, id := range a {
		if b[name] != id {
			t.Errorf("%s: id %q in one order, %q in the other", name, id, b[name])
		}
	}
}

func TestHugeSidecarIsRefusedNotRead(t *testing.T) {
	z := filepath.Join(t.TempDir(), "takeout-20200101T000000Z-1-001.zip")
	huge := append([]byte(`{"title":"`), bytes.Repeat([]byte("a"), maxSidecar+10)...)
	writeZip(t, z, map[string][]byte{"Takeout/Google Photos/Photos from 2019/a.jpg.json": append(huge, `"}`...)})
	idx, err := zipindex.Open([]string{z})
	if err != nil {
		t.Fatal(err)
	}
	readers, err := openZips([]string{z})
	if err != nil {
		t.Fatal(err)
	}
	defer closeZips(readers)
	if _, err := readSidecar(readers, idx.Entries[0]); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("got %v", err)
	}
}
