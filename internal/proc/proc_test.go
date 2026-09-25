package proc

import (
	"bufio"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// helper runs inside a child copy of the test binary. It starts a long-lived
// grandchild, prints the grandchild's pid, and waits.
func TestMain(m *testing.M) {
	if os.Getenv("TAKEOUT_PROC_HELPER") == "1" {
		if os.Getenv("TAKEOUT_PROC_JOB") == "1" {
			KillTreeOnExit()
		}
		gc := exec.Command(os.Args[0])
		gc.Env = append(os.Environ(), "TAKEOUT_PROC_HELPER=sleep")
		if err := gc.Start(); err != nil {
			os.Exit(3)
		}
		os.Stdout.WriteString(strconv.Itoa(gc.Process.Pid) + "\n")
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if os.Getenv("TAKEOUT_PROC_HELPER") == "sleep" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startHelper starts the helper and returns it with its grandchild's pid.
func startHelper(t *testing.T, env ...string) (*exec.Cmd, int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(append(os.Environ(), "TAKEOUT_PROC_HELPER=1"), env...)
	Isolate(cmd)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		cmd.Process.Kill()
		t.Fatalf("helper printed %q", line)
	}
	return cmd, pid
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !alive(pid) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	killPid(pid)
	t.Fatalf("process %d outlived its parent", pid)
}
