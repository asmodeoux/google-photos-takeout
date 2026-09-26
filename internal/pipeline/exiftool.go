package pipeline

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/dates"
	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
)

// startPool starts the ExifTool processes for a run or verify. The pool's
// health probe writes a real photo in results/.takeout/probe, on the same
// disk and past the same antivirus as the library.
func startPool(opt Options, size int, log *eventLog) (*exiftool.Pool, error) {
	o := exiftool.PoolOptions{
		Size:           size,
		CommandTimeout: opt.ExiftoolTimeout,
		Probe:          probe(opt.Results),
		OnEvent:        log.add,
	}
	if tunePool != nil {
		tunePool(&o)
	}
	return exiftool.NewPool(opt.Exiftool, o)
}

// tunePool lets tests shrink the pool's limits.
var tunePool func(*exiftool.PoolOptions)

// destError is a probe that failed before ExifTool ran: the results disk
// refused the test photo. It points at the disk, not at ExifTool.
type destError struct{ err error }

func (e *destError) Error() string { return e.err.Error() }
func (e *destError) Unwrap() error { return e.err }

// probe writes a date and GPS into a test photo and reads them back, with
// the same arguments the library gets.
func probe(results string) func(run func([]string) (exiftool.Reply, error)) error {
	return func(run func([]string) (exiftool.Reply, error)) error {
		// A folder of its own, so a verify running next to a run never
		// removes the run's test photo.
		base := filepath.Join(results, ".takeout")
		if err := os.MkdirAll(base, 0o755); err != nil {
			return &destError{err}
		}
		dir, err := os.MkdirTemp(base, "probe-")
		if err != nil {
			return &destError{err}
		}
		defer os.RemoveAll(dir)
		p := filepath.Join(dir, "probe.jpg")
		if err := os.WriteFile(p, exiftool.ProbePhoto(), 0o644); err != nil {
			return &destError{err}
		}
		when := dates.When{
			Instant: time.Date(2019, 6, 6, 11, 23, 31, 0, time.UTC), Offset: 3 * time.Hour, OffsetKnown: true,
			Year: 2019, Lat: 55.7558, Lon: 37.6173, HasGPS: true, OK: true,
		}
		args, err := exiftool.Args(exiftool.Plan{Path: p, Kind: "jpeg", WriteDates: true, WriteGPS: true, When: when})
		if err != nil {
			return err
		}
		r, err := run(args)
		if err != nil {
			return err
		}
		if !exiftool.Updated(r.Out) {
			return fmt.Errorf("the test write did not update: %s", firstNonEmpty(r.Err, r.Out))
		}
		abs, _ := filepath.Abs(p)
		r, err = run([]string{"-s3", "-DateTimeOriginal", abs})
		if err != nil {
			return err
		}
		if strings.TrimSpace(r.Out) != "2019:06:06 14:23:31" {
			return fmt.Errorf("the test date did not read back: %q", strings.TrimSpace(r.Out))
		}
		return nil
	}
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x = strings.TrimSpace(x); x != "" {
			return x
		}
	}
	return ""
}

// eventLog appends what the pool did to results/.takeout/exiftool.log, so a
// stopped run can be explained later. It opens the file on the first event.
type eventLog struct {
	mu    sync.Mutex
	path  string
	count int
}

func newEventLog(results string) *eventLog {
	if results == "" {
		return nil
	}
	return &eventLog{path: filepath.Join(results, ".takeout", "exiftool.log")}
}

func (l *eventLog) add(e exiftool.Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	line := time.Now().UTC().Format(time.RFC3339) + " " + e.Kind
	if e.Pid != 0 {
		line += fmt.Sprintf(" pid=%d", e.Pid)
	}
	for _, p := range e.Files {
		line += " file=" + filepath.Base(p)
	}
	if e.Detail != "" {
		line += " " + e.Detail
	}
	fmt.Fprintln(f, line)
	l.count++
}

// written is the log path if anything was logged in this run.
func (l *eventLog) written() string {
	if l == nil {
		return ""
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.count == 0 {
		return ""
	}
	return l.path
}

// stopError turns a pool that gave up into the message the user sees. Paths
// are quoted plainly, not with %q, which would double Windows backslashes. It is
// exit 2: something to fix, after which the same command resumes.
func stopError(err error, exiftoolPath, results string) error {
	var be *exiftool.BrokenError
	if !errors.As(err, &be) {
		return err
	}
	var de *destError
	switch {
	case errors.As(err, &de):
		abs, _ := filepath.Abs(results)
		return &RuntimeStopError{
			Problem: "cannot write a test photo in the results folder (" + de.Error() + ")", Value: abs,
			Fix: "free some space, or pass a folder you can write to with --results; then run the same command to resume", Anchor: "disk-space", Err: err,
		}
	case be.Window && be.Timeouts >= be.Crashes:
		return &RuntimeStopError{
			Problem: fmt.Sprintf("ExifTool timed out on %d and crashed on %d of the last files", be.Timeouts, be.Crashes), Value: exiftoolPath,
			Fix:    "the disk is slow or scanned by antivirus: exclude the results folder from scanning (README.md#antivirus), or raise the limit with --exiftool-timeout 10m; then run the same command to resume",
			Anchor: "exiftool-keeps-crashing", Err: err,
		}
	case be.Restart:
		return &RuntimeStopError{
			Problem: "ExifTool crashed and could not be started again (" + firstLine(be.ProbeErr.Error()) + ")", Value: exiftoolPath,
			Fix:    "run \"" + exiftoolPath + " -ver\"; if it fails, reinstall ExifTool or check that antivirus has not removed it; then run the same command to resume",
			Anchor: "exiftool-keeps-crashing", Err: err,
		}
	default:
		detail := ""
		if be.ProbeErr != nil {
			detail = " (" + firstLine(be.ProbeErr.Error()) + ")"
		}
		problem := "ExifTool failed on a test photo after it crashed on several files" + detail
		if be.Window {
			problem = fmt.Sprintf("ExifTool crashed on %d and timed out on %d of the last files", be.Crashes, be.Timeouts)
		}
		av := "check antivirus (README.md#antivirus)"
		if runtime.GOOS != "windows" {
			av = "check that nothing else is killing it"
		}
		return &RuntimeStopError{
			Problem: problem, Value: exiftoolPath,
			Fix:    "run \"" + exiftoolPath + " -ver\"; if it prints a version, " + av + ", otherwise reinstall ExifTool; then run the same command to resume",
			Anchor: "exiftool-keeps-crashing", Err: err,
		}
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
