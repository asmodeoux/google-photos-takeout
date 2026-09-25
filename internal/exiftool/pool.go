package exiftool

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
}

// Reply is what one -execute block printed.
type Reply struct {
	Out string // stdout, without the {readyN} line
	Err string // stderr, without the {errdoneN} line, at most maxStderr bytes
}

// Start launches exiftool -stay_open.
func Start(bin string) (*Client, error) {
	if bin == "" {
		bin = "exiftool"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("exiftool not found. Install it with: brew install exiftool")
	}
	cmd := exec.Command(bin, "-stay_open", "True", "-@", "-", "-common_args", "-charset", "filename=utf8")
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
	c.path = bin
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

// Close ends the stay_open process.
func (c *Client) Close() {
	if c == nil || c.in == nil {
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

// Look returns the exiftool path or an error with the install hint.
func Look(bin string) (string, error) {
	if bin == "" {
		bin = "exiftool"
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("exiftool not found. Install it with: brew install exiftool")
	}
	return p, nil
}

// Version runs exiftool -ver.
func Version(bin string) (string, error) {
	p, err := Look(bin)
	if err != nil {
		return "", err
	}
	out, err := exec.Command(p, "-ver").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// Within reports whether path is inside root. Used to refuse writes outside results/.
func Within(root, path string) bool {
	root = filepath.Clean(root) + string(os.PathSeparator)
	path = filepath.Clean(path)
	return strings.HasPrefix(path+string(os.PathSeparator), root) || path == filepath.Clean(strings.TrimSuffix(root, string(os.PathSeparator)))
}
