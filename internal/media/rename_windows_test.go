package media

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Defender and the search indexer open new files without FILE_SHARE_DELETE
// for a moment; the move must wait for them instead of failing.
func TestRenameWaitsForAFileHeldOpen(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.jpg")
	os.WriteFile(src, []byte("x"), 0o644)
	name, _ := windows.UTF16PtrFromString(src)
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(300 * time.Millisecond)
		windows.CloseHandle(h)
	}()
	before := RenameRetries.Load()
	if err := Rename(src, filepath.Join(dir, "b.jpg")); err != nil {
		t.Fatalf("rename while held: %v", err)
	}
	if RenameRetries.Load() == before {
		t.Error("the held file did not cause a retry; the test did not hold it")
	}
}

func TestRenameNeverReplacesOnWindows(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "a.jpg"), filepath.Join(dir, "b.jpg")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)
	if err := Rename(src, dst); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("got %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "old" {
		t.Fatal("replaced")
	}
}
