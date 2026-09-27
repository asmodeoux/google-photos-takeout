package exiftool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Pool owns a fixed number of ExifTool processes and keeps them running.
//
//	Run / ReadAll ─► checkout ─► Client.run ─┬─ reply ─────────────► caller
//	                                          └─ process failed ─► replace client
//	                                                                 │ crash: resend once
//	                                                                 │ timeout: no resend
//	                                                                 ▼
//	                                              failed lineage ─► crash run ≥ MaxCrashRun ─► probe
//	                                                             └─► failure window full ─┬─ mostly timeouts ─► broken
//	                                                                                       └─ mostly crashes ─► probe
//	probe: a real write and read-back on a fresh process; only a failed probe
//	(or a process that cannot start) breaks the pool.
//
// A lineage is one caller command, or one ReadAll batch with its resend and
// single-file reads. It counts once, however many processes it cost.
// ExifTool's own per-file errors are replies and count as success here: the
// process worked.
type Pool struct {
	bin string
	o   PoolOptions

	mu          sync.Mutex
	cond        *sync.Cond
	all         map[*Client]bool
	free        []*Client
	killed      bool
	broken      error
	probing     bool
	probeClient *Client
	crashRun    int
	probeFails  int
	window      []outcome
	next        int

	ids      atomic.Int64
	restarts atomic.Int64
}

// PoolOptions tunes a Pool. Zero values take the defaults.
type PoolOptions struct {
	Size           int           // processes; default 4
	CommandTimeout time.Duration // floor of the per-command time limit; default 2 minutes
	MaxCrashRun    int           // failed lineages in a row before the pool probes; default 3
	Window         int           // lineages remembered for the failure window; default 100
	WindowFailures int           // failures in the window that stop the run (timeouts) or start a probe (crashes); default 20
	// Probe checks a fresh process with real work. It gets a function that
	// runs one command on that process. nil runs "-ver".
	Probe func(run func(args []string) (Reply, error)) error
	// OnEvent receives crashes, timeouts, restarts and probes, for a log.
	OnEvent func(Event)
}

// Event is one thing the pool did.
type Event struct {
	Kind   string // "crash", "timeout", "restart", "probe-passed", "probe-failed", "stopped"
	Pid    int
	Files  []string
	Detail string
}

type outcome struct{ failed, timedOut bool }

// minThroughput is the slowest a command is expected to process a file, in
// bytes per second; it raises the time limit for big files.
const minThroughput = 5 << 20

// ErrPoolBroken is returned by every call once the pool gave up.
var ErrPoolBroken = errors.New("exiftool keeps failing")

// ErrKilled is returned by calls made after Kill.
var ErrKilled = errors.New("exiftool was stopped")

// BrokenError says why the pool gave up. errors.Is(err, ErrPoolBroken) holds.
type BrokenError struct {
	Path     string // the ExifTool that failed
	ProbeErr error  // set when a probe failed or a process could not start
	Restart  bool   // set when a process to replace a failed one could not start
	Window   bool   // set when too many recent commands failed
	Crashes  int    // failures in the window that were crashes
	Timeouts int    // failures in the window that were timeouts
}

func (e *BrokenError) Error() string {
	if e.Window {
		return fmt.Sprintf("exiftool failed on %d of the last commands (%d crashes, %d timeouts)", e.Crashes+e.Timeouts, e.Crashes, e.Timeouts)
	}
	if e.Restart {
		return "exiftool could not be started again after it failed: " + e.ProbeErr.Error()
	}
	return "exiftool failed a test write: " + e.ProbeErr.Error()
}

func (e *BrokenError) Is(target error) bool { return target == ErrPoolBroken }
func (e *BrokenError) Unwrap() error        { return e.ProbeErr }

// FileError is a command that failed at the process level after recovery.
// The pool carries on; the caller records the file as failed.
type FileError struct {
	Cause string
	Err   error
}

