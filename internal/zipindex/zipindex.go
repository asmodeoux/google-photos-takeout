// Package zipindex reads Google Takeout zip central directories into one tree.
package zipindex

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// FolderClass is how a directory under Google Photos is treated.
type FolderClass int

const (
	ClassLibrary FolderClass = iota // year folder, Archive, Locked Folder, Failed Videos
	ClassAlbum
	ClassTrash
	ClassSkip
)

// Entry is one non-directory zip member.
type Entry struct {
	ZipPath   string
	EntryName string
	RelFolder string // folder under Google Photos, NFC, spaces normalized
	Name      string
	Size      uint64
	CRC32     uint32
	JSON      bool
	// System is an operating-system file such as ._x.jpg or .DS_Store, or an
	// entry under __MACOSX/, which appear when a Takeout is re-zipped on a Mac.
	System bool
	// Symlink is a zip entry stored as a symbolic link. It is never created.
	Symlink bool
}

// Index is the merged view of every zip.
type Index struct {
	Entries   []Entry
	Zips      []ZipInfo
	ExportIDs []string
	Missing   []int
	// MissingByExport lists missing part numbers for each export ID.
	MissingByExport map[string][]int
	PartPrefix      string
	// FallbackRoot is set when no known "Google Photos" folder name was found
	// and the export's photo folder was recognized by its contents instead.
	FallbackRoot string
}

// ZipInfo describes one archive part.
type ZipInfo struct {
	Path     string
	ExportID string
	Part     int
	Files    int
}

var (
	yearPrefixes = []string{
		"Photos from ", "Fotos von ", "Fotos aus ", "Photos de ", "Fotos de ",
		"Foto's uit ", "Foto_s van ", "Foto dal ", "Foto del ", "Zdjęcia z ",
		"Фото за ", "Фотографии за ", "Fotky z ", "Fotografii din ", "Foton från ",
		"Bilder fra ", "Billeder fra ", "Valokuvat ", "Fényképek - ", "Fotoğraflar ",
	}
	yearSuffixes = []string{" 年の写真", "年のフォト", "년의 사진", "年的照片", "年的相片"}
	gpNames      = map[string]bool{
		"Google Photos": true, "Google Fotos": true, "Google Foto's": true,
		"Google Foto_s": true, "Google Foto": true, "Photos Google": true,
		"Zdjęcia Google": true, "Google Фото": true, "Fotky Google": true,
		"Google Foton": true, "Google Bilder": true, "Google Billeder": true,
		"Google Kuvat": true, "Google Fotók": true, "Google Fotoğraflar": true,
		"Google フォト": true, "Google 포토": true, "Google 相片": true, "Google 照片": true,
	}
	partRe = regexp.MustCompile(`takeout-(\d{8}T\d{6}Z).*?-(\d+)\.zip$`)
)

// Open reads central directories only. File bodies are not hashed or CRC-checked here.
func Open(paths []string) (*Index, error) {
	idx := &Index{}
	parts := map[string]map[int]bool{}
	var others []Entry // entries under a Takeout folder we did not recognize
	for _, p := range paths {
		zr, err := zip.OpenReader(p)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w (re-download this zip if it is truncated)", p, err)
		}
		info := ZipInfo{Path: p}
		base := path.Base(p)
		if m := partRe.FindStringSubmatch(base); m != nil {
			info.ExportID = m[1]
			info.Part, _ = strconv.Atoi(m[2])
			if parts[info.ExportID] == nil {
				parts[info.ExportID] = map[int]bool{}
			}
			parts[info.ExportID][info.Part] = true
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			name := f.Name
			if err := CheckPath(name); err != nil {
				zr.Close()
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			rel, baseName, ok := splitGooglePhotos(name)
			if !ok {
				if strings.HasPrefix(name, "Takeout/") && strings.Count(name, "/") >= 2 {
					others = append(others, Entry{ZipPath: p, EntryName: name, Size: f.UncompressedSize64, CRC32: f.CRC32,
						System: IsSystemFile(name), Symlink: f.Mode()&fs.ModeSymlink != 0})
				}
				continue
			}
			e := Entry{
				ZipPath:   p,
				EntryName: name,
				RelFolder: rel,
				Name:      baseName,
				Size:      f.UncompressedSize64,
				CRC32:     f.CRC32,
				JSON:      strings.HasSuffix(strings.ToLower(baseName), ".json"),
				System:    IsSystemFile(name),
				Symlink:   f.Mode()&fs.ModeSymlink != 0,
			}
			idx.Entries = append(idx.Entries, e)
			info.Files++
		}
		zr.Close()
		idx.Zips = append(idx.Zips, info)
	}
	if len(idx.Entries) == 0 && len(others) > 0 {
		idx.useFallbackRoot(others)
	}
	ids := make([]string, 0, len(parts))
	for id := range parts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		set := parts[id]
		idx.ExportIDs = append(idx.ExportIDs, id)
		max := 0
		for n := range set {
			if n > max {
				max = n
			}
		}
		for n := 1; n <= max; n++ {
			if !set[n] {
				idx.Missing = append(idx.Missing, n)
				if idx.MissingByExport == nil {
					idx.MissingByExport = map[string][]int{}
				}
				idx.MissingByExport[id] = append(idx.MissingByExport[id], n)
			}
		}
	}
	return idx, nil
}

