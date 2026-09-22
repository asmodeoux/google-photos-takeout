package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Reporter prints a live line on a terminal and plain lines otherwise.
type Reporter struct {
	out   io.Writer
	plain bool
	quiet bool
	mu    sync.Mutex
	last  time.Time
	phase string
}

func New(out io.Writer, mode string, quiet bool) *Reporter {
	plain := mode == "plain" || (mode != "tty" && !isatty(out))
	if mode == "tty" {
		plain = false
	}
	return &Reporter{out: out, plain: plain, quiet: quiet}
}

func isatty(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return fdIsatty(int(f.Fd()))
}

func (r *Reporter) Phase(s string) {
	if r.quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = s
	fmt.Fprintln(r.out, s)
}

func (r *Reporter) Tick(done, total int, detail string) {
	if r.quiet {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	if r.plain && now.Sub(r.last) < 30*time.Second && done != total && done != 0 {
		return
	}
	r.last = now
	line := fmt.Sprintf("%s  %d/%d  %s", r.phase, done, total, detail)
	if r.plain {
		fmt.Fprintln(r.out, line)
		return
	}
	fmt.Fprintf(r.out, "\r%s", strings.TrimRight(line, " "))
	if done == total {
		fmt.Fprintln(r.out)
	}
}
