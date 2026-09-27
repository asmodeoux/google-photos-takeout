package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/progress"
	"github.com/asmodeoux/google-photos-takeout/internal/state"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

func writeState(t *testing.T, results string, st progress.State) {
	t.Helper()
	dir := filepath.Join(results, ".takeout")
	os.MkdirAll(dir, 0o755)
	b, _ := json.Marshal(st)
	if err := os.WriteFile(filepath.Join(dir, "progress.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStatusLiveStoppedAndPidReuse(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	alive := func(int) bool { return true }
	dead := func(int) bool { return false }
	cases := []struct {
		age   time.Duration
		alive func(int) bool
		want  string
	}{
		{10 * time.Second, dead, "running: confirm duplicates 12/40"},
		{5 * time.Minute, alive, "running: confirm duplicates 12/40"},
		{5 * time.Minute, dead, "last run stopped during confirm duplicates 12/40"},
		// A pid that is alive long after the last update was reused by
		// another program.
		{30 * time.Minute, alive, "last run stopped during confirm duplicates 12/40"},
	}
	for _, c := range cases {
		results := t.TempDir()
		writeState(t, results, progress.State{Phase: "confirm duplicates", Done: 12, Total: 40, Pid: 42, UpdatedAt: now.Add(-c.age)})
		if got := status(results, now, c.alive); !strings.Contains(got, c.want) {
			t.Errorf("age %s: %q, want %q", c.age, got, c.want)
		}
	}
}

// status reads only: it creates nothing, not even the .takeout folder.
func TestStatusCreatesNoFiles(t *testing.T) {
	results := filepath.Join(t.TempDir(), "results")
	if got := Status(results); got != "no run yet" {
		t.Fatalf("got %q", got)
	}
	if _, err := os.Stat(results); !os.IsNotExist(err) {
		t.Fatalf("status created %s", results)
	}
}

// check writes no progress file; a finished run removes its own, so status
// shows only the journal line, as before.
func TestProgressFileLifecycle(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	arch := writeTakeout(t, dir, map[string][]byte{"a.jpg": testgen.JPEG(1)})
	results := filepath.Join(dir, "results")
	opt := testOptions(arch, results)
	opt.DryRun = true
	if code, _, err := Run(context.Background(), opt); code != ExitOK {
		t.Fatalf("check exit %d: %v", code, err)
	}
	if _, err := os.Stat(filepath.Join(results, ".takeout", "progress.json")); !os.IsNotExist(err) {
		t.Fatal("check wrote progress.json")
	}
	opt.DryRun = false
	opt.FailAfter = 1
	if code, _, _ := Run(context.Background(), opt); code != ExitInterrupt {
		t.Fatalf("fail-after exit %d", code)
	}
	if got := status(results, time.Now().Add(time.Hour), func(int) bool { return false }); !strings.Contains(got, "last run stopped during copy") {
		t.Fatalf("after interrupt: %q", got)
	}
	opt.FailAfter = 0
	if code, _, err := Run(context.Background(), opt); code != ExitOK {
		t.Fatalf("run exit %d: %v", code, err)
	}
	if got := Status(results); !strings.HasPrefix(got, "journal lines ") || strings.Contains(got, "\n") {
		t.Fatalf("after a finished run: %q", got)
	}
}

// A second run on a results folder in use stops before it reads the zips,
// and leaves the first run's progress file alone.
func TestSecondRunStopsAtOnceAndKeepsProgress(t *testing.T) {
	arch, results := fakeTakeout(t, "a")
	release, err := state.Lock(filepath.Join(results, ".takeout"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	live := progress.State{Phase: "tags", Done: 1, Total: 2, Pid: os.Getpid(), UpdatedAt: time.Now().UTC()}
	writeState(t, results, live)
	before, _ := os.ReadFile(filepath.Join(results, ".takeout", "progress.json"))
	code, _, err := Run(context.Background(), testOptions(arch, results))
	if code != ExitPreflight || !errors.Is(err, state.ErrLocked) {
		t.Fatalf("exit %d: %v", code, err)
	}
	after, _ := os.ReadFile(filepath.Join(results, ".takeout", "progress.json"))
	if string(after) != string(before) {
		t.Fatalf("progress.json changed:\n%s\n%s", before, after)
	}
}