func (e *FileError) Error() string { return e.Cause }
func (e *FileError) Unwrap() error { return e.Err }

// NewPool starts the processes. It starts none if any fails.
func NewPool(bin string, o PoolOptions) (*Pool, error) {
	if o.Size <= 0 {
		o.Size = 4
	}
	if o.CommandTimeout <= 0 {
		o.CommandTimeout = 2 * time.Minute
	}
	if o.MaxCrashRun <= 0 {
		o.MaxCrashRun = 3
	}
	if o.Window <= 0 {
		o.Window = 100
	}
	if o.WindowFailures <= 0 {
		o.WindowFailures = 20
	}
	path, err := Look(bin)
	if err != nil {
		return nil, err
	}
	p := &Pool{bin: path, o: o, all: map[*Client]bool{}}
	p.cond = sync.NewCond(&p.mu)
	for range o.Size {
		c, err := Start(path)
		if err != nil {
			p.Kill()
			return nil, err
		}
		p.all[c] = true
		p.free = append(p.free, c)
	}
	return p, nil
}

// Path is the ExifTool the pool runs.
func (p *Pool) Path() string { return p.bin }

// Restarts counts processes started to replace failed ones.
func (p *Pool) Restarts() int { return int(p.restarts.Load()) }

// timeout is the limit for a command touching size bytes of files.
func (p *Pool) timeout(size int64) time.Duration {
	t := p.o.CommandTimeout
	if size <= 0 {
		return t
	}
	if byThroughput := time.Duration(size/minThroughput+1) * time.Second; byThroughput > t {
		t = byThroughput
	}
	return t
}

// Run sends one command. size is the total size of the files it rewrites,
// which raises the time limit for big videos. A crash is recovered by
// resending the command once on a new process; a timeout is not resent,
// because a hang is usually caused by the file. Either way the process is
// replaced and a FileError says what happened.
func (p *Pool) Run(args []string, size int64) (Reply, error) {
	if err := CheckArgs(args); err != nil {
		return Reply{}, err
	}
	r, err := p.try(args, size)
	var pe *ProcessError
	if !isStop(err) && errors.As(err, &pe) && !pe.TimedOut {
		r, err = p.try(args, size)
	}
	if isStop(err) || !errors.As(err, &pe) {
		if err == nil {
			p.settle(outcome{})
		}
		return r, err
	}
	cause := "ExifTool crashed on this file twice"
	if pe.TimedOut {
		cause = fmt.Sprintf("timed out after %s", pe.Timeout.Round(time.Second))
	}
	p.settle(outcome{failed: true, timedOut: pe.TimedOut})
	if err := p.stopped(); err != nil {
		return r, err
	}
	return r, &FileError{Cause: cause, Err: err}
}

// try runs one command on one process and replaces the process if it failed.
func (p *Pool) try(args []string, size int64) (Reply, error) {
	c, err := p.checkout()
	if err != nil {
		return Reply{}, err
	}
	r, err := c.run(args, int(p.ids.Add(1)), p.timeout(size))
	var pe *ProcessError
	if isStop(err) || !errors.As(err, &pe) {
		p.giveBack(c)
		return r, err
	}
	kind := "crash"
	if pe.TimedOut {
		kind = "timeout"
	}
	p.emit(Event{Kind: kind, Pid: c.Pid(), Files: fileArgs(args), Detail: firstLine(pe.Stderr)})
	p.replace(c)
	if err := p.stopped(); err != nil {
		return r, err
	}
	return r, err
}

// stopped is the error every call returns once the pool was killed or broke.
func (p *Pool) stopped() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.killed {
		return ErrKilled
	}
	return p.broken
}

func (p *Pool) checkout() (*Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for !p.killed && p.broken == nil && (p.probing || len(p.free) == 0) {
		p.cond.Wait()
	}
	if p.killed {
		return nil, ErrKilled
	}
	if p.broken != nil {
		return nil, p.broken
	}
	c := p.free[len(p.free)-1]
	p.free = p.free[:len(p.free)-1]
	return c, nil
}

