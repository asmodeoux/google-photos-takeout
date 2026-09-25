package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/width"
)

// Reporter prints a live line on a terminal and plain lines otherwise.
type Reporter struct {
	out   io.Writer
	plain bool
	quiet bool
	mu    sync.Mutex
	last  time.Time
	phase string
	// width returns the terminal width in columns, 0 when unknown.
	width func() int
	prev  int // display width of the last live line
}

func New(out io.Writer, mode string, quiet bool) *Reporter {
	plain := mode == "plain" || (mode != "tty" && !isatty(out))
	if mode == "tty" {
		plain = false
	}
	return &Reporter{out: out, plain: plain, quiet: quiet, width: func() int { return termWidth(out) }}
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
	fmt.Fprint(r.out, "\r"+r.fit(strings.TrimRight(line, " ")))
	if done == total {
		fmt.Fprintln(r.out)
		r.prev = 0
	}
}

// fit cuts a live line to one column less than the terminal, so it never
// wraps, and pads it with spaces over whatever the previous line left.
func (r *Reporter) fit(line string) string {
	cols := 80
	if r.width != nil {
		if w := r.width(); w > 0 {
			cols = w
		}
	}
	max := cols - 1
	var b strings.Builder
	n := 0
	for _, c := range line {
		w := runeWidth(c)
		if n+w > max {
			break
		}
		b.WriteRune(c)
		n += w
	}
	if n < r.prev {
		b.WriteString(strings.Repeat(" ", r.prev-n))
	}
	r.prev = n
	return b.String()
}

// runeWidth is the number of terminal columns a character takes: two for
// East Asian wide and fullwidth characters, none for combining marks.
func runeWidth(c rune) int {
	if unicode.Is(unicode.Mn, c) || unicode.Is(unicode.Me, c) {
		return 0
	}
	switch width.LookupRune(c).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}
