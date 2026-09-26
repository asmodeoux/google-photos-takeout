package exiftool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool/fakeexif"
)

func TestMain(m *testing.M) {
	fakeexif.Main()
	os.Exit(m.Run())
}

// photos writes n copies of a tiny JPEG named after the given pattern.
func photos(t *testing.T, n int, name func(i int) string) []string {
	t.Helper()
	dir := t.TempDir()
	var out []string
	for i := range n {
		p := filepath.Join(dir, name(i))
		if err := os.WriteFile(p, tinyJPEG, 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func plain(i int) string { return fmt.Sprintf("p%02d.jpg", i) }

func writeArgs(p string) []string {
	return []string{"-m", "-overwrite_original", "-ExifIFD:DateTimeOriginal=2019:06:06 14:23:31", p}
}

// newFakePool starts a pool on the fake and stops it, and every process the
// fakes started, when the test ends.
func newFakePool(t *testing.T, r fakeexif.Rules, o PoolOptions) (*Pool, string) {
	t.Helper()
	bin, state := fakeexif.Setup(t, r)
	p, err := NewPool(bin, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		p.Kill()
		fakeexif.WaitGone(t, append(fakeexif.Starts(state), fakeexif.Kids(state)...), 10*time.Second)
	})
	return p, state
}

func mustWrite(t *testing.T, p *Pool, file string) {
	t.Helper()
	r, err := p.Run(writeArgs(file), 0)
	if err != nil || !Updated(r.Out) {
		t.Fatalf("write %s: %v %+v", filepath.Base(file), err, r)
	}
}

func TestPoolRecoversCrashDuringWrite(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{CrashOn: []int{1}}, PoolOptions{Size: 1})
	mustWrite(t, p, photos(t, 1, plain)[0])
	if p.Restarts() != 1 {
		t.Fatalf("restarts %d", p.Restarts())
	}
}

// The write happened but its {ready} line was lost: the resend finds the
// same values and ExifTool reports the file unchanged, which counts as done.
func TestPoolReplaysWriteWhoseReplyWasLost(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{CrashAfterReplyOn: []int{1}}, PoolOptions{Size: 1})
	f := photos(t, 1, plain)[0]
	mustWrite(t, p, f)
	r, err := p.Run([]string{"-s3", "-DateTimeOriginal", f}, 0)
	if err != nil || strings.TrimSpace(r.Out) != "2019:06:06 14:23:31" {
		t.Fatalf("read back %v %q", err, r.Out)
	}
}

func TestPoolReadBatchCrashLosesNoRows(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{CrashOn: []int{1}}, PoolOptions{Size: 1})
	files := photos(t, 5, plain)
	rows, failed, err := p.ReadAll(context.Background(), files, []string{"FileType"}, false)
	if err != nil || len(failed) != 0 || len(rows) != 5 {
		t.Fatalf("rows %d failed %v err %v", len(rows), failed, err)
	}
}

// A file that crashes ExifTool every time costs only itself, wherever it
// sits in the batch.
func TestPoolPoisonFileInReadBatchCostsOnlyItself(t *testing.T) {
	for _, pos := range []int{0, 3} {
		t.Run(fmt.Sprint(pos), func(t *testing.T) {
			p, _ := newFakePool(t, fakeexif.Rules{Poison: "poison"}, PoolOptions{Size: 2})
			files := photos(t, 6, func(i int) string {
				if i == pos {
					return "poison.jpg"
				}
				return plain(i)
			})
			rows, failed, err := p.ReadAll(context.Background(), files, []string{"FileType"}, false)
			if err != nil {
				t.Fatal(err)
			}
			if len(failed) != 1 || failed[files[pos]] == "" || len(rows) != 5 {
				t.Fatalf("rows %d failed %v", len(rows), failed)
			}
		})
	}
}

