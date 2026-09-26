package exiftool

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/proc"
)

// maxStderr caps the stderr text kept for one command.
const maxStderr = 8 << 10

// Client is one long-lived ExifTool process.
type Client struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Reader
	errs    *lineQueue // stderr lines
	mu      sync.Mutex
	path    string
	dead    atomic.Bool // set by Kill; Close must not wait on a stuck command
	started time.Time   // when the process started, for KillTree
	exited  chan struct{}
	pipes   []*os.File // parent ends of stdout and stderr, closed when the client is done
	closed  sync.Once
	state   atomic.Int64 // command number << 2 | cmdIdle, cmdRunning or cmdTimedOut
	seq     int64        // command number, under mu
}

// Command states. A timeout and the command's own completion race to leave
// cmdRunning; whichever wins decides the outcome and the other does nothing.
// The state carries the command number too, so a timer that fires late for
// one command can never stop the next one on the same process.
const (
	cmdIdle int64 = iota
	cmdRunning
	cmdTimedOut
)

// Reply is what one -execute block printed.
type Reply struct {
	Out string // stdout, without the {readyN} line
	Err string // stderr, without the {errdoneN} line, at most maxStderr bytes
}

// ProcessError means the ExifTool process itself failed during a command: it
// exited, its pipes broke, or it did not answer in time. ExifTool's own
// per-file errors are replies, not ProcessErrors.
type ProcessError struct {
	TimedOut bool
	Timeout  time.Duration
	Stderr   string // what the process printed before it failed, capped
	Err      error
}

func (e *ProcessError) Error() string {
	if e.TimedOut {
		return fmt.Sprintf("exiftool did not answer within %s", e.Timeout.Round(time.Second))
	}
	return "exiftool stopped: " + e.Err.Error()
}

func (e *ProcessError) Unwrap() error { return e.Err }

// Start launches exiftool -stay_open. Its stdout and stderr are ordinary pipes
// read until end of file, and a goroutine waits for the process: when it
// exits, anything it started is killed too, so the pipes close and a command
// in flight fails at once instead of hanging. On Windows that matters because
// exiftool.exe runs perl.exe, which inherits the pipes.
func Start(bin string) (*Client, error) {
	path, err := Look(bin)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path, "-stay_open", "True", "-@", "-", "-common_args", "-charset", "filename=utf8")
	proc.Isolate(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		outR.Close()
		outW.Close()
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = outW, errW
	started := time.Now()
	err = cmd.Start()
	outW.Close()
	errW.Close()
	if err != nil {
		outR.Close()
		errR.Close()
		stdin.Close()
		return nil, err
	}
	c := newClient(stdin, outR, errR)
	c.cmd = cmd
	c.path = path
	c.started = started
	c.pipes = []*os.File{outR, errR}
	c.exited = make(chan struct{})
	go func() {
		_ = cmd.Wait()
		proc.KillOrphans(cmd, started)
		close(c.exited)
	}()
	return c, nil
}

// Pid is the process id, or 0 for a client without a process.
func (c *Client) Pid() int {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return 0
	}
	return c.cmd.Process.Pid
}

// newClient wires the three streams. Stderr is read in a goroutine for the
// whole life of the process and that goroutine never blocks: ExifTool may
// write any amount of stderr before the {readyN} line Run waits for.
func newClient(in io.WriteCloser, out, errOut io.Reader) *Client {
	c := &Client{in: in, out: bufio.NewReader(out), errs: newLineQueue()}
	go func() {
		br := bufio.NewReaderSize(errOut, 64<<10)
		for {
			line, err := readLine(br)
			if line != "" || err == nil {
				c.errs.push(line)
			}
			if err != nil {
				break
			}
		}
		c.errs.close()
	}()
	return c
}

// readLine reads one line without its newline. A line longer than the reader's
// buffer is cut there and the rest of it is skipped.
func readLine(br *bufio.Reader) (string, error) {
	b, err := br.ReadSlice('\n')
	line := strings.TrimRight(string(b), "\r\n")
	for err == bufio.ErrBufferFull {
		_, err = br.ReadSlice('\n')
	}
	return line, err
}

// lineQueue holds stderr lines until Run takes them. It keeps at most
// maxStderr bytes of ordinary lines; marker lines are always kept.
type lineQueue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	lines  []string
	size   int
	closed bool
}

func newLineQueue() *lineQueue {
	q := &lineQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *lineQueue) push(line string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	marker := strings.HasPrefix(strings.TrimSpace(line), "{errdone")
	if !marker && q.size >= maxStderr {
		return
	}
	if !marker {
		q.size += len(line)
	}
	q.lines = append(q.lines, line)
	q.cond.Broadcast()
}

func (q *lineQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}