// useFallbackRoot handles an export whose photo folder has a name we do not
// know, for example in a language not listed in gpNames. It picks the one
// top folder under Takeout/ that holds both media and JSON sidecars. Year
// folders there are recognized by a four-digit year before or after a word.
func (idx *Index) useFallbackRoot(others []Entry) {
	media, sidecars := map[string]int{}, map[string]int{}
	for _, e := range others {
		root := strings.SplitN(e.EntryName, "/", 3)[1]
		if strings.HasSuffix(strings.ToLower(e.EntryName), ".json") {
			sidecars[root]++
		} else {
			media[root]++
		}
	}
	var roots []string
	for r := range media {
		if sidecars[r] > 0 {
			roots = append(roots, r)
		}
	}
	if len(roots) != 1 {
		return
	}
	root := roots[0]
	idx.FallbackRoot = root
	for _, e := range others {
		parts := strings.Split(e.EntryName, "/")
		if parts[1] != root || len(parts) < 3 {
			continue
		}
		folder, name := "", parts[len(parts)-1]
		if len(parts) > 3 {
			folder = NormFolder(parts[2])
			if y := genericYear(folder); y != "" {
				folder = "Photos from " + y
			}
		}
		if name == "" {
			continue
		}
		e.RelFolder = folder
		e.Name = name
		e.JSON = strings.HasSuffix(strings.ToLower(name), ".json")
		idx.Entries = append(idx.Entries, e)
	}
}

var genericYearRe = regexp.MustCompile(`^(?:\p{L}[\p{L}' ]*[ _-])?((?:18|19|20)\d{2})(?:[ _-][\p{L}' ]*\p{L})?$`)

// genericYear returns the year of a folder named like "<word> 2019" or
// "2019 <word>" in any language, or "".
func genericYear(folder string) string {
	m := genericYearRe.FindStringSubmatch(folder)
	if m == nil || !yearNum(m[1]) {
		return ""
	}
	return m[1]
}

// IsSystemFile reports files an operating system adds next to photos:
// macOS AppleDouble "._" files, .DS_Store, anything under __MACOSX/, and
// Windows Thumbs.db and desktop.ini.
func IsSystemFile(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if seg == "__MACOSX" {
			return true
		}
	}
	return IsSystemName(path.Base(name))
}

// IsSystemName is IsSystemFile for a single file name.
func IsSystemName(base string) bool {
	if strings.HasPrefix(base, "._") {
		return true
	}
	switch strings.ToLower(base) {
	case ".ds_store", "thumbs.db", "desktop.ini":
		return true
	}
	return false
}

// CheckPath rejects absolute paths and ".." segments (zip slip).
func CheckPath(name string) error {
	if path.IsAbs(name) || strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("unsafe zip path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return fmt.Errorf("unsafe zip path %q", name)
		}
	}
	return nil
}

func splitGooglePhotos(entry string) (folder, name string, ok bool) {
	parts := strings.Split(entry, "/")
	for i, p := range parts {
		if gpNames[NormFolder(p)] && i+1 < len(parts)-0 && i+1 < len(parts) {
			// file directly inside Google Photos has no album folder
			if i+1 == len(parts)-1 {
				return "", parts[len(parts)-1], true
			}
			folder = NormFolder(parts[i+1])
			name = parts[len(parts)-1]
			if name == "" {
				return "", "", false
			}
			return folder, name, true
		}
	}
	return "", "", false
}