// Isolated poison files, more than the pool has processes, each fail once
// and never break the pool.
func TestPoolManyPoisonFilesNeverBreakIt(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{Poison: "poison"}, PoolOptions{Size: 2})
	files := photos(t, 12, func(i int) string {
		if i%2 == 1 {
			return fmt.Sprintf("poison%02d.jpg", i)
		}
		return plain(i)
	})
	for i, f := range files {
		_, err := p.Run(writeArgs(f), 0)
		var fe *FileError
		switch {
		case i%2 == 0 && err != nil:
			t.Fatalf("good file %d: %v", i, err)
		case i%2 == 1 && !errors.As(err, &fe):
			t.Fatalf("poison file %d: %v", i, err)
		}
	}
}

// Poison files hit by every worker at once reach the crash limit; the probe
// passes, so the pool carries on and each file fails alone.
func TestPoolAdjacentPoisonFilesOnAllWorkers(t *testing.T) {
	for _, n := range []int{4, 8} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			p, _ := newFakePool(t, fakeexif.Rules{Poison: "poison"}, PoolOptions{Size: 4})
			good := photos(t, 4, plain)
			for _, f := range good {
				mustWrite(t, p, f)
			}
			bad := photos(t, n, func(i int) string { return fmt.Sprintf("poison%d.jpg", i) })
			var mu sync.Mutex
			failures := 0
			var wg sync.WaitGroup
			for _, f := range bad {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := p.Run(writeArgs(f), 0)
					var fe *FileError
					mu.Lock()
					defer mu.Unlock()
					if errors.As(err, &fe) {
						failures++
					} else {
						t.Errorf("poison write: %v", err)
					}
				}()
			}
			wg.Wait()
			if failures != n {
				t.Fatalf("failures %d, want %d", failures, n)
			}
			mustWrite(t, p, good[0])
		})
	}
}

// A process that crashes on every command breaks the pool; callers already
// waiting get the same error and no process is left.
func TestPoolBreaksWhenEveryCommandCrashes(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{CrashAll: true}, PoolOptions{Size: 2})
	files := photos(t, 8, plain)
	var wg sync.WaitGroup
	errs := make(chan error, len(files))
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Run(writeArgs(f), 0)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	broken := 0
	for err := range errs {
		var fe *FileError
		switch {
		case errors.Is(err, ErrPoolBroken):
			broken++
		case errors.As(err, &fe):
		default:
			t.Errorf("unexpected %v", err)
		}
	}
	if broken == 0 {
		t.Fatal("pool never broke")
	}
	if _, err := p.Run(writeArgs(files[0]), 0); !errors.Is(err, ErrPoolBroken) {
		t.Fatalf("after break: %v", err)
	}
}

// "-ver" works but every real write crashes: a -ver probe would pass and the
// run would end as "usable". The probe does a real write, so the pool breaks.
func TestPoolRealProbeCatchesBrokenWrites(t *testing.T) {
	probeFile := photos(t, 1, func(int) string { return "probe.jpg" })[0]
	probe := func(run func([]string) (Reply, error)) error {
		r, err := run(writeArgs(probeFile))
		if err == nil && !Updated(r.Out) {
			err = errors.New("probe write did not update")
		}
		return err
	}
	p, _ := newFakePool(t, fakeexif.Rules{CrashAll: true, ExceptVersion: true}, PoolOptions{Size: 1, Probe: probe})
	files := photos(t, 4, plain)
	var err error
	for _, f := range files {
		if _, err = p.Run(writeArgs(f), 0); errors.Is(err, ErrPoolBroken) {
			break
		}
	}
	var be *BrokenError
	if !errors.As(err, &be) || be.ProbeErr == nil {
		t.Fatalf("want a failed probe, got %v", err)
	}
}

