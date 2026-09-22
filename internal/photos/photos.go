// Package photos builds the AppleScript that imports a library into Photos.
// Tests check the script text. They never launch Photos.
package photos

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// SystemLibrary is the Photos library that syncs with iCloud.
func SystemLibrary() string {
	if out, err := exec.Command("defaults", "read", "com.apple.Photos", "IPXDefaultLibraryURL").Output(); err == nil {
		s := strings.TrimSpace(string(out))
		s = strings.Trim(s, `"`)
		if u, err := url.Parse(s); err == nil && u.Path != "" {
			return filepath.Clean(u.Path)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Pictures", "Photos Library.photoslibrary")
}

// IsSystemLibrary reports whether path is the Photos library that syncs to iCloud.
func IsSystemLibrary(path string) bool {
	if path == "" {
		return false
	}
	sys := SystemLibrary()
	if sys == "" {
		return false
	}
	a, _ := filepath.Abs(path)
	b, _ := filepath.Abs(sys)
	return filepath.Clean(a) == filepath.Clean(b)
}

// ICloudWarning is printed before an import into the system library.
func ICloudWarning(path string) string {
	return fmt.Sprintf("This library syncs to iCloud:\n  %s\nImporting the whole Takeout can upload tens of gigabytes and fill iCloud storage. Re-run with --confirm-icloud only if that is what you want.", path)
}

// Script imports files into the already-open library and adds them to albums.
// library is named in a comment so the test can see which library was requested.
func Script(library string, files []string, albums map[string][]int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "-- library %s\n", library)
	b.WriteString("tell application \"Photos\"\n")
	for i, f := range files {
		fmt.Fprintf(&b, "  set f%d to POSIX file %q\n", i, f)
	}
	if len(files) > 0 {
		b.WriteString("  set imported to import {")
		for i := range files {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "f%d", i)
		}
		b.WriteString("} skip check duplicates yes\n")
	}
	names := make([]string, 0, len(albums))
	for name := range albums {
		names = append(names, name)
	}
	// stable order
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		fmt.Fprintf(&b, "  set alb to make new album named %q\n", name)
		b.WriteString("  add {")
		idxs := albums[name]
		for i, idx := range idxs {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "item %d of imported", idx+1)
		}
		b.WriteString("} to alb\n")
	}
	b.WriteString("end tell\n")
	return b.String()
}
