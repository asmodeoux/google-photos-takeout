//go:build !windows

package exiftool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestExifToolMemoryGrowth measures whether one stay_open process grows over
// many writes, which decides whether processes need recycling. It is opt-in:
//
//	TAKEOUT_MEASURE_RSS=1 go test ./internal/exiftool -run MemoryGrowth -v -timeout 30m
//
// It writes the pipeline's date and GPS tags 20,000 times over 200 files and
// logs the process's resident size every 1,000 commands.
func TestExifToolMemoryGrowth(t *testing.T) {
	if os.Getenv("TAKEOUT_MEASURE_RSS") != "1" {
		t.Skip("set TAKEOUT_MEASURE_RSS=1 to measure")
	}
	requireExiftool(t)
	c, err := Start("")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	dir := t.TempDir()
	var files []string
	for i := range 200 {
		p := filepath.Join(dir, strconv.Itoa(i)+".jpg")
		if err := os.WriteFile(p, tinyJPEG, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, p)
	}
	var first int
	for i := 1; i <= 20000; i++ {
		args := []string{"-m", "-overwrite_original",
			"-ExifIFD:DateTimeOriginal=2019:06:06 14:23:31", "-ExifIFD:OffsetTimeOriginal=+03:00",
			"-GPSLatitude=55.7558", "-GPSLatitudeRef=N", "-GPSLongitude=37.6173", "-GPSLongitudeRef=E",
			files[i%len(files)]}
		if r, err := c.Run(args, i); err != nil || !Updated(r.Out) {
			t.Fatalf("write %d: %v %+v", i, err, r)
		}
		if i%1000 == 0 {
			rss := rssKB(t, c.Pid())
			if i == 1000 {
				first = rss
			}
			t.Logf("after %5d writes: %d KB (%+.0f%% since 1,000)", i, rss, 100*float64(rss-first)/float64(first))
		}
	}
}

// rssKB is the resident size of pid, from ps.
func rssKB(t *testing.T, pid int) int {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n
}