func TestPoolKillDuringCommandStartsNothing(t *testing.T) {
	p, state := newFakePool(t, fakeexif.Rules{Hang: "hang"}, PoolOptions{Size: 1})
	f := photos(t, 1, func(int) string { return "hang.jpg" })[0]
	done := make(chan error, 1)
	go func() { _, err := p.Run(writeArgs(f), 0); done <- err }()
	waitFor(t, func() bool { return fakeexif.Commands(state) >= 1 })
	starts := len(fakeexif.Starts(state))
	p.Kill()
	select {
	case err := <-done:
		if !errors.Is(err, ErrKilled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Kill")
	}
	time.Sleep(200 * time.Millisecond)
	if n := len(fakeexif.Starts(state)); n != starts {
		t.Fatalf("%d processes started after Kill", n-starts)
	}
}

// Ctrl+C while the pool probes must not wait for the probe.
func TestPoolKillDuringProbeReturnsPromptly(t *testing.T) {
	probing := make(chan struct{})
	hangFile := photos(t, 1, func(int) string { return "hang.jpg" })[0]
	probe := func(run func([]string) (Reply, error)) error {
		close(probing)
		_, err := run(writeArgs(hangFile))
		return err
	}
	p, _ := newFakePool(t, fakeexif.Rules{Hang: "hang", Poison: "poison"}, PoolOptions{Size: 1, MaxCrashRun: 1, Probe: probe})
	bad := photos(t, 1, func(int) string { return "poison.jpg" })[0]
	done := make(chan error, 1)
	go func() { _, err := p.Run(writeArgs(bad), 0); done <- err }()
	select {
	case <-probing:
	case <-time.After(10 * time.Second):
		t.Fatal("probe did not start")
	}
	start := time.Now()
	p.Kill()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Kill took %s", d)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Kill")
	}
}

func TestPoolTimeoutFailsFileWithoutResend(t *testing.T) {
	p, state := newFakePool(t, fakeexif.Rules{Hang: "hang"}, PoolOptions{Size: 1, CommandTimeout: time.Second})
	f := photos(t, 1, func(int) string { return "hang.jpg" })[0]
	_, err := p.Run(writeArgs(f), 0)
	var fe *FileError
	if !errors.As(err, &fe) || !strings.HasPrefix(fe.Cause, "timed out after") {
		t.Fatalf("got %v", err)
	}
	if n := fakeexif.Commands(state); n != 1 {
		t.Fatalf("commands %d, want 1 (no resend)", n)
	}
	mustWrite(t, p, photos(t, 1, plain)[0])
}

func TestPoolTimeoutGrowsWithFileSize(t *testing.T) {
	p := &Pool{o: PoolOptions{CommandTimeout: 2 * time.Minute}}
	if got := p.timeout(1 << 20); got != 2*time.Minute {
		t.Fatalf("small file: %s", got)
	}
	if got := p.timeout(20 << 30); got < 60*time.Minute {
		t.Fatalf("20 GB video: %s", got)
	}
}

// When the launcher exits but a child keeps its stdout open, as perl.exe
// does after exiftool.exe, the pool notices at once instead of waiting for
// the time limit, and the child is killed.
func TestPoolNoticesLauncherExitWhileChildHoldsPipe(t *testing.T) {
	p, state := newFakePool(t, fakeexif.Rules{OrphanOn: 1}, PoolOptions{Size: 1, CommandTimeout: time.Minute})
	start := time.Now()
	mustWrite(t, p, photos(t, 1, plain)[0])
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("took %s: the exit was not noticed", d)
	}
	fakeexif.WaitGone(t, fakeexif.Kids(state)[:len(fakeexif.Kids(state))-1], 10*time.Second)
}

