package pipeline

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorPassesAndLeavesTakeoutFilesAlone(t *testing.T) {
	requireTools(t, "exiftool")
	results := filepath.Join(t.TempDir(), "results")
	meta := filepath.Join(results, ".takeout")
	if err := os.MkdirAll(filepath.Join(meta, "doctor-keep"), 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(meta, "state.jsonl")
	if err := os.WriteFile(keep, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := Doctor(Options{Results: results, Stdout: &out}); code != ExitOK {
		t.Fatalf("exit %d\n%s", code, out.String())
	}
	for _, want := range []string{"ok    exiftool", "ok    results", "ok    utf-8 paths", "All required checks passed."} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in\n%s", want, out.String())
		}
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("doctor removed state.jsonl")
	}
	ents, _ := os.ReadDir(meta)
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "doctor-keep,state.jsonl" {
		t.Errorf(".takeout holds %v after doctor", names)
	}
}

func TestDoctorFailsWithFixLines(t *testing.T) {
	var out bytes.Buffer
	code := Doctor(Options{Results: t.TempDir(), Exiftool: filepath.Join(t.TempDir(), "no-exiftool"), Stdout: &out})
	if code != ExitPreflight {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	for _, want := range []string{"FAIL  exiftool", "Fix: ", "See: README.md#exiftool", "1 problem(s)"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
}
