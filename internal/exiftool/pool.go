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
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	errs chan string // stderr lines, closed when stderr ends
	mu   sync.Mutex
	path string
	dead atomic.Bool // set by Kill; Close must not wait on a stuck command
}

// Reply is what one -execute block printed.
type Reply struct {
	Out string // stdout, without the {readyN} line
	Err string // stderr, without the {errdoneN} line, at most maxStderr bytes
}

// Start launches exiftool -stay_open.
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := newClient(stdin, stdout, stderr)
	c.cmd = cmd
	c.path = path
	return c, nil
}

// newClient wires the three streams. Stderr is read in a goroutine for the
// whole life of the process: a full stderr pipe would block ExifTool.
func newClient(in io.WriteCloser, out, errOut io.Reader) *Client {
	c := &Client{in: in, out: bufio.NewReader(out), errs: make(chan string, 256)}
	go func() {
		sc := bufio.NewScanner(errOut)
		sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
		for sc.Scan() {
			c.errs <- sc.Text()
		}
		close(c.errs)
	}()
	return c
}

// Exec sends one block of arguments and returns its stdout.
func (c *Client) Exec(args []string, id int) (string, error) {
	r, err := c.Run(args, id)
	return r.Out, err
}

// Run sends one block of arguments and waits until both {readyN} on stdout and
// {errdoneN} on stderr arrive, so no output leaks into the next block.
func (c *Client) Run(args []string, id int) (Reply, error) {
	if err := CheckArgs(args); err != nil {
		return Reply{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	n := strconv.Itoa(id)
	var w strings.Builder
	for _, a := range args {
		w.WriteString(a)
		w.WriteByte('\n')
	}
	w.WriteString("-echo4\n{errdone" + n + "}\n-execute" + n + "\n")
	if _, err := io.WriteString(c.in, w.String()); err != nil {
		return Reply{}, err
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
			return Reply{Out: out.String()}, fmt.Errorf("exiftool: %w", err)
		}
	}
	r.Out = out.String()
	done := "{errdone" + n + "}"
	var errb strings.Builder
	for line := range c.errs {
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
	return r, fmt.Errorf("exiftool: stderr closed before %s", done)
}

// Kill stops the process at once. A command in flight returns an error.
func (c *Client) Kill() {
	if c == nil {
		return
	}
	c.dead.Store(true)
	proc.Kill(c.cmd)
	if c.in != nil {
		_ = c.in.Close()
	}
}

// Close ends the stay_open process.
func (c *Client) Close() {
	if c == nil || c.in == nil || c.dead.Load() {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _ = io.WriteString(c.in, "-stay_open\nFalse\n")
	_ = c.in.Close()
	if c.cmd != nil {
		_ = c.cmd.Wait()
	}
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
