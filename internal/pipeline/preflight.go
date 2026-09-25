package pipeline

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
)

// PreflightError is a problem found before any file is written (exit 2). It
// prints as three lines: the problem with the value that was found, the fix
// for this kind of system, and the README section that explains it.
type PreflightError struct {
	Problem string
	Value   string
	Fix     string
	Anchor  string // README.md has <a id="Anchor"></a> for each
	Err     error
}

func (e *PreflightError) Error() string {
	var b strings.Builder
	b.WriteString(e.Problem)
	if e.Value != "" {
		b.WriteString(": " + e.Value)
	}
	b.WriteString("\nFix: " + e.Fix)
	b.WriteString("\nSee: README.md#" + e.Anchor)
	return b.String()
}

func (e *PreflightError) Unwrap() error { return e.Err }

// DefaultLauncher is how a user starts takeout when no launcher script set
// TAKEOUT_LAUNCHER.
func DefaultLauncher(goos string) string {
	if goos == "windows" {
		return `.\takeout.exe`
	}
	return "./takeout"
}

// examplePath is a folder on an external disk, written the way this kind of
// system writes paths.
func examplePath(goos string) string {
	switch goos {
	case "windows":
		return `D:\Takeout`
	case "darwin":
		return "/Volumes/Disk/Takeout"
	default:
		return "/media/disk/Takeout"
	}
}

func errNoZips(archives, launcher, goos string) error {
	abs, err := filepath.Abs(archives)
	if err != nil {
		abs = archives
	}
	return &PreflightError{
		Problem: "no Takeout zip files found",
		Value:   abs,
		Fix:     fmt.Sprintf(`put the takeout-*.zip files in that folder, or point to them: %s check --archives "%s"`, launcher, examplePath(goos)),
		Anchor:  "zips",
	}
}

func errMissingParts(detail string) error {
	return &PreflightError{
		Problem: "the export is incomplete",
		Value:   detail,
		Fix:     "download the missing parts again from takeout.google.com (Manage exports) and put them next to the others",
		Anchor:  "missing-parts",
	}
}

func errExiftool(err error) error {
	var le *exiftool.LookError
	if errors.As(err, &le) {
		return &PreflightError{Problem: le.Problem, Value: le.Where, Fix: le.Fix, Anchor: "exiftool", Err: err}
	}
	return &PreflightError{
		Problem: "ExifTool did not run",
		Value:   err.Error(),
		Fix:     "run exiftool -ver in a new terminal; if that fails too, reinstall ExifTool",
		Anchor:  "exiftool",
		Err:     err,
	}
}

func errExiftoolOld(have, goos string) error {
	fix := "install ExifTool " + exiftool.MinWindowsVersion + " or newer from exiftool.org"
	if goos == "windows" {
		fix = "winget upgrade --id OliverBetz.ExifTool -e (then open a new PowerShell window)"
	}
	return &PreflightError{
		Problem: "ExifTool is too old for long and non-English Windows paths (need " + exiftool.MinWindowsVersion + " or newer)",
		Value:   have,
		Fix:     fix,
		Anchor:  "exiftool",
	}
}

func errDiskSpace(needGB, haveGB uint64, dir, fsType string, goos string) error {
	return &PreflightError{
		Problem: "not enough free space",
		Value:   fmt.Sprintf("need about %d GB on %s (%s), have %d GB", needGB, dir, fsType, haveGB),
		Fix:     fmt.Sprintf(`free some space, or write to a bigger disk: --results "%s"`, examplePath(goos)+"-results"),
		Anchor:  "disk-space",
	}
}

func errFAT(n int, dir, fsType string) error {
	return &PreflightError{
		Problem: "files of 4 GiB or more cannot be written to a FAT32 disk",
		Value:   fmt.Sprintf("%d file(s), %s (%s)", n, dir, fsType),
		Fix:     "write to an exFAT, NTFS or APFS disk instead",
		Anchor:  "fat32",
	}
}

func errNames(err error) error {
	return &PreflightError{
		Problem: "the --names choice does not fit this disk",
		Value:   err.Error(),
		Fix:     "use --names auto (the default)",
		Anchor:  "names",
		Err:     err,
	}
}

func errLocked(err error) error {
	return &PreflightError{
		Problem: "another takeout is using the results folder",
		Value:   err.Error(),
		Fix:     "wait for it to finish, or close it; takeout resumes where it stopped",
		Anchor:  "in-use",
		Err:     err,
	}
}

// checkRoot rejects folder names with line breaks or other control characters,
// which would split ExifTool's one-argument-per-line input.
func checkRoot(flag, dir string) error {
	for _, r := range dir {
		if r < 0x20 || r == 0x7f {
			return &PreflightError{
				Problem: "the " + flag + " folder name contains a line break or control character",
				Value:   fmt.Sprintf("%q", dir),
				Fix:     "rename the folder, or pass a different --" + flag,
				Anchor:  "paths",
			}
		}
	}
	return nil
}
