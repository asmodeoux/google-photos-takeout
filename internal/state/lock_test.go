package state

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Lock(dir)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	if !strings.Contains(err.Error(), "process") {
		t.Errorf("the message should name the holder: %v", err)
	}
	release()
	again, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

// Leftovers of a crash, such as an empty or half-written lock file, or one
// naming a process that is gone, do not keep the folder locked.
func TestLeftoverLockFilesDoNotBlock(t *testing.T) {
	for _, content := range []string{"", "{", `{"pid":999999,"host":"elsewhere"}`} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "lock"), []byte(content), 0o644)
		release, err := Lock(dir)
		if err != nil {
			t.Fatalf("content %q: %v", content, err)
		}
		release()
	}
}

func TestConcurrentLockHasOneWinner(t *testing.T) {
	for round := 0; round < 20; round++ {
		dir := t.TempDir()
		var wg sync.WaitGroup
		var winners atomic.Int32
		start := make(chan struct{})
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				if _, err := Lock(dir); err == nil {
					winners.Add(1)
				} else if !errors.Is(err, ErrLocked) {
					t.Error(err)
				}
			}()
		}
		close(start)
		wg.Wait()
		if n := winners.Load(); n != 1 {
			t.Fatalf("round %d: %d runs got the lock", round, n)
		}
	}
}

// A run that is killed releases the lock with it.
func TestKilledHolderReleasesLock(t *testing.T) {
	if os.Getenv("TAKEOUT_LOCK_HELPER") != "" {
		if _, err := Lock(os.Getenv("TAKEOUT_LOCK_HELPER")); err != nil {
			os.Exit(3)
		}
		os.Stdout.WriteString("locked\n")
		select {}
	}
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestKilledHolderReleasesLock$")
	cmd.Env = append(os.Environ(), "TAKEOUT_LOCK_HELPER="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(out).ReadString('\n')
	if strings.TrimSpace(line) != "locked" {
		cmd.Process.Kill()
		t.Fatalf("helper said %q", line)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrLocked) {
		cmd.Process.Kill()
		t.Fatalf("lock held by another process: %v", err)
	}
	cmd.Process.Kill()
	cmd.Wait()
	release, err := Lock(dir)
	if err != nil {
		t.Fatalf("lock of a killed process: %v", err)
	}
	release()
}
