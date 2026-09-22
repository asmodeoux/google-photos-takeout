package zipindex

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestClassify(t *testing.T) {
	if Classify("Photos from 2016") != ClassLibrary {
		t.Fatal("year")
	}
	if Classify("Фото за 2014") != ClassLibrary {
		t.Fatal("ru year")
	}
	if Classify("Archive") != ClassLibrary || Classify("Failed Videos") != ClassLibrary || Classify("Locked Folder") != ClassLibrary {
		t.Fatal("system library folders")
	}
	if Classify("Trash") != ClassTrash || Classify("Bin") != ClassTrash {
		t.Fatal("trash")
	}
	if Classify("Trip(1)") != ClassAlbum {
		t.Fatal("album with suffix is still an album")
	}
	// NBSP between words
	if Classify("Photos\u00a0from 2018") != ClassLibrary {
		t.Fatal("nbsp year folder")
	}
}

func TestSniff(t *testing.T) {
	if Sniff([]byte{0xff, 0xd8, 0xff, 0xe0}) != "jpeg" {
		t.Fatal("jpeg")
	}
	png := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if Sniff(png) != "png" {
		t.Fatal("png")
	}
	webp := append([]byte("RIFF"), make([]byte, 4)...)
	webp = append(webp, []byte("WEBP")...)
	if Sniff(webp) != "webp" {
		t.Fatal("webp")
	}
	// ftyp + heic brand, but we pass jpeg bytes through the caller when the name says heic
	heic := make([]byte, 12)
	copy(heic[4:8], "ftyp")
	copy(heic[8:12], "heic")
	if Sniff(heic) != "heic" {
		t.Fatal("heic")
	}
	mkv := []byte{0x1a, 0x45, 0xdf, 0xa3}
	if Sniff(mkv) != "webm" {
		t.Fatal("webm")
	}
	mov := make([]byte, 12)
	copy(mov[4:8], "ftyp")
	copy(mov[8:12], "qt  ")
	if Sniff(mov) != "mov" {
		t.Fatal("mov")
	}
}

func TestZipSlipAndOpen(t *testing.T) {
	if err := CheckPath("Takeout/../../etc/passwd"); err == nil {
		t.Fatal("expected slip rejection")
	}
	dir := t.TempDir()
	zp := filepath.Join(dir, "takeout-20200101T000000Z-1-001.zip")
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	w, err := zw.Create("Takeout/Google Photos/Photos from 2016/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte{0xff, 0xd8, 0xff, 0xd9})
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zp, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err := Open([]string{zp})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 1 || idx.Entries[0].RelFolder != "Photos from 2016" {
		t.Fatalf("%+v", idx.Entries)
	}
	if len(idx.Missing) != 0 {
		t.Fatal(idx.Missing)
	}

	// gap: only part 2
	zp2 := filepath.Join(dir, "takeout-20200101T000000Z-1-002.zip")
	if err := os.WriteFile(zp2, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	idx, err = Open([]string{zp2})
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Missing) != 1 || idx.Missing[0] != 1 {
		t.Fatalf("missing %+v", idx.Missing)
	}
}

func TestBadZip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "broken.zip")
	if err := os.WriteFile(p, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open([]string{p})
	if err == nil {
		t.Fatal("expected error")
	}
}
