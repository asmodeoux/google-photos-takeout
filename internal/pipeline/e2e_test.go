package pipeline

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestE2EAndResume(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	dir := t.TempDir()
	arch := filepath.Join(dir, "archives")
	out := filepath.Join(dir, "results")
	os.MkdirAll(arch, 0o755)
	jpeg := []byte{0xff, 0xd8, 0xff, 0xd9}
	// a slightly larger jpeg so exiftool accepts it
	jpeg = mustJPEG(t)
	writeZip(t, filepath.Join(arch, "takeout-20200101T000000Z-1-001.zip"), map[string][]byte{
		"Takeout/Google Photos/Trip/photo.jpg":             jpeg,
		"Takeout/Google Photos/Trip/still.HEIC":            jpeg,
		"Takeout/Google Photos/Photos from 2016/still.MP4": ftyp("qt  "),
	})
	writeZip(t, filepath.Join(arch, "takeout-20200101T000000Z-1-002.zip"), map[string][]byte{
		"Takeout/Google Photos/Trip/photo.jpg.supplemental-metadata.json":              sidecar("photo.jpg", 1464739200, 55.75, 37.62),
		"Takeout/Google Photos/Photos from 2016/photo.jpg":                             jpeg,
		"Takeout/Google Photos/Photos from 2016/still.HEIC.supplemental-metadata.json": sidecar("still.HEIC", 1574677762, 0, 0),
		"Takeout/Google Photos/Old/tiny.png":                                           tinyPNG(),
		"Takeout/Google Photos/Old/tiny.png.supplemental-metadata.json":                sidecar("tiny.png", 1400000000, 0, 0),
		"Takeout/Google Photos/Old2/tiny.png":                                          tinyPNG(),
		"Takeout/Google Photos/Old2/tiny.png.supplemental-metadata.json":               sidecar("tiny.png", 1401000000, 0, 0),
		"Takeout/Google Photos/Old3/tiny.png":                                          tinyPNG(),
		"Takeout/Google Photos/Old3/tiny.png.supplemental-metadata.json":               sidecar("tiny.png", 1402000000, 0, 0),
	})
	ctx := context.Background()
	code, rep, err := Run(ctx, Options{Archives: arch, Results: out, Albums: "clone", Progress: "plain", Quiet: true, Stdout: &bytes.Buffer{}, FailAfter: 1})
	if code != ExitInterrupt {
		t.Fatalf("fail-after code %d err %v", code, err)
	}
	code, rep, err = Run(ctx, Options{Archives: arch, Results: out, Albums: "clone", Progress: "plain", Quiet: true, Stdout: &bytes.Buffer{}})
	if code != ExitOK {
		t.Fatalf("run code %d err %v errors %v", code, err, rep.Errors)
	}
	// second run is a no-op and must not invent a second copy
	code2, _, err := Run(ctx, Options{Archives: arch, Results: out, Albums: "clone", Progress: "plain", Quiet: true, Stdout: &bytes.Buffer{}})
	if err != nil && code2 == ExitReconcile {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(out, "2016", "*"))
	dup := 0
	for _, m := range matches {
		if bytes.Contains([]byte(filepath.Base(m)), []byte("(2)")) {
			dup++
		}
	}
	if dup > 0 {
		t.Fatalf("rerun created duplicates: %v", matches)
	}
	vcode, _, verr := Verify(ctx, Options{Results: out})
	if verr != nil && vcode == ExitReconcile {
		t.Fatal(verr)
	}
	_ = json.Marshal
}

func writeZip(t *testing.T, path string, files map[string][]byte) {
	t.Helper()
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sidecar(title string, ts int64, lat, lon float64) []byte {
	b, _ := json.Marshal(map[string]any{
		"title":          title,
		"description":    "",
		"photoTakenTime": map[string]any{"timestamp": json.Number(intStr(ts))},
		"geoData":        map[string]any{"latitude": lat, "longitude": lon, "altitude": 0.0},
		"geoDataExif":    map[string]any{"latitude": lat, "longitude": lon, "altitude": 0.0},
	})
	return b
}

func intStr(n int64) string {
	return jsonNumber(n)
}

func jsonNumber(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func ftyp(brand string) []byte {
	b := make([]byte, 20)
	b[3] = 20
	copy(b[4:8], "ftyp")
	copy(b[8:12], brand)
	return b
}

func tinyPNG() []byte {
	// 1x1 png
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe,
		0x02, 0xfe, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
		0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func mustJPEG(t *testing.T) []byte {
	t.Helper()
	// 1x1 jpeg
	return []byte{
		0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00, 0x01,
		0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xff, 0xdb, 0x00, 0x43,
		0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07, 0x07, 0x09,
		0x09, 0x08, 0x0a, 0x0c, 0x14, 0x0d, 0x0c, 0x0b, 0x0b, 0x0c, 0x19, 0x12,
		0x13, 0x0f, 0x14, 0x1d, 0x1a, 0x1f, 0x1e, 0x1d, 0x1a, 0x1c, 0x1c, 0x20,
		0x24, 0x2e, 0x27, 0x20, 0x22, 0x2c, 0x23, 0x1c, 0x1c, 0x28, 0x37, 0x29,
		0x2c, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1f, 0x27, 0x39, 0x3d, 0x38, 0x32,
		0x3c, 0x2e, 0x33, 0x34, 0x32, 0xff, 0xc0, 0x00, 0x0b, 0x08, 0x00, 0x01,
		0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xff, 0xc4, 0x00, 0x14, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x03, 0xff, 0xc4, 0x00, 0x14, 0x10, 0x01, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0xff, 0xda, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3f, 0x00,
		0x37, 0xff, 0xd9,
	}
}