// drain takes the ordinary lines queued so far without waiting, for the
// message of a command whose process failed.
func (q *lineQueue) drain() string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var b strings.Builder
	for _, l := range q.lines {
		if !strings.HasPrefix(strings.TrimSpace(l), "{errdone") && b.Len() < maxStderr {
			b.WriteString(l)
			b.WriteByte('\n')
		}
	}
	q.lines, q.size = nil, 0
	return b.String()
}

// next waits for a line. ok is false once stderr has ended and is empty.
func (q *lineQueue) next() (line string, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.lines) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.lines) == 0 {
		return "", false
	}
	line = q.lines[0]
	q.lines = q.lines[1:]
	if !strings.HasPrefix(strings.TrimSpace(line), "{errdone") {
		q.size -= len(line)
	}
	return line, true
}

// Run sends one block of arguments and waits until both {readyN} on stdout and
// {errdoneN} on stderr arrive, so no output leaks into the next block.
func (c *Client) Run(args []string, id int) (Reply, error) {
	return c.run(args, id, 0)
}

// run is Run with a time limit; 0 means none. When the limit passes, the
// process and everything it started are killed and run returns a
// ProcessError with TimedOut set.
func (c *Client) run(args []string, id int, timeout time.Duration) (Reply, error) {
	if err := CheckArgs(args); err != nil {
		return Reply{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	running, idle, timedOut := c.seq<<2|cmdRunning, c.seq<<2|cmdIdle, c.seq<<2|cmdTimedOut
	c.state.Store(running)
	if timeout > 0 {
		t := time.AfterFunc(timeout, func() {
			if c.state.CompareAndSwap(running, timedOut) {
				c.killTree()
			}
		})
		defer t.Stop()
	}
	r, err := c.exchange(args, id)
	if !c.state.CompareAndSwap(running, idle) {
		return r, &ProcessError{TimedOut: true, Timeout: timeout, Stderr: r.Err, Err: fmt.Errorf("timed out")}
	}
	return r, err
}

// exchange writes one block and reads its reply. The caller holds c.mu.
func (c *Client) exchange(args []string, id int) (Reply, error) {
	n := strconv.Itoa(id)
	var w strings.Builder
	for _, a := range args {
		w.WriteString(a)
		w.WriteByte('\n')
	}
	w.WriteString("-echo4\n{errdone" + n + "}\n-execute" + n + "\n")
	if _, err := io.WriteString(c.in, w.String()); err != nil {
		return Reply{}, &ProcessError{Err: err}
	}
	var r Reply
	ready := "{ready" + n + "}"
	var out strings.Builder
	for {
		line, err := c.out.ReadString('\n')
		if strings.TrimSpace(line) == ready {
			break
		}
		out.WriteString(line)
		if err != nil {
			stderr := c.errs.drain()
			return Reply{Out: out.String(), Err: stderr}, &ProcessError{Stderr: stderr, Err: err}
		}
	}
	r.Out = out.String()
	done := "{errdone" + n + "}"
	var errb strings.Builder
	for {
		line, ok := c.errs.next()
		if !ok {
			break
		}
		if strings.TrimSpace(line) == done {
			r.Err = errb.String()
			return r, nil
		}
		if errb.Len() < maxStderr {
			errb.WriteString(line)
			errb.WriteByte('\n')
		}
	}
	r.Err = errb.String()
	return r, &ProcessError{Stderr: r.Err, Err: fmt.Errorf("stderr closed before %s", done)}
}

// killTree kills the process and everything it started. Once the process
// has exited, the watcher already did that, and its id may belong to
// another process by now.
func (c *Client) killTree() {
	if c.cmd == nil || c.gone() {
		if c.in != nil {
			_ = c.in.Close()
		}
		return
	}
	proc.KillTree(c.cmd, c.started)
	if c.in != nil {
		_ = c.in.Close()
	}
}

// Kill stops the process at once. A command in flight returns an error.
func (c *Client) Kill() {
	if c == nil {
		return
	}
	c.dead.Store(true)
	c.killTree()
	c.release()
}

// closeWait is how long Close waits for ExifTool to exit after being asked.
var closeWait = 10 * time.Second

// Close ends the stay_open process.
func (c *Client) Close() {
	if c == nil || c.in == nil || c.dead.Load() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = io.WriteString(c.in, "-stay_open\nFalse\n")
	_ = c.in.Close()
	if c.exited != nil {
		select {
		case <-c.exited:
		case <-time.After(closeWait):
			c.killTree()
			<-c.exited
		}
	}
	c.release()
}

// gone reports whether the process has exited and been waited for.
func (c *Client) gone() bool {
	if c.exited == nil {
		return false
	}
	select {
	case <-c.exited:
		return true
	default:
		return false
	}
}

// release closes the parent ends of the pipes once the process is gone.
func (c *Client) release() {
	c.closed.Do(func() {
		if c.exited != nil {
			<-c.exited
		}
		for _, f := range c.pipes {
			_ = f.Close()
		}
	})
}

// MinWindowsVersion is the first ExifTool that reads and writes long and
// non-ASCII paths on Windows by default (WindowsLongPath with wide characters).
const MinWindowsVersion = "13.07"

// Look finds ExifTool: the --exiftool value, then PATH, then a copy next to
// the takeout program. On Windows the copy next to takeout counts only when
// its exiftool_files folder sits beside it, as in the official download, so a
// stray exiftool.exe in Downloads is not picked up.
func Look(bin string) (string, error) {
	return look(bin, runtime.GOOS)
}

func look(bin, goos string) (string, error) {
	if bin != "" {
		if err := checkNotKeypress(bin); err != nil {
			return "", err
		}
		p, err := exec.LookPath(bin)
		if err != nil {
			return "", &LookError{Problem: "ExifTool not found", Where: bin, Fix: InstallHint(goos)}
		}
		return p, nil
	}
	if p, err := exec.LookPath("exiftool"); err == nil {
		return p, nil
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		name := "exiftool"
		if goos == "windows" {
			name = "exiftool.exe"
		}
		cand := filepath.Join(dir, name)
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			if goos != "windows" {
				return cand, nil
			}
			if st, err := os.Stat(filepath.Join(dir, "exiftool_files")); err == nil && st.IsDir() {
				return cand, nil
			}
		}
		if goos == "windows" {
			if _, err := os.Stat(filepath.Join(dir, "exiftool(-k).exe")); err == nil {
				return "", keypressError(filepath.Join(dir, "exiftool(-k).exe"))
			}
		}
	}
	return "", &LookError{Problem: "ExifTool not found", Where: "PATH and the takeout folder", Fix: InstallHint(goos)}
}

// LookError says why ExifTool could not be used and how to fix it.
type LookError struct {
	Problem string
	Where   string // the path or places looked at
	Fix     string
}

func (e *LookError) Error() string {
	return e.Problem + " at " + e.Where + ". " + e.Fix
}

// checkNotKeypress refuses the "exiftool(-k).exe" build from the Windows zip,
// which waits for a key press after every command and would hang takeout.
func checkNotKeypress(p string) error {
	if strings.Contains(strings.ToLower(filepath.Base(p)), "(-k)") {
		return keypressError(p)
	}
	return nil
}

func keypressError(p string) error {
	return &LookError{
		Problem: "this ExifTool waits for a key press and cannot run in batch mode",
		Where:   p,
		Fix:     "Rename exiftool(-k).exe to exiftool.exe and keep the exiftool_files folder next to it",
	}
}

// InstallHint is the install command for the current kind of system.
func InstallHint(goos string) string {
	switch goos {
	case "windows":
		return "Install it with: winget install --id OliverBetz.ExifTool -e (then open a new PowerShell window), or download the Windows zip from exiftool.org and rename exiftool(-k).exe to exiftool.exe"
	case "darwin":
		return "Install it with: brew install exiftool"
	default:
		return "Install it with: sudo apt install libimage-exiftool-perl (or your distribution's exiftool package)"
	}
}

// versionTimeout allows for a cold start while antivirus scans ExifTool's
// bundled Perl on Windows.
var versionTimeout = 45 * time.Second

// Version runs exiftool -ver.
func Version(bin string) (string, error) {
	p, err := Look(bin)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p, "-ver")
	proc.Isolate(cmd)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return "", fmt.Errorf("exiftool at %s did not answer within %s", p, versionTimeout)
	}
	if err != nil {
		return "", fmt.Errorf("exiftool at %s: %w", p, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// AtLeast compares ExifTool versions such as "13.07" and "9.5" as numbers.
func AtLeast(have, want string) bool {
	hm, hn := splitVersion(have)
	wm, wn := splitVersion(want)
	if hm != wm {
		return hm > wm
	}
	return hn >= wn
}

func splitVersion(v string) (int, int) {
	major, minor, _ := strings.Cut(strings.TrimSpace(v), ".")
	a, _ := strconv.Atoi(major)
	b, _ := strconv.Atoi(minor)
	return a, b
}

// Within reports whether path is inside root. Used to refuse writes outside results/.
func Within(root, path string) bool {
	root = filepath.Clean(root) + string(os.PathSeparator)
	path = filepath.Clean(path)
	return strings.HasPrefix(path+string(os.PathSeparator), root) || path == filepath.Clean(strings.TrimSuffix(root, string(os.PathSeparator)))
}
