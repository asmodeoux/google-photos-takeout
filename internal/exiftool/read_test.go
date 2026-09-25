package exiftool

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPathKeyWindows(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Users\me\results\2019\a.jpg`, "c:/users/me/results/2019/a.jpg"},
		{`c:/Users/me/results/2019/a.jpg`, "c:/users/me/results/2019/a.jpg"},
		{`\\?\C:\Users\me\x.jpg`, "c:/users/me/x.jpg"},
		{`\\?\UNC\server\share\x.jpg`, "//server/share/x.jpg"},
		{`\\server\share\dir\..\x.jpg`, "//server/share/x.jpg"},
		{`C:\Users\RUNNER~1\AppData\Local\Temp\x.jpg`, "c:/users/runner~1/appdata/local/temp/x.jpg"},
		{`C:\Фото\Ёлка.JPG`, "c:/фото/ёлка.jpg"},
		{"C:\\cafe\u0301.jpg", "c:/caf\u00e9.jpg"},
	}
	for _, c := range cases {
		if got := pathKey(c.in, true); got != c.want {
			t.Errorf("pathKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// Go's form and ExifTool's echo of the same file must meet.
	if pathKey(`C:\a\Б.jpg`, true) != pathKey(`c:/a/б.jpg`, true) {
		t.Fatal("go and exiftool forms differ")
	}
}

func TestPathKeyUnixKeepsBackslashAndCase(t *testing.T) {
	if got := pathKey(`/r/a\b.jpg`, false); got != `/r/a\b.jpg` {
		t.Fatalf("got %q", got)
	}
	if pathKey("/r/A.jpg", false) == pathKey("/r/a.jpg", false) {
		t.Fatal("case folded on unix")
	}
	if got := pathKey("/r/./x/../cafe\u0301.jpg", false); got != "/r/caf\u00e9.jpg" {
		t.Fatalf("got %q", got)
	}
}

// fakeTool answers each block on stdout and stderr like ExifTool does.
type fakeTool struct {
	inR       *io.PipeReader
	outW      *io.PipeWriter
	errW      *io.PipeWriter
	stderrLag time.Duration
	stdout    func(args []string) string
	stderr    func(args []string) string
}

func startFake(t *testing.T, f *fakeTool) *Client {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	errR, errW := io.Pipe()
	f.inR, f.outW, f.errW = inR, outW, errW
	go func() {
		buf := make([]byte, 0, 4096)
		var args []string
		tmp := make([]byte, 4096)
		for {
			n, err := inR.Read(tmp)
			buf = append(buf, tmp[:n]...)
			for {
				i := strings.IndexByte(string(buf), '\n')
				if i < 0 {
					break
				}
				line := string(buf[:i])
				buf = buf[i+1:]
				if id, ok := strings.CutPrefix(line, "-execute"); ok {
					done := ""
					for j := range args {
						if args[j] == "-echo4" && j+1 < len(args) {
							done = args[j+1]
						}
					}
					out, errText := "", ""
					if f.stdout != nil {
						out = f.stdout(args)
					}
					if f.stderr != nil {
						errText = f.stderr(args)
					}
					io.WriteString(outW, out+"{ready"+id+"}\n")
					go func(e, d string) {
						time.Sleep(f.stderrLag)
						io.WriteString(errW, e+d+"\n")
					}(errText, done)
					args = nil
					continue
				}
				args = append(args, line)
			}
			if err != nil {
				outW.Close()
				errW.Close()
				return
			}
		}
	}()
	return newClient(inW, outR, errR)
}

func TestRunWaitsForBothMarkers(t *testing.T) {
	f := &fakeTool{
		stderrLag: 100 * time.Millisecond,
		stdout:    func(a []string) string { return "    1 image files updated\n" },
		stderr:    func(a []string) string { return "Warning: slow disk\n" },
	}
	c := startFake(t, f)
	r, err := c.Run([]string{"-m", "/tmp/a.jpg"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !Updated(r.Out) || !strings.Contains(r.Err, "slow disk") {
		t.Fatalf("%+v", r)
	}
	// The second block must not see the first block's stderr.
	f.stderr = func(a []string) string { return "" }
	r, err = c.Run([]string{"-m", "/tmp/b.jpg"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r.Err != "" {
		t.Fatalf("stderr leaked: %q", r.Err)
	}
}

func TestRunLargeReplyAndStderrCap(t *testing.T) {
	big := strings.Repeat("x", 1<<20) + "\n"
	f := &fakeTool{
		stdout: func(a []string) string { return big },
		stderr: func(a []string) string { return strings.Repeat("Error: e\n", 1<<14) },
	}
	c := startFake(t, f)
	r, err := c.Run([]string{"/tmp/a.jpg"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Out) != len(big) {
		t.Fatalf("stdout %d bytes", len(r.Out))
	}
	if len(r.Err) > maxStderr+64 {
		t.Fatalf("stderr not capped: %d", len(r.Err))
	}
}

func requireExiftool(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("exiftool"); err != nil {
		if os.Getenv("TAKEOUT_REQUIRE_TOOLS") == "1" {
			t.Fatal("exiftool is required (TAKEOUT_REQUIRE_TOOLS=1)")
		}
		t.Skip("exiftool not installed")
	}
}

func TestReadJSONNonASCIIAndMissing(t *testing.T) {
	requireExiftool(t)
	dir := filepath.Join(t.TempDir(), "Фото café 🌅")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "Ёлка é.jpg")
	if err := os.WriteFile(good, tinyJPEG, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := Start("")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	w, err := c.Run([]string{"-m", "-overwrite_original", "-ExifIFD:DateTimeOriginal=2019:06:06 14:23:31", good}, 1)
	if err != nil || !Updated(w.Out) {
		t.Fatalf("write: %v %+v", err, w)
	}
	rows, errs := ReadAll([]*Client{c}, []string{good, filepath.Join(dir, "missing.jpg")}, []string{"DateTimeOriginal"}, false)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	row, ok := rows[PathKey(good)]
	if !ok {
		t.Fatalf("no row for %s in %v", good, rows)
	}
	if row["DateTimeOriginal"] != "2019:06:06 14:23:31" {
		t.Fatalf("row %v", row)
	}
	if len(rows) != 1 {
		t.Fatalf("rows %v", rows)
	}
}

// tinyJPEG is a valid 1x1 JPEG with no metadata.
var tinyJPEG = []byte{
	0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07,
	0x07, 0x09, 0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12, 0x13, 0x0f,
	0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20, 0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c,
	0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29, 0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27, 0x39, 0x3d,
	0x38, 0x32, 0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01, 0x00, 0x01,
	0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x14, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09, 0xff, 0xc4, 0x00, 0x14, 0x10, 0x01,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00, 0x2a, 0x9f, 0xff, 0xd9,
}

func TestAtLeast(t *testing.T) {
	cases := []struct {
		have, want string
		ok         bool
	}{
		{"13.07", "13.07", true}, {"13.25", "13.07", true}, {"13.06", "13.07", false},
		{"12.99", "13.07", false}, {"9.5", "13.07", false}, {"14.00", "13.07", true},
		{" 13.10\n", "13.07", true},
	}
	for _, c := range cases {
		if AtLeast(c.have, c.want) != c.ok {
			t.Errorf("AtLeast(%q, %q) != %v", c.have, c.want, c.ok)
		}
	}
}

func TestLookRejectsKeypressBuild(t *testing.T) {
	_, err := look(`C:\Downloads\exiftool(-k).exe`, "windows")
	if err == nil || !strings.Contains(err.Error(), "Rename it to exiftool.exe") {
		t.Fatalf("err %v", err)
	}
	_, err = look(filepath.Join(t.TempDir(), "nope"), "windows")
	if err == nil || !strings.Contains(err.Error(), "winget install") {
		t.Fatalf("err %v", err)
	}
}
