package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
	"github.com/asmodeoux/google-photos-takeout/internal/exiftool/fakeexif"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

var update = flag.Bool("update", false, "rewrite the corpus golden files")

// requireTools skips when ExifTool or ffmpeg is missing, and fails instead
// when TAKEOUT_REQUIRE_TOOLS=1, so CI can never pass by skipping.
func requireTools(t *testing.T, tools ...string) {
	t.Helper()
	for _, tool := range tools {
		if _, err := exec.LookPath(tool); err != nil {
			if os.Getenv("TAKEOUT_REQUIRE_TOOLS") == "1" {
				t.Fatalf("%s is required (TAKEOUT_REQUIRE_TOOLS=1)", tool)
			}
			t.Skipf("%s not installed; set TAKEOUT_REQUIRE_TOOLS=1 to fail instead", tool)
		}
	}
}

// corpusResult is everything about a run that must be the same on every OS.
type corpusResult struct {
	Files  map[string]map[string]string `json:"files"`
	Report map[string]any               `json:"report"`
}

var goldenTags = []string{
	"DateTimeOriginal", "OffsetTimeOriginal", "Keys:CreationDate", "QuickTime:CreateDate",
	"GPSLatitude", "GPSLongitude", "Keys:GPSCoordinates", "ContentIdentifier",
	"ImageDescription", "XMP:DateCreated",
}

func runCorpus(t *testing.T, rule string) corpusResult {
	t.Helper()
	res, _ := runCorpusWith(t, rule, nil)
	return res
}

