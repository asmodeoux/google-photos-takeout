// Package fakeexif is a test double for ExifTool. It is imported only by
// tests: TestMain calls Main, which turns the test binary into a proxy that
// runs the real ExifTool and fails on purpose when a rule says so.
//
//	Pool ─stdin─► fake (this binary) ─stdin─► real exiftool
//	     ◄stdout─      │ rules          ◄stdout─
//	                   └─ state dir: rules.json, cmd/<n>, fired/<rule>, starts.log, kids.log
//
// The state lives in files because every restart is a new fake process: the
// command counter and one-shot rules must span them all.
package fakeexif

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	envReal    = "TAKEOUT_FAKE_EXIFTOOL"
	envState   = "TAKEOUT_FAKE_STATE"
	envSleeper = "TAKEOUT_FAKE_SLEEPER"
)

// Rules say when the fake fails. Command numbers count every -execute block
// across all fake processes, starting at 1.
type Rules struct {
	CrashOn           []int  // exit before running these commands
	CrashOnceOn       []int  // like CrashOn, but a retry of a command that crashed goes through
	CrashAfterReplyOn []int  // run these, then exit before passing on {readyN}
	Poison            string // exit before running any command that contains this; "|" separates alternatives
	Hang              string // never answer a command that contains this
	CrashAll          bool   // exit before running any command
	ExceptVersion     bool   // with CrashAll: "-ver" still works
	OrphanOn          int    // on this command, start a child that keeps stdout open, then exit
	DieAtStartAfter   int    // processes after this many exit at once
}

// Setup writes the rules and points the fake at the real ExifTool. It
// returns the binary to pass as the ExifTool path and the state directory.
func Setup(t testing.TB, r Rules) (bin, state string) {
	t.Helper()
	real, err := exec.LookPath("exiftool")
	if err != nil {
		t.Skip("exiftool not installed")
	}
	state = t.TempDir()
	for _, d := range []string{"cmd", "fired"} {
		if err := os.MkdirAll(filepath.Join(state, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := json.Marshal(r)
	if err := os.WriteFile(filepath.Join(state, "rules.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envReal, real)
	t.Setenv(envState, state)
	return os.Args[0], state
}

// Commands is the number of -execute blocks the fakes have received.
func Commands(state string) int {
	es, _ := os.ReadDir(filepath.Join(state, "cmd"))
	return len(es)
}

// Starts lists the pids of every fake process started.
func Starts(state string) []int { return pids(filepath.Join(state, "starts.log")) }

// Kids lists the pids of processes the fakes started: the real ExifTool
// and any orphan.
func Kids(state string) []int { return pids(filepath.Join(state, "kids.log")) }

func pids(file string) []int {
	b, _ := os.ReadFile(file)
	var out []int
	for _, f := range strings.Fields(string(b)) {
		if n, err := strconv.Atoi(f); err == nil {
			out = append(out, n)
		}
	}
	return out
}

// WaitGone fails the test if any pid is still running after the deadline.
func WaitGone(t testing.TB, pids []int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for _, pid := range pids {
		for alive(pid) {
			if time.Now().After(deadline) {
				t.Errorf("process %d is still running", pid)
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// Main runs the fake if the environment asks for it, and never returns then.
func Main() {
	if os.Getenv(envSleeper) == "1" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	real, state := os.Getenv(envReal), os.Getenv(envState)
	if real == "" || state == "" {
		return
	}
	os.Exit(run(real, state))
}

func run(real, state string) int {
	var r Rules
	if b, err := os.ReadFile(filepath.Join(state, "rules.json")); err == nil {
		_ = json.Unmarshal(b, &r)
	}
	appendLine(filepath.Join(state, "starts.log"), strconv.Itoa(os.Getpid()))
	if r.DieAtStartAfter > 0 && len(Starts(state)) > r.DieAtStartAfter {
		return 3
	}
	child := exec.Command(real, os.Args[1:]...)
	child.Stderr = os.Stderr
	in, err := child.StdinPipe()
	if err != nil {
		return 2
	}
	outPipe, err := child.StdoutPipe()
	if err != nil {
		return 2
	}
	if err := child.Start(); err != nil {
		return 2
	}
	appendLine(filepath.Join(state, "kids.log"), strconv.Itoa(child.Process.Pid))
	childOut := bufio.NewReader(outPipe)
	stdin := bufio.NewReader(os.Stdin)
	var block strings.Builder
	for {
		line, err := stdin.ReadString('\n')
		if line != "" && !strings.HasPrefix(line, "-execute") {
			block.WriteString(line)
			io.WriteString(in, line)
		}
		if strings.HasPrefix(line, "-execute") {
			n := nextCommand(state)
			text := block.String()
			block.Reset()
			switch {
			case r.Hang != "" && strings.Contains(text, r.Hang):
				time.Sleep(time.Minute)
				return 3
			case r.Poison != "" && containsAny(text, strings.Split(r.Poison, "|")),
				contains(r.CrashOn, n),
				r.CrashAll && !(r.ExceptVersion && strings.Contains(text, "-ver\n")):
				return 3
			case contains(r.CrashOnceOn, n) && firstCrash(state, text):
				return 3
			case n == r.OrphanOn:
				orphan(state)
				return 3
			}
			io.WriteString(in, line)
			ready := "{ready" + strings.TrimSpace(strings.TrimPrefix(line, "-execute")) + "}"
			for {
				out, err := childOut.ReadString('\n')
				if strings.TrimSpace(out) == ready && contains(r.CrashAfterReplyOn, n) {
					return 3
				}
				io.WriteString(os.Stdout, out)
				if strings.TrimSpace(out) == ready || err != nil {
					break
				}
			}
		}
		if err != nil {
			in.Close()
			child.Wait()
			return 0
		}
	}
}

// nextCommand claims the next command number across all fake processes by
// creating cmd/<n> exclusively; nothing to unlock if a fake is killed.
func nextCommand(state string) int {
	for n := Commands(state) + 1; ; n++ {
		f, err := os.OpenFile(filepath.Join(state, "cmd", strconv.Itoa(n)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return n
		}
		if !errors.Is(err, os.ErrExist) {
			return n
		}
	}
}

// firstCrash records a crash on this command text and says whether it is
// the first. The pool resends a crashed command, often after other workers'
// commands have taken the next numbers, so numbers alone cannot spare it.
// The {errdoneN} marker differs per attempt, so it is left out.
func firstCrash(state, text string) bool {
	var args []string
	for _, l := range strings.Split(text, "\n") {
		if !strings.HasPrefix(l, "{errdone") {
			args = append(args, l)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(args, "\n")))
	f, err := os.OpenFile(filepath.Join(state, "fired", "crash-"+hex.EncodeToString(sum[:8])), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// orphan starts a process that inherits stdout and outlives the fake, like
// perl.exe after exiftool.exe exits.
func orphan(state string) {
	c := exec.Command(os.Args[0])
	c.Env = append(os.Environ(), envSleeper+"=1")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if c.Start() == nil {
		appendLine(filepath.Join(state, "kids.log"), strconv.Itoa(c.Process.Pid))
	}
}

func appendLine(file, s string) {
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	fmt.Fprintln(f, s)
	f.Close()
}

func containsAny(s string, subs []string) bool {
	for _, x := range subs {
		if x != "" && strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func contains(xs []int, n int) bool {
	for _, x := range xs {
		if x == n {
			return true
		}
	}
	return false
}
