//go:build !windows

package media

import (
	"os"
	"path/filepath"
	"testing"
)

// Once the new name exists, a source that cannot be removed must not turn the
// move into a failure: the caller would move the file again under a new name.
func TestRenameSucceedsWhenSourceCannotBeRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove files from a read-only folder")
	}
	src := filepath.Join(t.TempDir(), "locked")
	os.MkdirAll(src, 0o755)
	a := filepath.Join(src, "a.jpg")
	os.WriteFile(a, []byte("x"), 0o644)
	os.Chmod(src, 0o555)
	defer os.Chmod(src, 0o755)
	dst := filepath.Join(t.TempDir(), "a.jpg")
	if err := Rename(a, dst); err != nil {
		t.Fatalf("rename reported %v although %s exists", err, dst)
	}
	if b, _ := os.ReadFile(dst); string(b) != "x" {
		t.Fatal("destination content")
	}
}
