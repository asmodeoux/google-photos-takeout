package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
	"github.com/asmodeoux/google-photos-takeout/internal/exiftool/fakeexif"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

func TestMain(m *testing.M) {
	fakeexif.Main()
	os.Exit(m.Run())
}

// fakeTakeout is a small export whose photos carry a caption, so a rule can
// pick one out: writes go to staged files named by hash, but the caption is
// in the write's arguments.
func fakeTakeout(t *testing.T, captions ...string) (arch, results string) {
	t.Helper()
	dir := t.TempDir()
	tk := testgen.New()
	for i, c := range captions {
		side := testgen.Side{Taken: time.Date(2019, 3, 4, 9, 0, i, 0, time.UTC), Description: c}
		tk.Photo(1, "Photos from 2019", c+".jpg", testgen.JPEG(i+1), &side)
	}
	arch = filepath.Join(dir, "archives")
	if _, err := tk.Write(arch); err != nil {
		t.Fatal(err)
	}
	return arch, filepath.Join(dir, "results")
}

func fakeOptions(t *testing.T, r fakeexif.Rules, arch, results string) Options {
	t.Helper()
	bin, _ := fakeexif.Setup(t, r)
	opt := testOptions(arch, results)
	opt.Exiftool = bin
	return opt
}

func TestRunRecoversFromExifToolCrash(t *testing.T) {
	arch, results := fakeTakeout(t, "a", "b", "c")
	code, rep, err := Run(context.Background(), fakeOptions(t, fakeexif.Rules{CrashOn: []int{2}}, arch, results))
	if code != ExitOK {
		t.Fatalf("exit %d: %v %v", code, err, rep.Errors)
	}
	if rep.Retries.ExiftoolRestarts < 1 || rep.TagErrors != 0 {
		t.Fatalf("restarts %d tag errors %d", rep.Retries.ExiftoolRestarts, rep.TagErrors)
	}
	txt, _ := os.ReadFile(filepath.Join(results, ".takeout", "report.txt"))
	if !strings.Contains(string(txt), "exiftool restarted 1 times") {
		t.Errorf("report.txt:\n%s", txt)
	}
	log, _ := os.ReadFile(rep.ExiftoolLog)
	if !strings.Contains(string(log), " crash ") || !strings.Contains(string(log), " restart ") {
		t.Errorf("exiftool.log:\n%s", log)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: results}); vcode != ExitOK {
		t.Fatalf("verify exit %d: %v", vcode, verr)
	}
}

// A file that crashes ExifTool every time is one tag error; the rest of the
// library is tagged.
func TestRunPoisonFileIsOneTagError(t *testing.T) {
	arch, results := fakeTakeout(t, "a", "poison", "c")
	code, rep, err := Run(context.Background(), fakeOptions(t, fakeexif.Rules{Poison: "poison"}, arch, results))
	if code != ExitTagErrors {
		t.Fatalf("exit %d: %v %v", code, err, rep.Errors)
	}
	if rep.TagErrors != 1 || len(rep.TagErrorFiles) != 1 || rep.TagErrorFiles[0].Path != "2019/poison.jpg" ||
		!strings.Contains(rep.TagErrorFiles[0].Stderr, "ExifTool crashed on this file twice") {
		t.Fatalf("tag errors %d %+v", rep.TagErrors, rep.TagErrorFiles)
	}
}

// An ExifTool that crashes on every command stops the run with exit 2 and a
// fix, not a "usable" library full of tag errors; after the fix the same
// command resumes.
func TestRunBrokenExifToolStopsWithFix(t *testing.T) {
	arch, results := fakeTakeout(t, "a", "b")
	opt := fakeOptions(t, fakeexif.Rules{CrashAll: true}, arch, results)
	code, _, err := Run(context.Background(), opt)
	var rs *RuntimeStopError
	if code != ExitPreflight || !errors.As(err, &rs) {
		t.Fatalf("exit %d: %v", code, err)
	}
	msg := err.Error()
	if !strings.Contains(msg, "ExifTool failed on a test photo") || !strings.Contains(msg, opt.Exiftool+" -ver") ||
		!strings.Contains(msg, "See: README.md#exiftool-keeps-crashing") {
		t.Fatalf("message:\n%s", msg)
	}
	opt.Exiftool = ""
	if code, rep, err := Run(context.Background(), opt); code != ExitOK {
		t.Fatalf("resume exit %d: %v %v", code, err, rep.Errors)
	}
}

// The pool can also give up while reading dates back; that is exit 2 too.
func TestRunPoolBreaksDuringReadBack(t *testing.T) {
	tunePool = func(o *exiftool.PoolOptions) { o.MaxCrashRun = 1 }
	defer func() { tunePool = nil }()
	arch, results := fakeTakeout(t, "a", "b")
	sep := string(filepath.Separator)
	opt := fakeOptions(t, fakeexif.Rules{Poison: sep + "2019" + sep + "|probe.jpg"}, arch, results)
	code, _, err := Run(context.Background(), opt)
	if code != ExitPreflight || !errors.Is(err, exiftool.ErrPoolBroken) {
		t.Fatalf("exit %d: %v", code, err)
	}
}

