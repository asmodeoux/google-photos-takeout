package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

func TestTagErrorFilesAndStatus(t *testing.T) {
	groups := []group{
		{members: []member{{Entry: zipindex.Entry{RelFolder: "Photos from 2019", Name: "a.jpg"}}}, outRel: filepath.Join("2019", "a.jpg"), tagErr: "exiftool did not update: Error: boom"},
		{members: []member{{Entry: zipindex.Entry{RelFolder: "Photos from 2019", Name: "b.jpg"}}}, tagErr: "copy failed"},
		{members: []member{{Entry: zipindex.Entry{RelFolder: "Photos from 2019", Name: "c.jpg"}}}, outRel: "2019/c.jpg"},
	}
	got := tagErrorFiles(groups)
	if len(got) != 2 || got[0].Path != "2019/a.jpg" || got[1].Path != "Photos from 2019/b.jpg" || !strings.Contains(got[0].Stderr, "boom") {
		t.Fatalf("tag error files %+v", got)
	}

	results := t.TempDir()
	if err := os.MkdirAll(filepath.Join(results, ".takeout"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(results, ".takeout", "state.jsonl"), []byte("{}\n"), 0o644)
	writeReport(results, Report{TagErrors: 2, TagErrorFiles: got})
	if s := Status(results); !strings.Contains(s, "2 tag errors, see ") || !strings.Contains(s, "tag_error_files") {
		t.Errorf("status %q", s)
	}
	writeReport(results, Report{})
	if s := Status(results); strings.Contains(s, "tag errors") {
		t.Errorf("status with no errors %q", s)
	}
}

func TestReportHasTimingsAndRetries(t *testing.T) {
	requireTools(t, "exiftool", "ffmpeg")
	dir := t.TempDir()
	arch := filepath.Join(dir, "archives")
	if _, err := testgen.Corpus().Write(arch); err != nil {
		t.Fatal(err)
	}
	results := filepath.Join(dir, "results")
	code, _, err := Run(context.Background(), Options{Archives: arch, Results: results, DefaultTZ: "UTC", Albums: "none",
		Progress: "plain", Quiet: true, Stdout: &bytes.Buffer{}, Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if code != ExitOK {
		t.Fatalf("exit %d: %v", code, err)
	}
	b, err := os.ReadFile(filepath.Join(results, ".takeout", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	json.Unmarshal(b, &raw)
	secs, _ := raw["seconds"].(map[string]any)
	for _, p := range []string{"index", "sidecars", "copy", "tags", "place", "verify"} {
		if _, ok := secs[p]; !ok {
			t.Errorf("seconds lacks %s: %v", p, secs)
		}
	}
	if fps, _ := raw["files_per_second"].(float64); fps <= 0 {
		t.Errorf("files_per_second %v", raw["files_per_second"])
	}
	if r, ok := raw["retries"].(map[string]any); !ok || r["rename"] == nil || r["tag"] == nil {
		t.Errorf("retries %v", raw["retries"])
	}
}
