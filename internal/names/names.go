// Package names keeps original filenames and only changes what the filesystem or a collision requires.
package names

import (
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Sanitize returns a single path segment safe on APFS and exFAT.
func Sanitize(name string) (string, bool) {
	orig := name
	name = norm.NFC.String(name)
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	for len(name) > 255 {
		ext := path.Ext(name)
		stem := strings.TrimSuffix(name, ext)
		_, size := utf8.DecodeLastRuneInString(stem)
		if size == 0 {
			break
		}
		stem = stem[:len(stem)-size]
		name = stem + ext
		if stem == "" {
			name = "file" + ext
			break
		}
	}
	return name, name != orig
}

// Key is the case-folded NFC form used to detect collisions on case-insensitive disks.
func Key(name string) string {
	return strings.ToLower(norm.NFC.String(name))
}

// WithIndex inserts " (n)" before the extension. n<=1 returns name unchanged.
func WithIndex(name string, n int) string {
	if n <= 1 {
		return name
	}
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	return stem + " (" + itoa(n) + ")" + ext
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// IsScreenshot reports names Takeout uses for screen captures, such as
// Screenshot_20190606-142331.jpg and the macOS "Screen Shot ..." form.
func IsScreenshot(name string) bool {
	base := strings.ToLower(path.Base(name))
	return strings.HasPrefix(base, "screenshot") || strings.HasPrefix(base, "screen shot")
}

// Stem is the filename without its extension.
func Stem(name string) string {
	return strings.TrimSuffix(name, path.Ext(name))
}

// OutputExt picks the extension from the sniffed type.
// liveVideo forces .MOV for the QuickTime family. MP4 and MOV that are not
// Live Photo halves keep a family extension they already have.
func OutputExt(trueType, original string, liveVideo bool) string {
	if liveVideo && (trueType == "mp4" || trueType == "mov" || trueType == "unknown") {
		return ".MOV"
	}
	switch trueType {
	case "jpeg":
		return ".jpg"
	case "png":
		return ".png"
	case "gif":
		return ".gif"
	case "webp":
		return ".webp"
	case "heic":
		return ".heic"
	case "webm":
		return ".mov"
	case "mov":
		return ".mov"
	case "mp4":
		e := strings.ToLower(path.Ext(original))
		switch e {
		case ".mp4", ".mov", ".m4v":
			return e
		default:
			return ".mp4"
		}
	default:
		e := path.Ext(original)
		return e
	}
}

// ReplaceExt keeps the stem and sets ext, which includes the dot.
// Dots that are not a known media extension stay in the stem, so
// "18.12.12 - 8" becomes "18.12.12 - 8.png".
func ReplaceExt(name, ext string) string {
	e := strings.ToLower(path.Ext(name))
	if knownExt(e) {
		name = strings.TrimSuffix(name, path.Ext(name))
	}
	return name + ext
}

func knownExt(e string) bool {
	switch e {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif",
		".mp4", ".mov", ".m4v", ".mkv", ".webm", ".avi":
		return true
	}
	return false
}
