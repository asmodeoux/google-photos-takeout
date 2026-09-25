package pipeline

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

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