// A process that cannot even start breaks the pool instead of leaving
// callers waiting for a free process forever.
func TestPoolStartFailureBreaksIt(t *testing.T) {
	bin, _ := fakeexif.Setup(t, fakeexif.Rules{CrashOn: []int{1}})
	dir := t.TempDir()
	name := "exiftool-copy"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cp := filepath.Join(dir, name)
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cp, b, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := NewPool(cp, PoolOptions{Size: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Kill()
	if err := os.Rename(cp, cp+".gone"); err != nil {
		t.Skip("cannot rename a running executable here:", err)
	}
	_, err = p.Run(writeArgs(photos(t, 1, plain)[0]), 0)
	if !errors.Is(err, ErrPoolBroken) {
		t.Fatalf("got %v", err)
	}
}

// Too many recent failures stop the run even though successes keep resetting
// the crash counter: a disk that hangs on every video would otherwise cost
// days.
func TestPoolFailureWindowStopsRun(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{Hang: "hang"}, PoolOptions{Size: 1, CommandTimeout: 500 * time.Millisecond, Window: 10, WindowFailures: 3})
	files := photos(t, 10, func(i int) string {
		if i%2 == 1 {
			return fmt.Sprintf("hang%d.mov.jpg", i)
		}
		return plain(i)
	})
	var err error
	for _, f := range files {
		if _, err = p.Run(writeArgs(f), 0); errors.Is(err, ErrPoolBroken) {
			break
		}
	}
	var be *BrokenError
	if !errors.As(err, &be) || !be.Window || be.Timeouts != 3 {
		t.Fatalf("got %v", err)
	}
}

func TestPoolConcurrentRunsWithCrashes(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{CrashOn: []int{5}}, PoolOptions{Size: 3})
	files := photos(t, 24, plain)
	var wg sync.WaitGroup
	for _, f := range files {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, err := p.Run(writeArgs(f), 0); err != nil || !Updated(r.Out) {
				t.Errorf("%s: %v", filepath.Base(f), err)
			}
		}()
	}
	wg.Wait()
	if p.Restarts() < 1 {
		t.Fatalf("restarts %d", p.Restarts())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A read batch that times out is not resent; read one file at a time, only
// the file that hangs is lost.
func TestPoolReadBatchTimeoutCostsOnlyTheHangingFile(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{Hang: "hang.jpg"}, PoolOptions{Size: 1, CommandTimeout: time.Second})
	files := photos(t, 4, func(i int) string {
		if i == 2 {
			return "hang.jpg"
		}
		return plain(i)
	})
	rows, failed, err := p.ReadAll(context.Background(), files, []string{"FileType"}, false)
	if err != nil || len(failed) != 1 || failed[files[2]] == "" || len(rows) != 3 {
		t.Fatalf("rows %d failed %v err %v", len(rows), failed, err)
	}
}

// When ExifTool hangs on every file, a batch costs a few timeouts, not one
// per file.
func TestPoolReadHangOnEveryFileGivesUpTheBatch(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{Hang: ".jpg"}, PoolOptions{Size: 1, CommandTimeout: time.Second, WindowFailures: 50})
	files := photos(t, ReadBatch, plain)
	start := time.Now()
	rows, failed, err := p.ReadAll(context.Background(), files, []string{"FileType"}, false)
	if len(rows) != 0 || len(failed) != len(files) {
		t.Fatalf("rows %d failed %d err %v", len(rows), len(failed), err)
	}
	// The batch, then MaxCrashRun single files: about four timeouts of 1s.
	if d := time.Since(start); d > 15*time.Second {
		t.Fatalf("took %s", d)
	}
}

// Many files that crash ExifTool while ExifTool itself works do not stop the
// run: a resume, which skips the finished files, would stop on them again.
func TestPoolCrashWindowAsksTheProbe(t *testing.T) {
	p, _ := newFakePool(t, fakeexif.Rules{Poison: "poison"}, PoolOptions{Size: 1, Window: 10, WindowFailures: 3})
	files := photos(t, 8, func(i int) string { return fmt.Sprintf("poison%02d.jpg", i) })
	for _, f := range files {
		var fe *FileError
		if _, err := p.Run(writeArgs(f), 0); !errors.As(err, &fe) {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
	}
	mustWrite(t, p, photos(t, 1, plain)[0])
}

// Ctrl+C stops a read: no new batch starts once the context is done.
func TestPoolReadAllStopsOnCancel(t *testing.T) {
	p, state := newFakePool(t, fakeexif.Rules{}, PoolOptions{Size: 1})
	files := photos(t, 3*ReadBatch, plain)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := p.ReadAll(ctx, files, []string{"FileType"}, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if n := fakeexif.Commands(state); n > 1 {
		t.Fatalf("%d batches sent after cancel", n)
	}
}
