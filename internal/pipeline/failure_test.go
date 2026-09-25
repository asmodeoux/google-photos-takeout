package pipeline

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/state"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

// writeTakeout writes one zip part with photos under "Photos from 2019", each
// with a sidecar, and returns the archives folder.
func writeTakeout(t *testing.T, dir string, files map[string][]byte) string {
	t.Helper()
	tk := testgen.New()
	for name, data := range files {
		side := testgen.Side{Taken: time.Date(2019, 3, 4, 9, 0, 0, 0, time.UTC)}
		tk.Photo(1, "Photos from 2019", name, data, &side)
	}
	arch := filepath.Join(dir, "archives")
	if _, err := tk.Write(arch); err != nil {
		t.Fatal(err)
	}
	return arch
}

// corruptEntry flips a byte inside one entry's data, so reading it fails the
// CRC check the way a damaged download does.
func corruptEntry(t *testing.T, arch, name string) {
	t.Helper()
	zips, _ := filepath.Glob(filepath.Join(arch, "*.zip"))
	for _, z := range zips {
		r, err := zip.OpenReader(z)
		if err != nil {
			t.Fatal(err)
		}
		var off int64 = -1
		var size uint64
		for _, f := range r.File {
			if strings.HasSuffix(f.Name, "/"+name) {
				off, _ = f.DataOffset()
				size = f.CompressedSize64
			}
		}
		r.Close()
		if off < 0 {
			continue
		}
		b, err := os.ReadFile(z)
		if err != nil {
			t.Fatal(err)
		}
		b[off+int64(size/2)] ^= 0xff
		if err := os.WriteFile(z, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatalf("entry %s not found", name)
}

func testOptions(arch, results string) Options {
	return Options{Archives: arch, Results: results, Albums: "copy", DefaultTZ: "UTC", Progress: "plain",
		Quiet: true, Stdout: &bytes.Buffer{}, Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

// A damaged zip entry must not disappear behind a successful exit code.
func TestDamagedEntryFailsRunAndVerify(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	arch := writeTakeout(t, dir, map[string][]byte{"good.jpg": testgen.JPEG(1), "bad.jpg": testgen.JPEG(2)})
	corruptEntry(t, arch, "bad.jpg")
	results := filepath.Join(dir, "results")

	code, rep, err := Run(context.Background(), testOptions(arch, results))
	if code != ExitReconcile || err == nil {
		t.Fatalf("run exit %d (%v), want %d", code, err, ExitReconcile)
	}
	if rep.Failed != 1 || rep.Library != 1 || len(rep.FailedFiles) != 1 || !strings.HasSuffix(rep.FailedFiles[0].Entry, "/bad.jpg") {
		t.Fatalf("failed %d library %d files %+v", rep.Failed, rep.Library, rep.FailedFiles)
	}
	if _, err := os.Stat(filepath.Join(results, "2019", "good.jpg")); err != nil {
		t.Errorf("the good file was not placed: %v", err)
	}
	if vcode, _, _ := Verify(context.Background(), Options{Results: results}); vcode != ExitReconcile {
		t.Errorf("verify exit %d, want %d", vcode, ExitReconcile)
	}
}

// A WebM placed in not-importable/ by a run without ffmpeg must survive a
// later run that has ffmpeg.
func TestPlacedWebMSurvivesRerunWithFFmpeg(t *testing.T) {
	requireTools(t, "exiftool", "ffmpeg")
	dir := t.TempDir()
	arch := writeTakeout(t, dir, map[string][]byte{"clip.webm": testgen.Fixture("clip1.webm")})
	results := filepath.Join(dir, "results")

	opt := testOptions(arch, results)
	opt.FFmpeg = filepath.Join(dir, "no-ffmpeg")
	if code, rep, err := Run(context.Background(), opt); code != ExitOK {
		t.Fatalf("first run exit %d: %v %v", code, err, rep.Errors)
	}
	placed := filepath.Join(results, "not-importable", "clip.webm")
	if _, err := os.Stat(placed); err != nil {
		t.Fatalf("without ffmpeg the WebM should be kept as %s: %v", placed, err)
	}

	if code, rep, err := Run(context.Background(), testOptions(arch, results)); code != ExitOK {
		t.Fatalf("second run exit %d: %v %v", code, err, rep.Errors)
	}
	if _, err := os.Stat(placed); err != nil {
		t.Fatalf("the rerun removed %s", placed)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: results}); vcode != ExitOK {
		t.Fatalf("verify exit %d: %v", vcode, verr)
	}
}

// After an interrupted run, verify must not pass on the previous run's report.
func TestVerifyAfterInterruptedRunIsNotOK(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	arch := writeTakeout(t, dir, map[string][]byte{"a.jpg": testgen.JPEG(1), "b.jpg": testgen.JPEG(2)})
	results := filepath.Join(dir, "results")
	if code, _, err := Run(context.Background(), testOptions(arch, results)); code != ExitOK {
		t.Fatalf("run exit %d: %v", code, err)
	}
	opt := testOptions(arch, results)
	opt.FailAfter = 1
	if code, _, _ := Run(context.Background(), opt); code != ExitInterrupt {
		t.Fatalf("interrupted run exit %d", code)
	}
	if vcode, _, _ := Verify(context.Background(), Options{Results: results}); vcode == ExitOK {
		t.Fatal("verify passed on a stale report")
	}
}

// A folder name with glob characters, and .ZIP in capitals, still find the zips.
func TestArchivesFolderWithBracketsAndUpperCaseZip(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	arch := writeTakeout(t, dir, map[string][]byte{"a.jpg": testgen.JPEG(1)})
	odd := filepath.Join(dir, "Takeout [2024]*?")
	if runtime.GOOS == "windows" {
		odd = filepath.Join(dir, "Takeout [2024]")
	}
	if err := os.Rename(arch, odd); err != nil {
		t.Fatal(err)
	}
	zips, _ := os.ReadDir(odd)
	for _, z := range zips {
		os.Rename(filepath.Join(odd, z.Name()), filepath.Join(odd, strings.TrimSuffix(z.Name(), ".zip")+".ZIP"))
	}
	results := filepath.Join(dir, "results")
	code, rep, err := Run(context.Background(), testOptions(odd, results))
	if code != ExitOK || rep.Library != 1 {
		t.Fatalf("exit %d library %d: %v", code, rep.Library, err)
	}
}

// verify is the success check users and CI rely on; each way a library can be
// wrong must give its own exit code.
func TestVerifyFailurePaths(t *testing.T) {
	requireTools(t, "exiftool")
	cases := []struct {
		name   string
		break_ func(t *testing.T, results string)
		want   int
	}{
		{"intact", func(t *testing.T, results string) {}, ExitOK},
		{"library file deleted", func(t *testing.T, results string) {
			os.Remove(filepath.Join(results, "2019", "a.jpg"))
		}, ExitReconcile},
		{"photo in the wrong year folder", func(t *testing.T, results string) {
			os.MkdirAll(filepath.Join(results, "2018"), 0o755)
			b, _ := os.ReadFile(filepath.Join(results, "2019", "a.jpg"))
			os.WriteFile(filepath.Join(results, "2018", "stray.jpg"), b, 0o644)
		}, ExitReconcile},
		{"tag errors in the report", func(t *testing.T, results string) {
			editReport(t, results, func(m map[string]any) { m["tag_errors"] = 1 })
		}, ExitTagErrors},
		{"failed files in the report", func(t *testing.T, results string) {
			editReport(t, results, func(m map[string]any) { m["failed"] = 1 })
		}, ExitReconcile},
		{"album copy deleted", func(t *testing.T, results string) {
			os.RemoveAll(filepath.Join(results, "albums"))
		}, ExitReconcile},
		{"leftover intent line for a finished file", func(t *testing.T, results string) {
			rec := lastRecord(t, results, "2019/a.jpg")
			appendRecord(t, results, state.Rec{ID: rec.ID, SHA: rec.SHA, Stage: "placing", Path: "2019/never-made.jpg"})
			appendRecord(t, results, rec)
		}, ExitOK},
		{"no report", func(t *testing.T, results string) {
			os.Remove(filepath.Join(results, ".takeout", "report.json"))
		}, ExitPreflight},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			tk := testgen.New()
			side := &testgen.Side{Taken: time.Date(2019, 3, 4, 9, 0, 0, 0, time.UTC)}
			tk.Photo(1, "Photos from 2019", "a.jpg", testgen.JPEG(1), side)
			tk.Photo(1, "Photos from 2019", "b.jpg", testgen.JPEG(2), side)
			tk.Photo(1, "Trip", "a.jpg", testgen.JPEG(1), side)
			arch := filepath.Join(dir, "archives")
			if _, err := tk.Write(arch); err != nil {
				t.Fatal(err)
			}
			results := filepath.Join(dir, "results")
			if code, _, err := Run(context.Background(), testOptions(arch, results)); code != ExitOK {
				t.Fatalf("run exit %d: %v", code, err)
			}
			c.break_(t, results)
			if code, _, err := Verify(context.Background(), Options{Results: results}); code != c.want {
				t.Fatalf("verify exit %d (%v), want %d", code, err, c.want)
			}
		})
	}
}

func editReport(t *testing.T, results string, edit func(map[string]any)) {
	t.Helper()
	p := filepath.Join(results, ".takeout", "report.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	b, _ = json.Marshal(m)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A file ExifTool cannot write is placed, counted once, listed with its
// message, and makes both run and verify exit 4.
func TestTagErrorExitsFourAndIsListed(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	broken := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00garbage-bytes-here")
	arch := writeTakeout(t, dir, map[string][]byte{"good.jpg": testgen.JPEG(1), "broken.jpg": broken})
	results := filepath.Join(dir, "results")
	code, rep, err := Run(context.Background(), testOptions(arch, results))
	if code != ExitTagErrors {
		t.Fatalf("run exit %d (%v), want %d; errors %v", code, err, ExitTagErrors, rep.Errors)
	}
	if rep.TagErrors != 1 || len(rep.TagErrorFiles) != 1 || rep.TagErrorFiles[0].Path != "2019/broken.jpg" ||
		!strings.Contains(rep.TagErrorFiles[0].Stderr, "Corrupted") {
		t.Fatalf("tag errors %d, files %+v", rep.TagErrors, rep.TagErrorFiles)
	}
	if rep.Library != 2 || rep.Failed != 0 {
		t.Fatalf("library %d failed %d", rep.Library, rep.Failed)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: results}); vcode != ExitTagErrors {
		t.Fatalf("verify exit %d (%v), want %d", vcode, verr, ExitTagErrors)
	}
}

// With more tag errors than the report lists by name, verify still reports
// exit 4, not a wrong year folder for the ones past the list.
func TestManyTagErrorsVerifyAsFour(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	files := map[string][]byte{"good.jpg": testgen.JPEG(1)}
	for i := 0; i < maxTagErrorFiles+5; i++ {
		files[fmt.Sprintf("broken%02d.jpg", i)] = []byte(fmt.Sprintf("\xff\xd8\xff\xe0\x00\x10JFIF\x00broken-%d", i))
	}
	arch := writeTakeout(t, dir, files)
	results := filepath.Join(dir, "results")
	code, rep, _ := Run(context.Background(), testOptions(arch, results))
	if code != ExitTagErrors || rep.TagErrors != maxTagErrorFiles+5 {
		t.Fatalf("run exit %d, tag errors %d", code, rep.TagErrors)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: results}); vcode != ExitTagErrors {
		t.Fatalf("verify exit %d (%v), want %d", vcode, verr, ExitTagErrors)
	}
}