func TestVerifyBrokenExifToolExitsTwo(t *testing.T) {
	arch, results := fakeTakeout(t, "a")
	if code, _, err := Run(context.Background(), testOptions(arch, results)); code != ExitOK {
		t.Fatalf("run exit %d: %v", code, err)
	}
	tunePool = func(o *exiftool.PoolOptions) { o.MaxCrashRun = 1 }
	defer func() { tunePool = nil }()
	bin, _ := fakeexif.Setup(t, fakeexif.Rules{CrashAll: true})
	code, _, err := Verify(context.Background(), Options{Results: results, Exiftool: bin})
	if code != ExitPreflight || !errors.Is(err, exiftool.ErrPoolBroken) {
		t.Fatalf("verify exit %d: %v", code, err)
	}
}

// A results folder the probe cannot write to is blamed on the disk, not on
// ExifTool.
func TestProbeBlamesTheDisk(t *testing.T) {
	results := t.TempDir()
	if err := os.WriteFile(filepath.Join(results, ".takeout"), []byte("not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := probe(results)(func([]string) (exiftool.Reply, error) { return exiftool.Reply{}, nil })
	msg := stopError(&exiftool.BrokenError{ProbeErr: err}, "/bin/exiftool", results).Error()
	if !strings.Contains(msg, "cannot write a test photo in the results folder") || !strings.Contains(msg, "#disk-space") {
		t.Fatalf("message:\n%s", msg)
	}
}

// A file whose own tags ExifTool cannot read, even on a second try, is
// counted in read_errors under its Takeout name, and its tags are not
// written: the camera's date would be replaced by the sidecar's. A rerun
// with a working ExifTool finishes it.
func TestRunUnreadableFileKeepsItsTags(t *testing.T) {
	arch, results := fakeTakeout(t, "a", "b")
	sum := sha256.Sum256(testgen.JPEG(1)) // a.jpg; its staged copy is named by this
	opt := fakeOptions(t, fakeexif.Rules{Poison: hex.EncodeToString(sum[:])}, arch, results)
	code, rep, err := Run(context.Background(), opt)
	if code != ExitTagErrors {
		t.Fatalf("exit %d: %v %v", code, err, rep.Errors)
	}
	if rep.ReadErrors != 1 || len(rep.ReadErrorFiles) != 1 || !strings.HasPrefix(rep.ReadErrorFiles[0], "Photos from 2019/a.jpg: ") {
		t.Fatalf("read errors %d %q", rep.ReadErrors, rep.ReadErrorFiles)
	}
	if rep.TagErrors != 1 || !strings.Contains(strings.Join(rep.Errors, "\n"), "tags not written") {
		t.Fatalf("tag errors %d %q", rep.TagErrors, rep.Errors)
	}
	opt.Exiftool = ""
	if code, rep, err := Run(context.Background(), opt); code != ExitOK || rep.TagErrors != 0 {
		t.Fatalf("rerun exit %d: %v %v", code, err, rep.Errors)
	}
}

// verify that cannot read a library file says so with exit 2 and names the
// file; it is not a photo in the wrong year folder (exit 3).
func TestVerifyUnreadableFileIsNotAWrongYear(t *testing.T) {
	arch, results := fakeTakeout(t, "a", "b")
	if code, rep, err := Run(context.Background(), testOptions(arch, results)); code != ExitOK {
		t.Fatalf("run exit %d: %v %v", code, err, rep.Errors)
	}
	bin, _ := fakeexif.Setup(t, fakeexif.Rules{Poison: "a.jpg"})
	code, _, err := Verify(context.Background(), Options{Results: results, Exiftool: bin})
	var rs *RuntimeStopError
	if code != ExitPreflight || !errors.As(err, &rs) {
		t.Fatalf("exit %d: %v", code, err)
	}
	if msg := err.Error(); !strings.Contains(msg, "could not read 1 library files") || !strings.Contains(msg, "a.jpg (") {
		t.Fatalf("message:\n%s", msg)
	}
}

// A file whose date cannot be read back after the write is a tag error that
// says why, not "date did not read back" with empty values.
func TestRunReadBackFailureSaysWhy(t *testing.T) {
	arch, results := fakeTakeout(t, "a", "b")
	code, rep, err := Run(context.Background(), fakeOptions(t, fakeexif.Rules{Poison: "a.jpg"}, arch, results))
	if code != ExitTagErrors {
		t.Fatalf("exit %d: %v %v", code, err, rep.Errors)
	}
	if len(rep.TagErrorFiles) != 1 || !strings.HasPrefix(rep.TagErrorFiles[0].Stderr, "date could not be read back: ") {
		t.Fatalf("tag error files %+v", rep.TagErrorFiles)
	}
}
