package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/state"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

// crashTakeout is a small export: a.jpg is in the year folder and in two
// albums, b.jpg only in the year folder.
func crashTakeout(t *testing.T) (arch, results string) {
	t.Helper()
	dir := t.TempDir()
	tk := testgen.New()
	a, b := testgen.JPEG(1), testgen.JPEG(2)
	side := func(d int) *testgen.Side {
		return &testgen.Side{Taken: time.Date(2019, 3, d, 9, 0, 0, 0, time.UTC)}
	}
	tk.Photo(1, "Photos from 2019", "a.jpg", a, side(1))
	tk.Photo(1, "Photos from 2019", "b.jpg", b, side(2))
	tk.Photo(1, "Trip", "a.jpg", a, side(1))
	tk.Photo(1, "Beach", "a.jpg", a, side(1))
	arch = filepath.Join(dir, "archives")
	if _, err := tk.Write(arch); err != nil {
		t.Fatal(err)
	}
	return arch, filepath.Join(dir, "results")
}

// snapshot maps every file under results, except .takeout, to its content hash.
func snapshot(t *testing.T, results string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(results, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".takeout" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(results, p)
		sum := sha256.Sum256(b)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// lastRecord returns the journal's latest record whose path is rel.
func lastRecord(t *testing.T, results, rel string) state.Rec {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(results, ".takeout", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var found state.Rec
	for _, line := range strings.Split(string(b), "\n") {
		var r state.Rec
		if json.Unmarshal([]byte(line), &r) == nil && r.Path == rel {
			found = r
		}
	}
	if found.ID == "" {
		t.Fatalf("no journal record for %s", rel)
	}
	return found
}

func appendRecord(t *testing.T, results string, r state.Rec) {
	t.Helper()
	j, err := state.OpenJournal(filepath.Join(results, ".takeout", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := j.Put(r); err != nil {
		t.Fatal(err)
	}
}

func runOK(t *testing.T, arch, results string) Report {
	t.Helper()
	code, rep, err := Run(context.Background(), testOptions(arch, results))
	if code != ExitOK {
		t.Fatalf("run exit %d: %v %v", code, err, rep.Errors)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: results}); vcode != ExitOK {
		t.Fatalf("verify exit %d: %v", vcode, verr)
	}
	return rep
}

func sameFiles(t *testing.T, want, got map[string]string) {
	t.Helper()
	if strings.Join(keys(want), "\n") != strings.Join(keys(got), "\n") {
		t.Fatalf("files changed after resume\nbefore: %v\nafter:  %v", keys(want), keys(got))
	}
}

func TestRerunLeavesLibraryUnchanged(t *testing.T) {
	requireTools(t, "exiftool")
	arch, results := crashTakeout(t)
	runOK(t, arch, results)
	before := snapshot(t, results)
	runOK(t, arch, results)
	after := snapshot(t, results)
	sameFiles(t, before, after)
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed on rerun", k)
		}
	}
}

// Killed after the file was moved into the year folder, before the move was
// journaled as done: the file is adopted, not placed a second time.
func TestResumeAfterCrashAfterMove(t *testing.T) {
	requireTools(t, "exiftool")
	arch, results := crashTakeout(t)
	runOK(t, arch, results)
	before := snapshot(t, results)
	rec := lastRecord(t, results, "2019/a.jpg")
	for _, a := range rec.Albums {
		os.Remove(filepath.Join(results, filepath.FromSlash(a)))
	}
	appendRecord(t, results, state.Rec{ID: rec.ID, SHA: rec.SHA, Stage: "placing", Path: "2019/a.jpg"})
	runOK(t, arch, results)
	sameFiles(t, before, snapshot(t, results))
}

// Killed after the intent was journaled but before the move: the file is
// placed under the name it was going to get, not "a (2).jpg".
func TestResumeAfterCrashBeforeMove(t *testing.T) {
	requireTools(t, "exiftool")
	arch, results := crashTakeout(t)
	runOK(t, arch, results)
	before := snapshot(t, results)
	rec := lastRecord(t, results, "2019/a.jpg")
	for _, a := range rec.Albums {
		os.Remove(filepath.Join(results, filepath.FromSlash(a)))
	}
	staging := filepath.Join(results, ".takeout", "staging")
	os.MkdirAll(staging, 0o755)
	if err := os.Rename(filepath.Join(results, "2019", "a.jpg"), filepath.Join(staging, rec.SHA)); err != nil {
		t.Fatal(err)
	}
	appendRecord(t, results, state.Rec{ID: rec.ID, SHA: rec.SHA, Stage: "placing", Path: "2019/a.jpg"})
	runOK(t, arch, results)
	sameFiles(t, before, snapshot(t, results))
}

// Killed between journaling an album link and making it: the missing link is
// made under the journaled name, and the finished one is not copied again.
func TestResumeAfterCrashDuringAlbums(t *testing.T) {
	requireTools(t, "exiftool")
	arch, results := crashTakeout(t)
	runOK(t, arch, results)
	before := snapshot(t, results)
	rec := lastRecord(t, results, "2019/a.jpg")
	if len(rec.Albums) != 2 {
		t.Fatalf("albums %v", rec.Albums)
	}
	os.Remove(filepath.Join(results, filepath.FromSlash(rec.Albums[1])))
	appendRecord(t, results, state.Rec{ID: rec.ID, SHA: rec.SHA, Stage: "placed", Path: rec.Path, Albums: rec.Albums})
	runOK(t, arch, results)
	sameFiles(t, before, snapshot(t, results))
}

// Killed in the middle of a journal write: the partial line is lost, but the
// records written after it are not.
func TestResumeAfterTornJournalLine(t *testing.T) {
	requireTools(t, "exiftool")
	arch, results := crashTakeout(t)
	runOK(t, arch, results)
	before := snapshot(t, results)
	f, err := os.OpenFile(filepath.Join(results, ".takeout", "state.jsonl"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"id":"torn","sta`)
	f.Close()
	runOK(t, arch, results)
	runOK(t, arch, results)
	sameFiles(t, before, snapshot(t, results))
}

// A resumed run needs room only for what is not done yet.
func TestPendingLeavesOutFinishedFiles(t *testing.T) {
	requireTools(t, "exiftool")
	arch, results := crashTakeout(t)
	runOK(t, arch, results)
	j, err := state.OpenJournal(filepath.Join(results, ".takeout", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	mk := func(id string, folders ...string) group {
		g := group{id: id}
		for _, f := range folders {
			g.members = append(g.members, member{Entry: zipindex.Entry{RelFolder: f, Name: "x.jpg", Size: 1000}})
		}
		return g
	}
	a := lastRecord(t, results, "2019/a.jpg")
	b := lastRecord(t, results, "2019/b.jpg")
	groups := []group{mk(a.ID, "Photos from 2019", "Trip"), mk(b.ID, "Photos from 2019"), mk("new", "Photos from 2019", "Trip")}
	if got := estimate(pending(groups, j, results), false, "copy"); got != 2200 {
		t.Fatalf("need %d bytes for the unfinished file and its album copy, want 2200", got)
	}
	// a.jpg placed but its albums not made yet: only the album copy is needed.
	j.Put(state.Rec{ID: a.ID, SHA: a.SHA, Stage: "placed", Path: a.Path})
	if got := estimate(pending(groups, j, results), false, "copy"); got != 3300 {
		t.Fatalf("need %d, want 3300", got)
	}
}