func (p *Pool) giveBack(c *Client) {
	p.mu.Lock()
	p.free = append(p.free, c)
	p.mu.Unlock()
	p.cond.Signal()
}

// replace retires a failed process and starts another in its place. A
// process that cannot start breaks the pool: nothing else would.
func (p *Pool) replace(c *Client) {
	p.mu.Lock()
	delete(p.all, c)
	stop := p.killed || p.broken != nil
	p.mu.Unlock()
	c.Kill()
	if stop {
		return
	}
	n, err := Start(p.bin)
	p.mu.Lock()
	if err != nil {
		p.breakLocked(&BrokenError{Path: p.bin, ProbeErr: err, Restart: true})
		p.mu.Unlock()
		return
	}
	if p.killed || p.broken != nil {
		p.mu.Unlock()
		n.Kill()
		return
	}
	p.restarts.Add(1)
	p.all[n] = true
	p.free = append(p.free, n)
	p.cond.Broadcast()
	p.mu.Unlock()
	p.emit(Event{Kind: "restart", Pid: n.Pid()})
}

// settle records how a lineage ended and runs a probe or stops the pool.
func (p *Pool) settle(o outcome) {
	p.mu.Lock()
	if len(p.window) < p.o.Window {
		p.window = append(p.window, o)
	} else {
		p.window[p.next] = o
		p.next = (p.next + 1) % p.o.Window
	}
	crashes, timeouts := 0, 0
	for _, w := range p.window {
		if w.failed && w.timedOut {
			timeouts++
		} else if w.failed {
			crashes++
		}
	}
	if crashes+timeouts >= p.o.WindowFailures && p.broken == nil && !p.killed {
		// Mostly timeouts: the disk or antivirus is too slow, and a probe on
		// a tiny photo would not show it. Stop.
		if timeouts >= crashes {
			p.breakLocked(&BrokenError{Path: p.bin, Window: true, Crashes: crashes, Timeouts: timeouts})
			p.mu.Unlock()
			p.emit(Event{Kind: "stopped", Detail: fmt.Sprintf("%d crashes, %d timeouts in the last %d commands", crashes, timeouts, len(p.window))})
			return
		}
		// Mostly crashes: ask the probe. If a real write works, ExifTool is
		// fine and these files crash it; they stay tag errors and the run
		// goes on, or a resume would stop on them every time.
		if !p.probing {
			p.window, p.next = nil, 0
			p.probing = true
			p.mu.Unlock()
			p.runProbe()
			return
		}
	}
	switch {
	case !o.failed:
		if !p.probing {
			p.crashRun = 0
		}
	case p.probing:
		// Counted once the probe finishes.
		p.probeFails++
	default:
		p.crashRun++
	}
	probe := o.failed && !p.probing && p.crashRun >= p.o.MaxCrashRun && p.broken == nil && !p.killed
	if probe {
		p.probing = true
	}
	p.mu.Unlock()
	if probe {
		p.runProbe()
	}
}

// Check runs the health probe now. A caller that saw files fail and wants
// to know whether ExifTool itself works uses it: a failed probe breaks the
// pool and Check returns that error.
func (p *Pool) Check() error {
	p.mu.Lock()
	for p.probing && !p.killed && p.broken == nil {
		p.cond.Wait()
	}
	if p.killed || p.broken != nil {
		p.mu.Unlock()
		return p.stopped()
	}
	p.probing = true
	p.mu.Unlock()
	p.runProbe()
	return p.stopped()
}

