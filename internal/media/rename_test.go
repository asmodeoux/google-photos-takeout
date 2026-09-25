package media

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRenameNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)
	err := Rename(src, dst)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("want ErrExist, got %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "old" {
		t.Fatal("existing file was replaced")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal("source must stay when the rename is refused")
	}
}

func TestRenameMoves(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "Фото é.jpg")
	os.WriteFile(src, []byte("x"), 0o644)
	if err := Rename(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("source still there")
	}
	if b, _ := os.ReadFile(dst); string(b) != "x" {
		t.Fatal("content")
	}
}

func TestCopyNeverReplacesAndLeavesNoPartial(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "a.jpg"), filepath.Join(dir, "b.jpg")
	os.WriteFile(src, []byte("new"), 0o644)
	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	if err := Copy(src, dst); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second copy: %v", err)
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 2 {
		t.Fatalf("files left: %d", len(ents))
	}
}
