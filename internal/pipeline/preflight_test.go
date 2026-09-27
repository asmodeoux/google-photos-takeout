package pipeline

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"

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
	for _, be := range []*exiftool.BrokenError{
		{ProbeErr: errors.New("p")}, {ProbeErr: &destError{errors.New("d")}},
		{Window: true, Crashes: 20}, {Window: true, Timeouts: 20}, {ProbeErr: errors.New("s"), Restart: true},
	} {
		var rs *RuntimeStopError
		if !errors.As(stopError(be, "exiftool", "results"), &rs) {
			t.Fatalf("%+v is not a RuntimeStopError", be)
		}
		anchors[rs.Anchor] = true
	}
	anchors["verify"] = true
	// Every anchor written in the code, even one no test case reaches.
	lit := regexp.MustCompile(`Anchor:\s*"([a-z0-9-]+)"|README\.md#([a-z0-9-]+)`)
	for _, root := range []string{"..", filepath.Join("..", "..", "cmd")} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, _ := os.ReadFile(p)
			for _, m := range lit.FindAllStringSubmatch(string(b), -1) {
				anchors[m[1]+m[2]] = true
			}
			return nil
		})
	}

	// A link to an anchor inside a closed <details> lands on a folded
	// block, so anchors live outside them.
	text := string(readme)
	var hidden []bool // hidden[i]: byte i is inside <details>
	depth := 0
	for i := 0; i < len(text); i++ {
		switch {
		case strings.HasPrefix(text[i:], "<details>"):
			depth++
		case strings.HasPrefix(text[i:], "</details>"):
			depth--
			if depth < 0 {
				t.Fatalf("README.md: </details> without <details> at byte %d", i)
			}
		}
		hidden = append(hidden, depth > 0)
	}
	if depth != 0 {
		t.Fatalf("README.md: %d <details> not closed", depth)
	}
	targets := map[string]int{} // anchor -> byte offset
	for _, m := range regexp.MustCompile(`<a id="([^"]+)"></a>`).FindAllStringSubmatchIndex(text, -1) {
		id := text[m[2]:m[3]]
		if hidden[m[0]] {
			t.Errorf("README.md: anchor %q is inside <details>", id)
		}
		targets[id] = m[0]
	}
	for _, m := range regexp.MustCompile(`(?m)^#+ (.+)$`).FindAllStringSubmatchIndex(text, -1) {
		if !hidden[m[0]] {
			targets[slug(text[m[2]:m[3]])] = m[0]
		}
	}
	for a := range anchors {
		if !strings.Contains(text, `<a id="`+a+`"></a>`) {
			t.Errorf("README.md has no anchor %q", a)
		}
	}
	for _, m := range regexp.MustCompile(`\]\(#([^)]+)\)`).FindAllStringSubmatch(text, -1) {
		if _, ok := targets[m[1]]; !ok {
			t.Errorf("README.md links to #%s, which is not a heading or anchor outside <details>", m[1])
		}
	}
}

// slug is GitHub's heading anchor: lower case, punctuation dropped, spaces
// to hyphens.
func slug(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(h)) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}

// The fix line names ExifTool as the user would type it: a Windows path keeps
// single backslashes. Each kind of stop has its own problem line.
func TestStopErrorMessages(t *testing.T) {
	const bin = `C:\Tools\exiftool.exe`
	cases := []struct {
		be   *exiftool.BrokenError
		want string
	}{
		{&exiftool.BrokenError{ProbeErr: errors.New("p")}, "failed on a test photo"},
		{&exiftool.BrokenError{ProbeErr: errors.New("s"), Restart: true}, "could not be started again"},
		{&exiftool.BrokenError{Window: true, Crashes: 20}, "crashed on 20"},
		{&exiftool.BrokenError{Window: true, Timeouts: 20}, "--exiftool-timeout"},
	}
	for _, c := range cases {
		msg := stopError(c.be, bin, "results").Error()
		if !strings.Contains(msg, c.want) {
			t.Errorf("%+v: no %q in\n%s", c.be, c.want, msg)
		}
		if !c.be.Window && !strings.Contains(msg, `"`+bin+` -ver"`) {
			t.Errorf("%+v: fix line does not name %s -ver:\n%s", c.be, bin, msg)
		}
	}
}
