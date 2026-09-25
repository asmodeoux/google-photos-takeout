// Package names keeps original filenames and only changes what the filesystem or a collision requires.
package names

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Rule says which characters a file name may keep.
type Rule int

const (
	// Apple keeps every character APFS accepts; only ":", "/", "\" and control
	// characters are replaced. This is the rule of earlier releases.
	Apple Rule = iota
	// Portable also replaces < > " | ? * and renames reserved Windows device
	// names, so results can live on NTFS, exFAT and FAT disks.
	Portable
)

func (r Rule) String() string {
	if r == Portable {
		return "portable"
	}
	return "apple"
}

// maxName is the byte limit for one path segment. NTFS allows 255 UTF-16 units,
// so 255 bytes is safe everywhere.
const maxName = 255

// Sanitize returns a single path segment under the Apple rule.
func Sanitize(name string) (string, bool) {
	return SanitizeWith(name, Apple)
}

// SanitizeWith returns a single path segment safe under rule r.
func SanitizeWith(name string, r Rule) (string, bool) {
	orig := name
	name = norm.NFC.String(name)
	name = strings.Map(func(c rune) rune {
		switch {
		case c < 0x20 || c == 0x7f:
			return '-'
		case c == ':' || c == '/' || c == '\\':
			return '-'
		case r == Portable && strings.ContainsRune(`<>"|?*`, c):
			return '-'
		}
		return c
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	if r == Portable {
		name = avoidReserved(name)
	}
	name = fit(name, maxName)
	return name, name != orig
}

// SanitizeDir applies rule r to every segment of a slash-separated folder.
func SanitizeDir(folder string, r Rule) (string, bool) {
	parts := strings.Split(folder, "/")
	changed := false
	for i, p := range parts {
		s, ch := SanitizeWith(p, r)
		parts[i] = s
		changed = changed || ch
	}
	return strings.Join(parts, "/"), changed
}

var reserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true, "CONIN$": true, "CONOUT$": true,
	"COM¹": true, "COM²": true, "COM³": true, "LPT¹": true, "LPT²": true, "LPT³": true,
}

func init() {
	for i := '1'; i <= '9'; i++ {
		reserved["COM"+string(i)] = true
		reserved["LPT"+string(i)] = true
	}
}

// avoidReserved adds "_" to a Windows device name such as CON or LPT1.
// Windows compares the part before the first dot, ignoring case.
func avoidReserved(name string) string {
	base, rest, _ := strings.Cut(name, ".")
	if !reserved[strings.ToUpper(strings.TrimRight(base, " "))] {
		return name
	}
	if rest == "" && !strings.Contains(name, ".") {
		return base + "_"
	}
	return base + "_." + rest
}

// fit trims the stem so name is at most limit bytes, keeping the extension.
func fit(name string, limit int) string {
	for len(name) > limit {
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
	return name
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
	suffix := " (" + itoa(n) + ")"
	// Only shorten when a suffix is actually needed, so names without a
	// collision stay exactly as they were.
	stem = strings.TrimSuffix(fit(stem+ext, maxName-len(suffix)), ext)
	return stem + suffix + ext
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
		// Untranscoded WebM or Matroska bytes keep their own extension. A
		// transcoded file has kind mov by the time it is named.
		if e := strings.ToLower(path.Ext(original)); e == ".mkv" {
			return e
		}
		return ".webm"
	case "mov":
		return ".mov"
	case "mp4":
		e := strings.ToLower(path.Ext(original))
		switch e {
		case ".mp4", ".mov", ".m4v", ".3gp", ".3g2":
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
		".mp4", ".mov", ".m4v", ".3gp", ".3g2", ".mkv", ".webm", ".avi",
		".tif", ".tiff", ".dng", ".cr2", ".cr3", ".nef", ".nrw", ".arw", ".srw", ".pef", ".orf", ".rw2", ".raf",
		".bmp", ".mpg", ".mpeg", ".vob", ".wmv", ".asf", ".mts", ".m2ts":
		return true
	}
	return false
}

// windowsStyle lists filesystems that cannot store < > " | ? * or device names.
var windowsStyle = map[string]bool{
	"ntfs": true, "exfat": true, "fat": true, "fat12": true, "fat16": true, "fat32": true,
	"vfat": true, "msdos": true, "refs": true, "fuseblk": true, "ntfs3": true,
}

// ChooseRule picks the naming rule for --names (auto, apple or portable) and the
// filesystem the results live on. On Windows every volume uses the portable rule.
func ChooseRule(flag, fsType string, windows bool) (Rule, string, error) {
	fs := strings.ToLower(strings.TrimSpace(fsType))
	needsPortable := windows || windowsStyle[fs]
	where := "results on " + fsType
	if fsType == "" {
		where = "results filesystem unknown"
	}
	switch flag {
	case "", "auto":
		if needsPortable {
			return Portable, where, nil
		}
		return Apple, where, nil
	case "portable":
		return Portable, "--names portable", nil
	case "apple":
		if needsPortable {
			return Apple, "", fmt.Errorf("apple-style names cannot be written to %s: use --names auto or --names portable", strings.TrimPrefix(where, "results on "))
		}
		return Apple, "--names apple", nil
	}
	return Apple, "", fmt.Errorf("unknown --names value %q: use auto, apple or portable", flag)
}
