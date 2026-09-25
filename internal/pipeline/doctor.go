package pipeline

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
	"github.com/asmodeoux/google-photos-takeout/internal/media"
)

// doctorProbe is a name with Cyrillic and an accent, to prove that ExifTool
// reads and writes non-ASCII paths on this computer.
const doctorProbe = "probe-Фото-é.jpg"

// Doctor checks that this computer can run takeout. It prints one line per
// check and returns ExitOK when every required check passes, ExitPreflight
// otherwise. It prints tool paths and disk facts, never photo or album names.
func Doctor(opt Options) int {
	out := opt.Stdout
	if out == nil {
		out = os.Stdout
	}
	d := &doctor{out: out}
	d.info("system", runtime.GOOS+"/"+runtime.GOARCH)

	bin, v := d.exiftool(opt.Exiftool)
	d.results(opt.Results, bin, v)
	d.ffmpeg(opt.FFmpeg)
	if runtime.GOOS == "windows" {
		d.info("long paths", longPathsNote())
	}

	if d.failed > 0 {
		fmt.Fprintf(out, "\n%d problem(s). Fix them and run doctor again.\n", d.failed)
		return ExitPreflight
	}
	fmt.Fprintln(out, "\nAll required checks passed.")
	return ExitOK
}

type doctor struct {
	out    io.Writer
	failed int
}

func (d *doctor) ok(what, detail string)   { fmt.Fprintf(d.out, "ok    %-11s %s\n", what, detail) }
func (d *doctor) info(what, detail string) { fmt.Fprintf(d.out, "info  %-11s %s\n", what, detail) }

func (d *doctor) fail(what string, err error) {
	d.failed++
	lines := strings.Split(err.Error(), "\n")
	fmt.Fprintf(d.out, "FAIL  %-11s %s\n", what, lines[0])
	for _, l := range lines[1:] {
		fmt.Fprintf(d.out, "      %-11s %s\n", "", l)
	}
}

func (d *doctor) exiftool(flag string) (string, string) {
	bin, err := exiftool.Look(flag)
	if err != nil {
		d.fail("exiftool", errExiftool(err))
		return "", ""
	}
	v, err := exiftool.Version(bin)
	if err != nil {
		d.fail("exiftool", errExiftool(err))
		return "", ""
	}
	if runtime.GOOS == "windows" && !exiftool.AtLeast(v, exiftool.MinWindowsVersion) {
		d.fail("exiftool", errExiftoolOld(v, runtime.GOOS))
		return "", ""
	}
	d.ok("exiftool", v+"  "+bin)
	return bin, v
}

// results checks that the results folder can be written, then writes a date
// into a probe JPEG with a non-ASCII name and reads it back.
func (d *doctor) results(dir, bin, version string) {
	if dir == "" {
		dir = "results"
	}
	if err := checkRoot("results", dir); err != nil {
		d.fail("results", err)
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	meta := filepath.Join(abs, ".takeout")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		d.fail("results", &PreflightError{Problem: "cannot create the results folder", Value: err.Error(),
			Fix: "pass a folder you can write to with --results", Anchor: "paths"})
		return
	}
	probeDir, err := os.MkdirTemp(meta, "doctor-")
	if err != nil {
		d.fail("results", &PreflightError{Problem: "cannot write in the results folder", Value: err.Error(),
			Fix: "pass a folder you can write to with --results", Anchor: "paths"})
		return
	}
	defer os.RemoveAll(probeDir)
	d.ok("results", abs)
	if fs, err := media.Stat(abs); err == nil {
		d.info("disk", fmt.Sprintf("%s, %d GB free", fs.Type, fs.Free/1e9))
	}
	if bin == "" {
		return
	}
	if err := probeRoundTrip(bin, filepath.Join(probeDir, doctorProbe)); err != nil {
		d.fail("utf-8 paths", &PreflightError{
			Problem: "ExifTool could not write and read back a file with a non-English name",
			Value:   err.Error(),
			Fix:     "install ExifTool " + exiftool.MinWindowsVersion + " or newer and run doctor again",
			Anchor:  "exiftool",
		})
		return
	}
	d.ok("utf-8 paths", "wrote and read back "+doctorProbe)
}

func probeRoundTrip(bin, p string) error {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		return err
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		return err
	}
	c, err := exiftool.Start(bin)
	if err != nil {
		return err
	}
	defer c.Close()
	const want = "2001:02:03 04:05:06"
	if _, err := c.Run([]string{"-m", "-overwrite_original", "-ExifIFD:DateTimeOriginal=" + want, p}, 1); err != nil {
		return err
	}
	rows, err := c.ReadJSON([]string{p}, []string{"DateTimeOriginal"}, false, 2)
	if err != nil {
		return err
	}
	for _, row := range rows {
		src, _ := row["SourceFile"].(string)
		if exiftool.PathKey(src) != exiftool.PathKey(p) {
			continue
		}
		if got, _ := row["DateTimeOriginal"].(string); got == want {
			return nil
		}
		return fmt.Errorf("read back %v", row["DateTimeOriginal"])
	}
	return fmt.Errorf("ExifTool returned no row for the probe file")
}

func (d *doctor) ffmpeg(flag string) {
	bin := flag
	if bin == "" {
		bin = "ffmpeg"
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		d.info("ffmpeg", "not found (optional: without it, WebM and MKV videos go to results/not-importable)")
		return
	}
	line := ""
	if b, err := exec.Command(p, "-hide_banner", "-version").Output(); err == nil {
		line, _, _ = strings.Cut(string(b), "\n")
		line = strings.TrimPrefix(line, "ffmpeg version ")
		if f := strings.Fields(line); len(f) > 0 {
			line = f[0]
		}
	}
	d.ok("ffmpeg", strings.TrimSpace(line+"  "+p))
}