// NormFolder makes folder comparison stable: NFC and ordinary spaces.
func NormFolder(s string) string {
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = strings.ReplaceAll(s, "\u202f", " ")
	s = strings.ReplaceAll(s, "\u2007", " ")
	s = strings.TrimSpace(norm.NFC.String(s))
	return s
}

// Classify reports how a folder under Google Photos should be used.
func Classify(folder string) FolderClass {
	folder = NormFolder(folder)
	if folder == "" {
		return ClassLibrary
	}
	switch strings.ToLower(folder) {
	case "trash", "bin", "papierkorb", "corbeille":
		return ClassTrash
	case "archive", "locked folder", "failed videos":
		return ClassLibrary
	}
	if isYear(folder) {
		return ClassLibrary
	}
	return ClassAlbum
}

func isYear(name string) bool {
	for _, p := range yearPrefixes {
		if rest, ok := strings.CutPrefix(name, p); ok && yearNum(rest) {
			return true
		}
	}
	for _, s := range yearSuffixes {
		if rest, ok := strings.CutSuffix(name, s); ok && yearNum(strings.TrimSpace(rest)) {
			return true
		}
	}
	return false
}

func yearNum(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	n, _ := strconv.Atoi(s)
	return n >= 1800 && n <= 2100
}

// IsSkippedJSON is album-level metadata, not a per-file sidecar.
func IsSkippedJSON(name string) bool {
	switch strings.ToLower(name) {
	case "metadata.json", "shared_album_comments.json", "user-generated-memory-titles.json":
		return true
	}
	return false
}

// Sniff returns jpeg, png, gif, webp, heic, mov, mp4, webm, tiff, raw, cr3, or
// unknown. tiff covers TIFF-based camera RAW such as DNG, CR2, NEF and ARW; raw
// is a RAW format with its own header (ORF, RW2, RAF); cr3 is Canon's ISO-BMFF
// RAW, which must not be mistaken for a video.
func Sniff(b []byte) string {
	if len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff {
		return "jpeg"
	}
	if len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return "png"
	}
	if len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))) {
		return "gif"
	}
	if len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")) {
		return "webp"
	}
	if len(b) >= 4 && b[0] == 0x1a && b[1] == 0x45 && b[2] == 0xdf && b[3] == 0xa3 {
		return "webm"
	}
	if len(b) >= 4 && (bytes.Equal(b[:4], []byte("II*\x00")) || bytes.Equal(b[:4], []byte("MM\x00*"))) {
		return "tiff"
	}
	if len(b) >= 4 {
		switch string(b[:4]) {
		case "IIRO", "IIRS", "MMOR", "IIU\x00":
			return "raw"
		}
	}
	if len(b) >= 15 && string(b[:15]) == "FUJIFILMCCD-RAW" {
		return "raw"
	}
	if len(b) >= 12 && bytes.Equal(b[4:8], []byte("ftyp")) {
		brand := string(b[8:12])
		switch brand {
		case "crx ":
			return "cr3"
		case "heic", "heix", "hevc", "hevx", "mif1", "msf1", "heim", "heis":
			return "heic"
		case "qt  ":
			return "mov"
		default:
			return "mp4"
		}
	}
	return "unknown"
}

// ReadHeader reads the first n bytes of a zip entry.
func ReadHeader(f *zip.File, n int) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	buf := make([]byte, n)
	got, err := io.ReadFull(r, buf)
	if err == io.ErrUnexpectedEOF || err == io.EOF {
		return buf[:got], nil
	}
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// PNGSize reads width and height from a PNG IHDR when b is a PNG header.
func PNGSize(b []byte) (w, h int, ok bool) {
	// signature 8 + IHDR length 4 + type 4 + w 4 + h 4
	if Sniff(b) != "png" || len(b) < 24 {
		return 0, 0, false
	}
	w = int(uint32(b[16])<<24 | uint32(b[17])<<16 | uint32(b[18])<<8 | uint32(b[19]))
	h = int(uint32(b[20])<<24 | uint32(b[21])<<16 | uint32(b[22])<<8 | uint32(b[23]))
	return w, h, true
}
