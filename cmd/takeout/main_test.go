package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/asmodeoux/google-photos-takeout/internal/pipeline"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

func TestCommandsAcceptOnlyTheirFlags(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run("doctor", []string{"--archives", "x"}, &out, &errOut); code != pipeline.ExitPreflight {
		t.Fatalf("doctor --archives: exit %d", code)
	}
	if !strings.Contains(errOut.String(), "-results") || strings.Contains(errOut.String(), "-names") {
		t.Errorf("doctor usage should list only its flags:\n%s", errOut.String())
	}
	errOut.Reset()
	if code := run("run", []string{"-h"}, &out, &errOut); code != 0 {
		t.Fatalf("run -h: exit %d", code)
	}
	for _, f := range []string{"-names", "-default-tz", "-no-keep-awake", "-sample"} {
		if !strings.Contains(errOut.String(), f) {
			t.Errorf("run -h lacks %s", f)
		}
	}
	errOut.Reset()
	run("check", []string{"-h"}, &out, &errOut)
	if strings.Contains(errOut.String(), "-sample") || strings.Contains(errOut.String(), "-no-keep-awake") {
		t.Errorf("check -h lists run-only flags:\n%s", errOut.String())
	}
}

func TestEveryCommandBuildsItsFlags(t *testing.T) {
	for cmd := range commands {
		var buf bytes.Buffer
		fs, _ := flags(cmd, &buf)
		if fs.Name() != cmd {
			t.Errorf("flag set name %s for %s", fs.Name(), cmd)
		}
	}
}

func TestStrayArgumentIsRejected(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run("check", []string{`D:\Takeout`}, &out, &errOut); code != pipeline.ExitPreflight {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errOut.String(), `--archives "D:\Takeout"`) {
		t.Errorf("hint: %s", errOut.String())
	}
}

func TestNextLine(t *testing.T) {
	c := &cli{archives: `D:\Takeout`, results: "results", tz: "Europe/Berlin"}
	if got := nextLine(`.\takeout.cmd`, c); got != `Next: .\takeout.cmd run --archives "D:\Takeout" --default-tz Europe/Berlin` {
		t.Errorf("got %s", got)
	}
	c = &cli{archives: "archives", results: "results"}
	if got := nextLine("./takeout.sh", c); !strings.HasPrefix(got, "Next: ./takeout.sh run --default-tz Area/City\n") {
		t.Errorf("got %s", got)
	}
}

func TestImportPhotosOffMacOSExplains(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS imports")
	}
	var out, errOut bytes.Buffer
	if code := importPhotos("", "results", false, &out, &errOut); code != pipeline.ExitPreflight {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errOut.String(), "needs a Mac") {
		t.Errorf("message: %s", errOut.String())
	}
}

func TestCheckEndsWithNextLine(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		if os.Getenv("TAKEOUT_REQUIRE_TOOLS") == "1" {
			t.Fatal("exiftool is required (TAKEOUT_REQUIRE_TOOLS=1)")
		}
		t.Skip("exiftool not installed")
	}
	dir := t.TempDir()
	arch := filepath.Join(dir, "my zips")
	if _, err := testgen.Corpus().Write(arch); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TAKEOUT_LAUNCHER", "./takeout.sh")
	var out, errOut bytes.Buffer
	code := run("check", []string{"--archives", arch, "--results", filepath.Join(dir, "results"), "--default-tz", "Asia/Tokyo", "--quiet"}, &out, &errOut)
	if code != pipeline.ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	last := lines[len(lines)-1]
	want := `Next: ./takeout.sh run --archives "` + arch + `" --results "` + filepath.Join(dir, "results") + `" --default-tz Asia/Tokyo`
	if last != want {
		t.Errorf("last line\n got %s\nwant %s", last, want)
	}
}
