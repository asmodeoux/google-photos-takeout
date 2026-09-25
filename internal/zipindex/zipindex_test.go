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
	for head, want := range map[string]string{
		"II*\x00\x08\x00\x00\x00":                         "tiff",
		"MM\x00*\x00\x00\x00\x08":                         "tiff",
		"IIRO\x08\x00\x00\x00":                            "raw",
		"IIU\x00\x08\x00\x00\x00":                         "raw",
		"FUJIFILMCCD-RAW 0201":                            "raw",
		"\x00\x00\x00\x18ftypcrx \x00\x00":                "cr3",
		"\x00\x00\x00\x18ftypisom\x00\x00":                "mp4",
		"RIFF\x10\x00\x00\x00AVI LIST":                    "avi",
		"RIFF\x10\x00\x00\x00WEBPVP8 ":                    "webp",
		"\x00\x00\x01\xba\x44\x00":                        "mpg",
		"\x30\x26\xb2\x75\x8e\x66\xcf\x11":                "wmv",
		"BM:\x00\x00\x00\x00\x00\x00\x00\x36\x00\x00\x00": "bmp",
		"BMW text file":                                   "unknown",
	} {
		if got := Sniff([]byte(head)); got != want {
			t.Errorf("Sniff(%q) = %s, want %s", head, got, want)
		}
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

func TestMissingPartsPerExport(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for _, name := range []string{
		"takeout-20240101T000000Z-1-001.zip", "takeout-20240101T000000Z-1-002.zip",
		"takeout-20250101T000000Z-1-001.zip", "takeout-20250101T000000Z-1-003.zip",
	} {
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, _ := zw.Create("Takeout/Google Photos/Photos from 2019/" + name + ".jpg")
		w.Write([]byte("x"))
		zw.Close()
		f.Close()
		paths = append(paths, p)
	}
	idx, err := Open(paths)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.ExportIDs) != 2 {
		t.Fatalf("exports %v", idx.ExportIDs)
	}
	if got := idx.MissingByExport["20250101T000000Z"]; len(got) != 1 || got[0] != 2 {
		t.Fatalf("missing %v", idx.MissingByExport)
	}
	if _, ok := idx.MissingByExport["20240101T000000Z"]; ok {
		t.Fatal("complete export reported missing")
	}
}

func TestFallbackRootForUnknownLanguage(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "takeout-20240101T000000Z-1-001.zip")
	f, _ := os.Create(p)
	zw := zip.NewWriter(f)
	for _, name := range []string{
		"Takeout/Fotoj Google/Fotoj de 2019/a.jpg",
		"Takeout/Fotoj Google/Fotoj de 2019/a.jpg.supplemental-metadata.json",
		"Takeout/Fotoj Google/Ferioj/b.jpg",
		"Takeout/Mail/inbox.mbox",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(name))
	}
	zw.Close()
	f.Close()
	idx, err := Open([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if idx.FallbackRoot != "Fotoj Google" || len(idx.Entries) != 3 {
		t.Fatalf("root %q entries %d", idx.FallbackRoot, len(idx.Entries))
	}
	folders := map[string]bool{}
	for _, e := range idx.Entries {
		folders[e.RelFolder] = true
	}
	if !folders["Photos from 2019"] || !folders["Ferioj"] {
		t.Fatalf("folders %v", folders)
	}
	for in, want := range map[string]string{"Fotoj de 2019": "2019", "2019 fotoj": "2019", "Ferioj": "", "Paris2019": "", "Trip 20190": ""} {
		if got := genericYear(in); got != want {
			t.Errorf("genericYear(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsSystemFile(t *testing.T) {
	for name, want := range map[string]bool{
		"Takeout/Google Photos/Photos from 2019/._a.jpg":        true,
		"Takeout/Google Photos/Photos from 2019/.DS_Store":      true,
		"Takeout/Google Photos/Trip/Thumbs.db":                  true,
		"Takeout/Google Photos/Trip/Desktop.ini":                true,
		"__MACOSX/Takeout/Google Photos/Photos from 2019/a.jpg": true,
		"Takeout/Google Photos/Photos from 2019/a.jpg":          false,
		"Takeout/Google Photos/Photos from 2019/_a.jpg":         false,
		"Takeout/Google Photos/Photos from 2019/.hidden.jpg":    false,
		"Takeout/Google Photos/Photos from 2019/thumbs.db.jpg":  false,
		"Takeout/Google Photos/__MACOSX notes/a.jpg":            false,
	} {
		if got := IsSystemFile(name); got != want {
			t.Errorf("IsSystemFile(%q) = %v, want %v", name, got, want)
		}
	}
}