// runProbe checks a fresh process with real work, outside the lock so Kill
// never waits for it. Callers wait in checkout while it runs.
func (p *Pool) runProbe() {
	c, err := Start(p.bin)
	if err == nil {
		p.mu.Lock()
		if p.killed {
			p.mu.Unlock()
			c.Kill()
			p.finishProbe(nil)
			return
		}
		p.probeClient = c
		p.mu.Unlock()
		run := func(args []string) (Reply, error) {
			return c.run(args, int(p.ids.Add(1)), p.o.CommandTimeout)
		}
		if p.o.Probe != nil {
			err = p.o.Probe(run)
		} else {
			var r Reply
			r, err = run([]string{"-ver"})
			if err == nil && strings.TrimSpace(r.Out) == "" {
				err = errors.New("no version printed")
			}
		}
		c.Kill()
	}
	p.finishProbe(err)
}

func (p *Pool) finishProbe(err error) {
	p.mu.Lock()
	p.probeClient = nil
	p.probing = false
	if p.killed {
		p.cond.Broadcast()
		p.mu.Unlock()
		return
	}
	if err != nil {
		p.breakLocked(&BrokenError{Path: p.bin, ProbeErr: err})
		p.mu.Unlock()
		p.emit(Event{Kind: "probe-failed", Detail: err.Error()})
		return
	}
	// Failures that ended while the probe ran count now, but cannot start
	// another probe on their own: the next failure after this one will.
	p.crashRun = min(p.probeFails, p.o.MaxCrashRun-1)
	p.probeFails = 0
	p.cond.Broadcast()
	p.mu.Unlock()
	p.emit(Event{Kind: "probe-passed"})
}

// breakLocked stops every process and wakes every waiter. p.mu is held.
func (p *Pool) breakLocked(err error) {
	if p.broken != nil {
		return
	}
	p.broken = err
	for c := range p.all {
		go c.Kill()
	}
	if p.probeClient != nil {
		go p.probeClient.Kill()
	}
	p.cond.Broadcast()
}

// Kill stops every process at once, for Ctrl+C. No process starts after it,
// and calls in flight return ErrKilled.
func (p *Pool) Kill() {
	p.mu.Lock()
	p.killed = true
	clients := make([]*Client, 0, len(p.all)+1)
	for c := range p.all {
		clients = append(clients, c)
	}
	if p.probeClient != nil {
		clients = append(clients, p.probeClient)
	}
	p.cond.Broadcast()
	p.mu.Unlock()
	for _, c := range clients {
		c.Kill()
	}
}

// Close ends every process normally.
func (p *Pool) Close() {
	p.mu.Lock()
	clients := make([]*Client, 0, len(p.all))
	for c := range p.all {
		clients = append(clients, c)
	}
	p.all = map[*Client]bool{}
	p.free = nil
	p.mu.Unlock()
	for _, c := range clients {
		c.Close()
	}
}

func (p *Pool) emit(e Event) {
	if p.o.OnEvent != nil {
		p.o.OnEvent(e)
	}
}

// ReadAll reads tags from paths in batches of ReadBatch spread over the pool.
// Rows are keyed by PathKey(SourceFile). A batch whose process fails is
// resent once (not after a timeout), then read one file at a time, so a file
// that crashes ExifTool costs only itself; after MaxCrashRun failures in a
// row the rest of that batch is given up. failed maps the paths that still
// could not be read to the reason. err is ctx's error, ErrKilled or a
// pool-broken error; then the rows are incomplete.
func (p *Pool) ReadAll(ctx context.Context, paths, tags []string, numeric bool) (rows map[string]map[string]any, failed map[string]string, err error) {
	rows = map[string]map[string]any{}
	failed = map[string]string{}
	if len(paths) == 0 {
		return rows, failed, nil
	}
	var (
		mu    sync.Mutex
		fatal error
		wg    sync.WaitGroup
	)
	batches := make(chan []string)
	add := func(got []map[string]any) {
		for _, row := range got {
			if src, ok := row["SourceFile"].(string); ok {
				rows[PathKey(src)] = row
			}
		}
	}
	for range p.o.Size {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range batches {
				got, bad, err := p.readBatch(ctx, b, tags, numeric)
				mu.Lock()
				add(got)
				for k, v := range bad {
					failed[k] = v
				}
				if err != nil && fatal == nil {
					fatal = err
				}
				mu.Unlock()
			}
		}()
	}
