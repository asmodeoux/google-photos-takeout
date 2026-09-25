package pipeline

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asmodeoux/google-photos-takeout/internal/names"
	"github.com/asmodeoux/google-photos-takeout/internal/state"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

func openTestJournal(t *testing.T, results string) *state.Journal {
	t.Helper()
	j, err := state.OpenJournal(filepath.Join(results, ".takeout", "state.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j
}

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPlacerNeverOverwritesOnResume(t *testing.T) {
	results := t.TempDir()
	write(t, filepath.Join(results, "2019", "x.jpg"), "first")
	write(t, filepath.Join(results, "2019", "x (2).jpg"), "second")
	pl := newPlacer(results, openTestJournal(t, results))
	src := filepath.Join(results, "staged")
	write(t, src, "third")
	rel, err := pl.moveInto(src, "2019", "x.jpg", "")
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join("2019", "x (3).jpg") {
		t.Fatalf("rel %s", rel)
	}
	for name, want := range map[string]string{"x.jpg": "first", "x (2).jpg": "second", "x (3).jpg": "third"} {
		b, _ := os.ReadFile(filepath.Join(results, "2019", name))
		if string(b) != want {
			t.Fatalf("%s = %q, want %q", name, b, want)
		}
	}
}

func TestPlacerSeedsFromJournalIgnoringCase(t *testing.T) {
	results := t.TempDir()
	j := openTestJournal(t, results)
	j.Put(state.Rec{ID: "a", Stage: "cloned", Path: "2019/IMG.jpg", Albums: []string{"albums/Trip/IMG.jpg"}})
	pl := newPlacer(results, j)
	if got := pl.pick("2019", "img.JPG", ""); got != filepath.Join("2019", "img (2).JPG") {
		t.Fatalf("library pick %s", got)
	}
	if got := pl.pick(filepath.Join("albums", "Trip"), "IMG.jpg", ""); got != filepath.Join("albums", "Trip", "IMG (2).jpg") {
		t.Fatalf("album pick %s", got)
	}
}

func TestPlacerKeepsPairNumbersTogether(t *testing.T) {
	results := t.TempDir()
	write(t, filepath.Join(results, "2020", "IMG_1.heic"), "other")
	pl := newPlacer(results, openTestJournal(t, results))
	still := pl.pick("2020", "IMG_1.heic", "pair1")
	pl.take(still)
	video := pl.pick("2020", "IMG_1.MOV", "pair1")
	if still != filepath.Join("2020", "IMG_1 (2).heic") || video != filepath.Join("2020", "IMG_1 (2).MOV") {
		t.Fatalf("still %s video %s", still, video)
	}
}

func TestAlbumDirsCollisions(t *testing.T) {
	mk := func(folder string) group {
		return group{members: []member{{Entry: zipindex.Entry{RelFolder: folder, Name: "a.jpg"}}}}
	}
	groups := []group{mk("Trip"), mk("trip"), mk("A?"), mk("A*"), mk("CON"), mk("Plain")}
	dirs, renames := albumDirs(groups, names.Portable)
	want := map[string]string{"A*": "A-", "A?": "A- (2)", "Trip": "Trip", "trip": "trip (2)", "CON": "CON_", "Plain": "Plain"}
	for k, v := range want {
		if dirs[k] != v {
			t.Errorf("%s -> %q, want %q", k, dirs[k], v)
		}
	}
	if len(renames) != 4 {
		t.Fatalf("renames %v", renames)
	}
	apple, _ := albumDirs(groups, names.Apple)
	if apple["A?"] != "A?" || apple["trip"] != "trip (2)" {
		t.Fatalf("apple %v", apple)
	}
}

func TestAlbumFilesThatCollideAreAllKept(t *testing.T) {
	results := t.TempDir()
	j := openTestJournal(t, results)
	pl := newPlacer(results, j)
	var groups []group
	for i, name := range []string{"a?.jpg", "a*.jpg"} {
		lib := filepath.Join("2019", "lib"+string(rune('0'+i))+".jpg")
		write(t, filepath.Join(results, lib), name)
		groups = append(groups, group{
			id: name, outRel: lib, trueType: "jpeg",
			members: []member{{Entry: zipindex.Entry{RelFolder: "Trip", Name: name}}},
		})
	}
	dirs, _ := albumDirs(groups, names.Portable)
	for i := range groups {
		if err := albums(Options{Results: results, Albums: "copy"}, groups, i, dirs, pl, names.Portable, j); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{"a-.jpg": "a?.jpg", "a- (2).jpg": "a*.jpg"} {
		b, err := os.ReadFile(filepath.Join(results, "albums", "Trip", name))
		if err != nil || string(b) != want {
			t.Fatalf("%s: %q %v", name, b, err)
		}
	}
	rec, _ := j.Get("a*.jpg")
	if rec.Stage != "cloned" || len(rec.Albums) != 1 || strings.Contains(rec.Albums[0], `\`) {
		t.Fatalf("journal %+v", rec)
	}
}

func TestUnzipRelPortableAndUnique(t *testing.T) {
	taken := map[string]bool{}
	a := unzipRel("Takeout/Google Photos/Trip?/a?.jpg", "file", taken)
	b := unzipRel("Takeout/Google Photos/Trip*/a*.jpg", "file", taken)
	c := unzipRel("Takeout/Google Photos/CON/CON.jpg", "file", taken)
	if a != filepath.FromSlash("Takeout/Google Photos/Trip-/a-.jpg") || b != filepath.FromSlash("Takeout/Google Photos/Trip-/a- (2).jpg") {
		t.Fatalf("%s %s", a, b)
	}
	if c != filepath.FromSlash("Takeout/Google Photos/CON_/CON_.jpg") {
		t.Fatal(c)
	}
}

func TestUnzipSkipsSymlinksAndSystemFiles(t *testing.T) {
	dir := t.TempDir()
	zips, err := testgen.Corpus().Write(filepath.Join(dir, "archives"))
	if err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "unzipped")
	if err := unzipAll(context.Background(), zips, dest); err != nil {
		t.Fatal(err)
	}
	var files int
	err = filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			t.Errorf("symlink created: %s", p)
		}
		if d.Name() == "__MACOSX" || (!d.IsDir() && zipindex.IsSystemName(d.Name())) {
			t.Errorf("system file unzipped: %s", p)
		}
		if !d.IsDir() {
			files++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("nothing unzipped")
	}
}
