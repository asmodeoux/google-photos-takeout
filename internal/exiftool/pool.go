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

// Client is one long-lived ExifTool process.
type Client struct {
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	mu   sync.Mutex
	path string
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
	// Stderr must be drained. A full pipe blocks ExifTool, which then never
	// prints {ready} and the whole run stalls.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Client{cmd: cmd, in: stdin, out: bufio.NewReader(stdout), path: bin}, nil
}

// Exec sends one file's arguments and waits for {readyID}.
func (c *Client) Exec(args []string, id int) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range args {
		if _, err := io.WriteString(c.in, a+"\n"); err != nil {
			return "", err
		}
	}
	if _, err := io.WriteString(c.in, "-execute"+strconv.Itoa(id)+"\n"); err != nil {
		return "", err
	}
	var b strings.Builder
	ready := "{ready" + strconv.Itoa(id) + "}"
	for {
		line, err := c.out.ReadString('\n')
		if len(line) > 0 {
			b.WriteString(line)
		}
		if strings.Contains(b.String(), ready) {
			break
		}
		if err != nil {
			return b.String(), fmt.Errorf("exiftool: %w", err)
		}
	}
	// stderr is line-buffered; read what is already there without blocking forever.
	return b.String(), nil
}

func drain(r *bufio.Reader) string {
	var b strings.Builder
	for r.Buffered() > 0 {
		line, err := r.ReadString('\n')
		b.WriteString(line)
		if err != nil {
			break
		}
	}
	return b.String()
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
	_ = c.cmd.Wait()
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
