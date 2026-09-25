package pipeline

import "testing"

func TestKindOfRawNeedsRawExtension(t *testing.T) {
	tiff := []byte("II*\x00\x08\x00\x00\x00")
	for name, want := range map[string]string{
		"a.dng":  "raw",
		"a.NEF":  "raw",
		"a.CR2":  "raw",
		"a.tif":  "tiff",
		"a.jpg":  "tiff", // content wins over a wrong extension
		"a.heic": "tiff",
	} {
		if got := kindOf(tiff, name); got != want {
			t.Errorf("kindOf(tiff, %q) = %s, want %s", name, got, want)
		}
	}
}
