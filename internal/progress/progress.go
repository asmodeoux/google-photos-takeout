package progress

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

	// State file for "takeout status": written whether or not the run is
	// quiet, at most once a second on change and every heartbeat.
	state      string
	done       int
	total      int
	written    time.Time
	stateErr   bool
	stopBeat   chan struct{}
	beatPeriod time.Duration
}

// State is what progress.json holds.
type State struct {
	Phase     string    `json:"phase"`
	Done      int       `json:"done"`
	Total     int       `json:"total"`
	Pid       int       `json:"pid"`
	UpdatedAt time.Time `json:"updated_at"`
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

// SetStateFile starts writing progress to path, a JSON State, so another
// process can show it. A heartbeat rewrites it while the run is alive.
func (r *Reporter) SetStateFile(path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state = path
	if r.beatPeriod == 0 {
		r.beatPeriod = 30 * time.Second
	}
	r.stopBeat = make(chan struct{})
	go func(stop chan struct{}, period time.Duration) {
		t := time.NewTicker(period)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				r.mu.Lock()
				r.writeState(true)
				r.mu.Unlock()
			}
		}
	}(r.stopBeat, r.beatPeriod)
}

// Finish stops the heartbeat. remove deletes the state file: the run ended.
// A run that stopped early keeps it, so status can say where.
func (r *Reporter) Finish(remove bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state == "" {
		return
	}
	close(r.stopBeat)
	if remove {
		_ = os.Remove(r.state)
	} else {
		r.writeState(true)
	}
	r.state = ""
}

// writeState writes the state file, at most once a second unless forced.
// Writing is best-effort: a failure never stops the run. r.mu is held.
func (r *Reporter) writeState(force bool) {
	// Before the first phase there is nothing to say, and a run that stops
	// in preflight must not create the results folder.
	if r.state == "" || r.phase == "" {
		return
	}
	now := time.Now()
	if !force && now.Sub(r.written) < time.Second {
		return
	}
	r.written = now
	b, _ := json.Marshal(State{Phase: r.phase, Done: r.done, Total: r.total, Pid: os.Getpid(), UpdatedAt: now.UTC()})
	err := os.MkdirAll(filepath.Dir(r.state), 0o755)
	if err == nil {
		tmp := r.state + ".tmp"
		if err = os.WriteFile(tmp, b, 0o644); err == nil {
			if err = os.Rename(tmp, r.state); err != nil {
				// Windows refuses to replace a file that antivirus or a
				// reader has open. Write in place instead: ReadState
				// ignores a file caught half-written.
				_ = os.Remove(tmp)
				err = os.WriteFile(r.state, b, 0o644)
			}
		}
	}
	if err != nil && !r.stateErr && !r.quiet {
		r.stateErr = true
		fmt.Fprintf(r.out, "note: cannot update %s (%v); takeout status will show less\n", r.state, err)
	}
}

// ReadState reads a state file. A missing, partial or unreadable file is
// reported as absent.
func ReadState(path string) (State, bool) {
	var st State
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &st) != nil || st.Phase == "" {
		return State{}, false
	}
	return st, true
}

func (r *Reporter) Phase(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.phase = s
	r.done, r.total = 0, 0
	r.writeState(true)
	if r.quiet {
		return
	}
	fmt.Fprintln(r.out, s)
}

func (r *Reporter) Tick(done, total int, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.done, r.total = done, total
	r.writeState(done == total)
	if r.quiet {
		return
	}
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
