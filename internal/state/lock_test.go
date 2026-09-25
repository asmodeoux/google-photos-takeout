package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLockIsExclusive(t *testing.T) {
	dir := t.TempDir()
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("second lock: %v", err)
	}
	release()
	again, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestStaleLockIsReplaced(t *testing.T) {
	dir := t.TempDir()
	host, _ := os.Hostname()
	b, _ := json.Marshal(lockInfo{PID: 999999, Host: host})
	os.WriteFile(filepath.Join(dir, "lock"), b, 0o644)
	release, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestLockFromOtherHostIsKept(t *testing.T) {
	dir := t.TempDir()
	b, _ := json.Marshal(lockInfo{PID: 1, Host: "another-machine"})
	os.WriteFile(filepath.Join(dir, "lock"), b, 0o644)
	if _, err := Lock(dir); !errors.Is(err, ErrLocked) {
		t.Fatalf("got %v", err)
	}
}