feed:
	for start := 0; start < len(paths); start += ReadBatch {
		mu.Lock()
		stop := fatal != nil
		mu.Unlock()
		if stop {
			break
		}
		select {
		case batches <- paths[start:min(start+ReadBatch, len(paths))]:
		case <-ctx.Done():
			break feed
		}
	}
	close(batches)
	wg.Wait()
	if fatal == nil {
		fatal = ctx.Err()
	}
	return rows, failed, fatal
}

// readBatch is one lineage: the batch, its resend, then single-file reads.
func (p *Pool) readBatch(ctx context.Context, batch, tags []string, numeric bool) ([]map[string]any, map[string]string, error) {
	args, err := readArgs(batch, tags, numeric)
	if err != nil {
		return nil, nil, err
	}
	// ExifTool reads only metadata, but antivirus may scan each whole file
	// as it opens, so big videos get more time here too.
	var size int64
	for _, path := range batch {
		size += fileSize(path)
	}
	r, err := p.try(args, size)
	var pe *ProcessError
	if !isStop(err) && errors.As(err, &pe) && !pe.TimedOut {
		r, err = p.try(args, size)
	}
	if err == nil {
		p.settle(outcome{})
		got, perr := parseRows(r.Out)
		if perr != nil {
			return nil, failedAll(batch, perr.Error()), nil
		}
		return got, nil, nil
	}
	if isStop(err) || !errors.As(err, &pe) {
		return nil, nil, err
	}
	var got []map[string]any
	failed := map[string]string{}
	anyOK, anyTimeout := false, pe.TimedOut
	// Read one file at a time. When ExifTool fails on several files in a
	// row, the trouble is not one file but the disk or ExifTool itself: the
	// rest of the batch is given up, so a hang on every file costs a few
	// timeouts per batch, not one per file.
	inARow := 0
	for i, path := range batch {
		if ctx.Err() != nil {
			return got, failed, ctx.Err()
		}
		if inARow >= p.o.MaxCrashRun {
			why := fmt.Sprintf("not read: ExifTool failed on the %d files before it", inARow)
			for _, rest := range batch[i:] {
				failed[rest] = why
			}
			break
		}
		one, _ := readArgs([]string{path}, tags, numeric)
		r, err := p.try(one, fileSize(path))
		switch {
		case err == nil:
			anyOK, inARow = true, 0
			rs, perr := parseRows(r.Out)
			if perr != nil {
				failed[path] = perr.Error()
			}
			got = append(got, rs...)
		case !isStop(err) && errors.As(err, &pe):
			anyTimeout = anyTimeout || pe.TimedOut
			failed[path] = pe.Error()
			inARow++
		default:
			return got, failed, err
		}
	}
	p.settle(outcome{failed: !anyOK, timedOut: !anyOK && anyTimeout})
	return got, failed, p.stopped()
}

// isStop reports the errors that end every call: Kill and a broken pool.
func isStop(err error) bool {
	return errors.Is(err, ErrKilled) || errors.Is(err, ErrPoolBroken)
}

func fileSize(path string) int64 {
	if st, err := os.Stat(path); err == nil {
		return st.Size()
	}
	return 0
}

func failedAll(paths []string, why string) map[string]string {
	m := make(map[string]string, len(paths))
	for _, p := range paths {
		m[p] = why
	}
	return m
}

// fileArgs picks the file paths out of a command, for the log.
func fileArgs(args []string) []string {
	var out []string
	for _, a := range args {
		if a != "" && !strings.HasPrefix(a, "-") && (strings.ContainsRune(a, '/') || strings.ContainsRune(a, '\\')) {
			out = append(out, a)
		}
	}
	return out
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if len(s) > 500 {
		s = s[:500]
	}
	return s
}
