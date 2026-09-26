package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
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
	got, err := confirmDuplicates(context.Background(), readers, groups, nil)
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
	got2, err := confirmDuplicates(context.Background(), readers, reversed, nil)
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

// Duplicate confirmation reports every hashed file, so one group with many
// copies still moves the count.
func TestConfirmDuplicatesTicksPerFile(t *testing.T) {
	dir := t.TempDir()
	z := filepath.Join(dir, "takeout-20200101T000000Z-1-001.zip")
	files := map[string][]byte{"Takeout/Google Photos/Photos from 2019/single.jpg": []byte("one")}
	for _, album := range []string{"A", "B", "C", "D"} {
		files["Takeout/Google Photos/"+album+"/same.jpg"] = []byte("same bytes")
	}
	writeZip(t, z, files)
	idx, err := zipindex.Open([]string{z})
	if err != nil {
		t.Fatal(err)
	}
	var items []member
	for _, e := range idx.Entries {
		items = append(items, member{Entry: e})
	}
	readers, err := openZips([]string{z})
	if err != nil {
		t.Fatal(err)
	}
	defer closeZips(readers)
	var ticks [][2]int
	if _, err := confirmDuplicates(context.Background(), readers, groupBy(items), func(d, n int) { ticks = append(ticks, [2]int{d, n}) }); err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 4 || ticks[3] != [2]int{4, 4} {
		t.Fatalf("ticks %v", ticks)
	}
}

// BenchmarkConfirmDuplicates measures duplicate confirmation on 300 groups of
// 3 album copies of 512 KB (about 460 MB of zips, made once in
// $TMPDIR/takeout-bench-dups and reused). Opt-in; never run in CI:
//
//	TAKEOUT_BENCH_DUPS=1 go test ./internal/pipeline -run '^$' -bench ConfirmDuplicates -benchtime 3x
//
// Set TAKEOUT_BENCH_DUPS_DIR to put the zips on another disk, such as an
// external hard disk, where parallel reads can be slower.
func BenchmarkConfirmDuplicates(b *testing.B) {
	if os.Getenv("TAKEOUT_BENCH_DUPS") != "1" {
		b.Skip("set TAKEOUT_BENCH_DUPS=1 to run")
	}
	dir := os.Getenv("TAKEOUT_BENCH_DUPS_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "takeout-bench-dups")
	}
	z := filepath.Join(dir, "takeout-20200101T000000Z-1-001.zip")
	if _, err := os.Stat(z); err != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			b.Fatal(err)
		}
		rng := rand.New(rand.NewPCG(1, 2))
		files := map[string][]byte{}
		for g := range 300 {
			data := make([]byte, 512<<10)
			for i := range data {
				data[i] = byte(rng.Uint32())
			}
			for _, album := range []string{"A", "B", "C"} {
				files[fmt.Sprintf("Takeout/Google Photos/%s/%03d.jpg", album, g)] = data
			}
		}
		writeZip(b, z, files)
	}
	idx, err := zipindex.Open([]string{z})
	if err != nil {
		b.Fatal(err)
	}
	var items []member
	for _, e := range idx.Entries {
		items = append(items, member{Entry: e})
	}
	readers, err := openZips([]string{z})
	if err != nil {
		b.Fatal(err)
	}
	defer closeZips(readers)
	b.ResetTimer()
	for b.Loop() {
		if _, err := confirmDuplicates(context.Background(), readers, groupBy(items), nil); err != nil {
			b.Fatal(err)
		}
	}
}