// runCorpusWith runs the corpus with options changed by tune, and checks the
// report's arithmetic: every unique file is in exactly one outcome.
func runCorpusWith(t *testing.T, rule string, tune func(*Options)) (corpusResult, Report) {
	t.Helper()
	dir := t.TempDir()
	arch := filepath.Join(dir, "archives")
	// CI points this at another drive so archives and results are on
	// different volumes, as when reading zips from an external disk.
	if base := os.Getenv("TAKEOUT_CORPUS_ARCHIVES"); base != "" {
		if err := os.MkdirAll(base, 0o755); err != nil {
			t.Fatal(err)
		}
		d, err := os.MkdirTemp(base, "archives-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(d) })
		arch = d
		t.Logf("archives %s, results %s", arch, dir)
	}
	results := filepath.Join(dir, "results")
	if _, err := testgen.Corpus().Write(arch); err != nil {
		t.Fatal(err)
	}
	opt := Options{
		Archives: arch, Results: results, Albums: "copy", Names: rule,
		DefaultTZ: "Europe/Berlin", Progress: "plain", Quiet: true, Stdout: &bytes.Buffer{},
		Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if tune != nil {
		tune(&opt)
	}
	code, rep, err := Run(context.Background(), opt)
	if code != ExitOK {
		t.Fatalf("run exit %d: %v\nerrors: %v", code, err, rep.Errors)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: results}); vcode != ExitOK {
		t.Fatalf("verify exit %d: %v", vcode, verr)
	}
	// The corpus has one photo in Trash and no failed placeholders.
	trashSkipped := 1
	if opt.IncludeTrash {
		trashSkipped = 0
	}
	if sum := rep.Library + rep.Unknown + rep.NotImportable + rep.Placeholders + rep.Failed + trashSkipped; sum != rep.Unique {
		t.Errorf("unique %d, but library %d + unknown %d + not_importable %d + placeholders %d + failed %d + trash %d = %d",
			rep.Unique, rep.Library, rep.Unknown, rep.NotImportable, rep.Placeholders, rep.Failed, trashSkipped, sum)
	}

	var paths []string
	err = filepath.WalkDir(results, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".takeout" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := exiftool.NewPool("", exiftool.PoolOptions{Size: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	rows, failed, err := pool.ReadAll(paths, goldenTags, true)
	if err != nil || len(failed) > 0 {
		t.Fatal(err, failed)
	}
	sort.Strings(paths)
	ids := map[string]string{}
	files := map[string]map[string]string{}
	for _, p := range paths {
		rel, _ := filepath.Rel(results, p)
		row := rows[exiftool.PathKey(p)]
		tags := map[string]string{}
		for k, v := range row {
			switch k {
			case "SourceFile":
				continue
			case "ContentIdentifier":
				s := fmt.Sprint(v)
				if ids[s] == "" {
					ids[s] = fmt.Sprintf("live-%d", len(ids)+1)
				}
				tags[k] = ids[s]
			case "CreateDate":
				// ExifTool shows QuickTime dates in the computer's zone; compare in UTC.
				tags[k] = fmt.Sprint(v)
				if ts, err := time.Parse("2006:01:02 15:04:05-07:00", fmt.Sprint(v)); err == nil {
					tags[k] = ts.UTC().Format("2006:01:02 15:04:05Z")
				}
			case "GPSLatitude", "GPSLongitude":
				tags[k] = fmt.Sprintf("%.4f", v)
			case "GPSCoordinates":
				var la, lo, al float64
				fmt.Sscan(fmt.Sprint(v), &la, &lo, &al)
				tags[k] = fmt.Sprintf("%.4f %.4f", la, lo)
			default:
				tags[k] = fmt.Sprint(v)
			}
		}
		files[filepath.ToSlash(rel)] = tags
	}
	b, _ := json.Marshal(rep)
	var all map[string]any
	json.Unmarshal(b, &all)
	report := map[string]any{}
	for _, k := range []string{"media", "sidecars", "unique", "library", "unknown", "not_importable", "placeholders", "live_pairs",
		"tag_errors", "formats", "years", "date_sources", "timezone_steps", "names_rule", "album_renames", "extension_fixes",
		"system_files_ignored", "symlinks_skipped", "untagged", "failed", "motion_photos", "screenshots_excluded"} {
		if v, ok := all[k]; ok {
			report[k] = v
		}
	}
	var errList []string
	for _, e := range rep.Errors {
		e = strings.ReplaceAll(e, arch, "<tmp>"+string(filepath.Separator)+"archives")
		e = strings.ReplaceAll(e, dir, "<tmp>")
		errList = append(errList, e)
	}
	sort.Strings(errList)
	report["errors"] = errList
	for rel := range files {
		if strings.HasPrefix(rel, "not-importable/") && strings.HasSuffix(rel, ".json") {
			t.Errorf("a sidecar went to not-importable: %s", rel)
		}
	}
	return corpusResult{Files: files, Report: report}, rep
}

func checkGolden(t *testing.T, name string, got corpusResult) {
	t.Helper()
	path := filepath.Join("testdata", name)
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run: go test ./internal/pipeline -run Corpus -update)", err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), b) {
		out := filepath.Join(t.TempDir(), name)
		os.WriteFile(out, b, 0o644)
		t.Fatalf("corpus result differs from %s; actual result written to %s\n%s", path, out, diffLines(string(want), string(b)))
	}
}

// diffLines lists lines that appear in only one of the two texts.
func diffLines(want, got string) string {
	count := func(s string) map[string]int {
		m := map[string]int{}
		for _, l := range strings.Split(s, "\n") {
			m[l]++
		}
		return m
	}
	w, g := count(want), count(got)
	var out []string
	for l, n := range w {
		if g[l] < n {
			out = append(out, "- "+l)
		}
	}
	for l, n := range g {
		if w[l] < n {
			out = append(out, "+ "+l)
		}
	}
	sort.Strings(out)
	if len(out) > 60 {
		out = append(out[:60], "...")
	}
	return strings.Join(out, "\n")
}

func TestCorpusPortableNames(t *testing.T) {
	requireTools(t, "exiftool", "ffmpeg")
	checkGolden(t, "corpus-portable.json", runCorpus(t, "portable"))
}

func TestCorpusAppleNames(t *testing.T) {
	requireTools(t, "exiftool", "ffmpeg")
	if runtime.GOOS == "windows" {
		t.Skip("Apple-style names cannot be written to NTFS; the portable corpus covers Windows")
	}
	checkGolden(t, "corpus-apple.json", runCorpus(t, "apple"))
}

// The screenshot and Trash flags change only the files they name: the
// screenshot leaves, the trashed photo joins the library.
func TestCorpusScreenshotAndTrashFlags(t *testing.T) {
	requireTools(t, "exiftool", "ffmpeg")
	const shot, trashed = "2019/Screenshot_20190607-101010.png", "2019/trashed.jpg"
	def, drep := runCorpusWith(t, "portable", nil)
	flagged, frep := runCorpusWith(t, "portable", func(o *Options) {
		o.ExcludeScreenshots, o.IncludeTrash = true, true
	})
	if _, ok := def.Files[shot]; !ok {
		t.Errorf("default run: %s missing", shot)
	}
	if _, ok := def.Files[trashed]; ok {
		t.Errorf("default run: %s placed", trashed)
	}
	if _, ok := flagged.Files[shot]; ok {
		t.Errorf("flagged run: %s placed", shot)
	}
	if _, ok := flagged.Files[trashed]; !ok {
		t.Errorf("flagged run: %s missing", trashed)
	}
	if drep.Screenshots != 0 || frep.Screenshots != 1 {
		t.Errorf("screenshots_excluded %d then %d, want 0 then 1", drep.Screenshots, frep.Screenshots)
	}
	if drep.Media != frep.Media || drep.Library != frep.Library {
		t.Errorf("media %d/%d library %d/%d: want both equal", drep.Media, frep.Media, drep.Library, frep.Library)
	}
	for rel := range def.Files {
		if _, ok := flagged.Files[rel]; !ok && rel != shot {
			t.Errorf("flagged run lost %s", rel)
		}
	}
}

// ExifTool crashing at random points of a corpus run changes nothing: every
// crash is retried on a new process and the library matches the golden.
// TAKEOUT_CHAOS_SEED replays a failure.
func TestCorpusSurvivesExifToolCrashes(t *testing.T) {
	requireTools(t, "exiftool", "ffmpeg")
	// A fixed seed keeps CI repeatable; "random" explores new schedules.
	seed := uint64(1)
	switch s := os.Getenv("TAKEOUT_CHAOS_SEED"); s {
	case "":
	case "random":
		seed = uint64(time.Now().UnixNano())
	default:
		fmt.Sscan(s, &seed)
	}
	t.Logf("TAKEOUT_CHAOS_SEED=%d", seed)
	rng := rand.New(rand.NewPCG(seed, 0))
	// Crashes at least four commands apart never add up to a crash run, and
	// a retried command always goes through.
	var crashes []int
	for n := 3 + rng.IntN(4); len(crashes) < 6; n += 4 + rng.IntN(12) {
		crashes = append(crashes, n)
	}
	bin, state := fakeexif.Setup(t, fakeexif.Rules{CrashOnceOn: crashes})
	got, rep := runCorpusWith(t, "portable", func(o *Options) { o.Exiftool = bin })
	// A crash point that lands on a retry is skipped, so count only one.
	if crashes[0] > fakeexif.Commands(state) || rep.Retries.ExiftoolRestarts == 0 {
		t.Errorf("crashes at %v, %d commands, %d restarts", crashes, fakeexif.Commands(state), rep.Retries.ExiftoolRestarts)
	}
	checkGolden(t, "corpus-portable.json", got)
}
