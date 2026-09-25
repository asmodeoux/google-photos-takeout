package pipeline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
	"github.com/asmodeoux/google-photos-takeout/internal/state"
)

func TestPreflightErrorsHaveProblemFixAndSee(t *testing.T) {
	for _, goos := range []string{"windows", "darwin", "linux"} {
		cases := map[string]error{
			"no zips":       errNoZips("archives", DefaultLauncher(goos), goos),
			"missing parts": errMissingParts("export 20240101T000000Z is missing part(s) [2]"),
			"exiftool":      errExiftool(&exiftool.LookError{Problem: "ExifTool not found", Where: "PATH", Fix: exiftool.InstallHint(goos)}),
			"exiftool run":  errExiftool(errors.New("exit status 1")),
			"exiftool old":  errExiftoolOld("12.40", goos),
			"disk":          errDiskSpace(40, 10, "results", "ntfs", goos),
			"fat":           errFAT(2, "results", "fat32"),
			"names":         errNames(errors.New("apple-style names cannot be written to ntfs")),
			"locked":        errLocked(state.ErrLocked),
			"root":          checkRoot("results", "res\nults"),
		}
		for name, err := range cases {
			var pe *PreflightError
			if !errors.As(err, &pe) {
				t.Fatalf("%s/%s: not a PreflightError: %v", goos, name, err)
			}
			lines := strings.Split(err.Error(), "\n")
			if len(lines) != 3 {
				t.Fatalf("%s/%s: want 3 lines, got %q", goos, name, err.Error())
			}
			if pe.Value == "" || !strings.Contains(lines[0], pe.Value) {
				t.Errorf("%s/%s: first line lacks the value found: %q", goos, name, lines[0])
			}
			if !strings.HasPrefix(lines[1], "Fix: ") || len(lines[1]) < 10 {
				t.Errorf("%s/%s: fix line %q", goos, name, lines[1])
			}
			if !strings.HasPrefix(lines[2], "See: README.md#") || pe.Anchor == "" {
				t.Errorf("%s/%s: see line %q", goos, name, lines[2])
			}
		}
	}
	if s := errNoZips("archives", `.\takeout.cmd`, "windows").Error(); !strings.Contains(s, `.\takeout.cmd check --archives "D:\Takeout"`) {
		t.Errorf("windows no-zips fix: %s", s)
	}
	if s := errNoZips("archives", "./takeout.sh", "darwin").Error(); !strings.Contains(s, `./takeout.sh check --archives "/Volumes/Disk/Takeout"`) {
		t.Errorf("macOS no-zips fix: %s", s)
	}
	if !errors.Is(errLocked(state.ErrLocked), state.ErrLocked) {
		t.Error("errLocked must wrap state.ErrLocked")
	}
	if checkRoot("results", "D:\\Фото results") != nil {
		t.Error("non-ASCII folder rejected")
	}
}

// Every "See: README.md#x" line must land on an <a id="x"> in the README.
func TestReadmeHasEveryAnchor(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	anchors := map[string]bool{"antivirus": true}
	for _, goos := range []string{"windows", "darwin", "linux"} {
		for _, err := range []error{
			errNoZips("a", "x", goos), errMissingParts("p"), errExiftool(errors.New("e")), errExiftoolOld("1", goos),
			errDiskSpace(1, 0, "r", "ntfs", goos), errFAT(1, "r", "fat32"), errNames(errors.New("n")),
			errLocked(state.ErrLocked), checkRoot("results", "a\nb"),
		} {
			var pe *PreflightError
			if errors.As(err, &pe) {
				anchors[pe.Anchor] = true
			}
		}
	}
	anchors["verify"] = true
	for a := range anchors {
		if !strings.Contains(string(readme), `<a id="`+a+`"></a>`) {
			t.Errorf("README.md has no anchor %q", a)
		}
	}
}
