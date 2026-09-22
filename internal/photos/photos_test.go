package photos

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScriptNamesLibrary(t *testing.T) {
	s := Script("/tmp/Takeout Test.photoslibrary", []string{"/tmp/a.jpg", "/tmp/a.MOV"}, map[string][]int{"Trip": {0, 1}})
	if !strings.Contains(s, "/tmp/Takeout Test.photoslibrary") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "import") || !strings.Contains(s, "a.MOV") {
		t.Fatal(s)
	}
	if !strings.Contains(s, "album named \"Trip\"") {
		t.Fatal(s)
	}
}

func TestSystemLibrary(t *testing.T) {
	sys := SystemLibrary()
	if sys == "" {
		t.Skip("no home")
	}
	if !IsSystemLibrary(sys) {
		t.Fatal("system library")
	}
	other := filepath.Join(os.TempDir(), "TakeoutNotSystem.photoslibrary")
	if IsSystemLibrary(other) {
		t.Fatal("other library")
	}
	if !strings.Contains(ICloudWarning(sys), "iCloud") || !strings.Contains(ICloudWarning(sys), "--confirm-icloud") {
		t.Fatal(ICloudWarning(sys))
	}
}
