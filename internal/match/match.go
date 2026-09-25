// Package match pairs a media filename with a Takeout JSON sidecar in the same folder.
// The rules follow GooglePhotosTakeoutHelper and the 51-character truncation used
// since 2024, including the (N) marker moving to just before ".json".
package match

import (
	"path"
	"regexp"
	"strings"
)

var (
	numRe  = regexp.MustCompile(`\((\d+)\)`)
	extras = []string{
		"-edited", "-edit", "-effects", "-bearbeitet", "-modifie", "-modifié",
		"-ha editado", "-edytowano", "-bewerkt", "-編集済み", "-bearbeitet",
	}
)

// Key is folder + filename, the only lookup. Names are not matched across folders.
func Key(folder, name string) string {
	return folder + "\x00" + name
}

// Candidates lists sidecar filenames to try, most specific first.
// Every name is complete. A shorter file never matches a longer one.
func Candidates(name string) []string {
	var out []string
	add := func(s string) {
		if s == "" {
			return
		}
		for _, e := range out {
			if e == s {
				return
			}
		}
		out = append(out, s)
	}
	addFull := func(n string) {
		add(n + ".supplemental-metadata.json")
		add(n + ".json")
		add(fit51(n + ".supplemental-metadata"))
		add(fit51(n))
		// Some exports name the sidecar "<name>.metadata.json".
		add(n + ".metadata.json")
		add(fit51(n + ".metadata"))
	}
	addNumbered := func(n string) {
		bare, num, ok := stripNumber(n)
		if !ok {
			return
		}
		add(bare + ".supplemental-metadata(" + num + ").json")
		add(bare + "(" + num + ").json")
		add(fit51(bare + ".supplemental-metadata"))
		// (N) is inserted after truncation, so the result may exceed 51 chars.
		base := bare + ".supplemental-metadata"
		full := base + ".json"
		if len(full) > 51 {
			base = base[:51-len(".json")]
		}
		add(base + "(" + num + ").json")
	}

	addFull(name)
	addNumbered(name)

	stem := strings.TrimSuffix(name, path.Ext(name))
	if stem != name && stem != "" {
		addFull(stem)
		addNumbered(stem)
	}
	if stripped := stripExtra(name); stripped != name {
		addFull(stripped)
		addNumbered(stripped)
		stem2 := strings.TrimSuffix(stripped, path.Ext(stripped))
		if stem2 != stripped && stem2 != "" {
			addFull(stem2)
			addNumbered(stem2)
		}
	}
	return out
}

// fit51 truncates so the filename is at most 51 bytes and still ends in .json.
func fit51(base string) string {
	full := base + ".json"
	if len(full) <= 51 {
		return full
	}
	cut := 51 - len(".json")
	if cut > len(base) {
		cut = len(base)
	}
	// don't split a UTF-8 sequence
	for cut > 0 && cut < len(base) && base[cut]&0xC0 == 0x80 {
		cut--
	}
	return base[:cut] + ".json"
}

func stripNumber(name string) (bare, num string, ok bool) {
	m := numRe.FindStringSubmatchIndex(name)
	if m == nil {
		return "", "", false
	}
	num = name[m[2]:m[3]]
	bare = name[:m[0]] + name[m[1]:]
	return bare, num, true
}

func stripExtra(name string) string {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	lower := strings.ToLower(stem)
	for _, e := range extras {
		el := strings.ToLower(e)
		if strings.HasSuffix(lower, el) {
			return stem[:len(stem)-len(e)] + ext
		}
	}
	return name
}

// TitleAgrees reports whether a sidecar title matches the media name.
// Takeout titles omit the (N) duplicate marker, so that marker is ignored.
func TitleAgrees(mediaName, title string) bool {
	if title == "" {
		return true
	}
	if mediaName == title {
		return true
	}
	a, _, ok := stripNumber(mediaName)
	if !ok {
		a = mediaName
	}
	b, _, ok := stripNumber(title)
	if !ok {
		b = title
	}
	stem := strings.TrimSuffix(a, path.Ext(a))
	// titles often omit the extension and the (N) marker
	if a == title || a == b || stem == title || stem == b {
		return true
	}
	stripped := stripExtra(mediaName)
	if stripped != mediaName && TitleAgrees(stripped, title) {
		return true
	}
	// A Live Photo's video half may share a sidecar titled with the still's
	// name: IMG_1.MOV and IMG_1.HEIC, or PXL_1.MP and PXL_1.MP.jpg.
	return strings.EqualFold(LiveStem(a), LiveStem(b))
}

// LiveStem is the name both halves of a Live or motion photo share: the name
// without its extension and without a Pixel ".MP" or ".MV" marker, so that
// PXL_1.MP.jpg, PXL_1.MP and PXL_1.MP~2 all give PXL_1.
func LiveStem(name string) string {
	stem := strings.TrimSuffix(name, path.Ext(name))
	for _, m := range []string{".MP", ".MV"} {
		if len(stem) > len(m) && strings.EqualFold(stem[len(stem)-len(m):], m) {
			return stem[:len(stem)-len(m)]
		}
	}
	return stem
}

// FoldKey is Key compared without regard to case. Google sometimes writes
// "IMG.JPG" next to "IMG.jpg.json".
func FoldKey(folder, name string) string {
	return folder + "\x00" + strings.ToLower(name)
}
