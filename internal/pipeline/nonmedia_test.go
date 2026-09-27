package pipeline

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/dates"
	"github.com/asmodeoux/google-photos-takeout/internal/state"
	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

func nonMediaTakeout(t *testing.T, dir string) string {
	t.Helper()
	tk := testgen.New()
	side := testgen.Side{Taken: time.Date(2019, 3, 4, 9, 0, 0, 0, time.UTC)}
	tk.Photo(1, "Photos from 2019", "photo.jpg", mustJPEG(t), &side)
	tk.Put(1, "Photos from 2019", "notes.txt", []byte("shopping list\n"))
	pdf := []byte("%PDF-1.4\n% synthetic\n%%EOF\n")
	tk.Photo(1, "Photos from 2019", "doc.pdf", pdf, &side)
	tk.Photo(1, "Trip", "doc.pdf", pdf, &side)
	arch := filepath.Join(dir, "archives")
	if _, err := tk.Write(arch); err != nil {
		t.Fatal(err)
	}
	return arch
}

func readReportJSON(t *testing.T, results string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(results, ".takeout", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Documents in a Google Photos folder go to not-importable/, never to a year
// folder, unknown/ or an album, and are counted apart from the library.
func TestNonMediaGoesToNotImportable(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	arch := nonMediaTakeout(t, dir)
	out := filepath.Join(dir, "results")
	plan := testOptions(arch, out)
	plan.DryRun = true
	if code, rep, err := Run(context.Background(), plan); code != ExitOK || rep.NotImportable != 2 || len(rep.LegacyNonMedia) > 0 {
		t.Fatalf("check exit %d: %v, not_importable %d, legacy %q", code, err, rep.NotImportable, rep.LegacyNonMedia)
	}
	code, rep, err := Run(context.Background(), testOptions(arch, out))
	if code != ExitOK {
		t.Fatalf("run exit %d: %v %v", code, err, rep.Errors)
	}
	for _, p := range []string{"not-importable/notes.txt", "not-importable/doc.pdf", "2019/photo.jpg"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("want %s: %v", p, err)
		}
	}
	for _, p := range []string{"2019/notes.txt", "2019/doc.pdf", "unknown", "albums/Trip/doc.pdf"} {
		if _, err := os.Stat(filepath.Join(out, p)); err == nil {
			t.Errorf("%s should not exist", p)
		}
	}
	jsons, _ := filepath.Glob(filepath.Join(out, "not-importable", "*.json"))
	if len(jsons) > 0 {
		t.Errorf("sidecars in not-importable: %v", jsons)
	}
	if rep.NotImportable != 2 || rep.Unknown != 0 || rep.Library != 1 {
		t.Errorf("not_importable %d unknown %d library %d", rep.NotImportable, rep.Unknown, rep.Library)
	}
	m := readReportJSON(t, out)
	if m["schema_version"] != 2.0 || m["not_importable"] != 2.0 {
		t.Errorf("report.json schema_version %v not_importable %v", m["schema_version"], m["not_importable"])
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: out}); vcode != ExitOK {
		t.Fatalf("verify exit %d: %v", vcode, verr)
	}
}

// A document that 1.0.0 put in a year folder stays there on a rerun, is
// counted as not importable, and report.txt says to move it.
func TestRerunKeepsLegacyNonMediaInPlace(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	arch := nonMediaTakeout(t, dir)
	out := filepath.Join(dir, "results")
	if code, rep, err := Run(context.Background(), testOptions(arch, out)); code != ExitOK {
		t.Fatalf("run exit %d: %v %v", code, err, rep.Errors)
	}
	// Make the results look like 1.0.0's: doc.pdf placed in 2019/.
	j := openTestJournal(t, out)
	moved := false
	for _, r := range j.All() {
		if r.Path == "not-importable/doc.pdf" {
			if err := os.Rename(filepath.Join(out, "not-importable", "doc.pdf"), filepath.Join(out, "2019", "doc.pdf")); err != nil {
				t.Fatal(err)
			}
			r.Path, r.Year = "2019/doc.pdf", "2019"
			if err := j.Put(r); err != nil {
				t.Fatal(err)
			}
			moved = true
		}
	}
	j.Close()
	if !moved {
		t.Fatal("setup: no journal record for not-importable/doc.pdf")
	}

	code, rep, err := Run(context.Background(), testOptions(arch, out))
	if code != ExitOK {
		t.Fatalf("rerun exit %d: %v %v", code, err, rep.Errors)
	}
	if _, err := os.Stat(filepath.Join(out, "2019", "doc.pdf")); err != nil {
		t.Errorf("legacy doc.pdf moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, "not-importable", "doc.pdf")); err == nil {
		t.Error("rerun made a second copy of doc.pdf")
	}
	if rep.NotImportable != 2 || rep.Library != 1 {
		t.Errorf("not_importable %d library %d", rep.NotImportable, rep.Library)
	}
	txt, _ := os.ReadFile(filepath.Join(out, ".takeout", "report.txt"))
	if !strings.Contains(string(txt), "  2019/doc.pdf\n") || !strings.Contains(string(txt), "Move them out") {
		t.Errorf("report.txt does not list the legacy file:\n%s", txt)
	}
	if vcode, _, verr := Verify(context.Background(), Options{Results: out}); vcode != ExitOK {
		t.Fatalf("verify exit %d: %v", vcode, verr)
	}
}

func testMember(folder, name string) member {
	return member{Entry: zipindex.Entry{ZipPath: "z.zip", EntryName: "Takeout/Google Photos/" + folder + "/" + name, RelFolder: folder, Name: name}}
}

// The ledger and the report decide by kind: a document's album copy is
// not-importable (no album copy is made), a document with photo bytes is a
// photo, and an unknown format Photos may read stays in unknown/.
func TestLedgerAndReportNonMedia(t *testing.T) {
	dated := dates.When{OK: true, Year: 2019, Source: "sidecar"}
	groups := []group{
		{members: []member{testMember("Photos from 2019", "doc.pdf"), testMember("Trip", "doc.pdf")}, trueType: "unknown", outRel: "not-importable/doc.pdf", when: dated},
		{members: []member{testMember("Photos from 2019", "old.txt")}, trueType: "unknown", outRel: "2019/old.txt", when: dated},
		{members: []member{testMember("Photos from 2019", "scan.pdf")}, trueType: "jpeg", outRel: "2019/scan.jpg", when: dated},
		{members: []member{testMember("Photos from 2019", "image.jxl")}, trueType: "unknown", outRel: "unknown/image.jxl"},
		{members: []member{testMember("Photos from 2019", "clip.webm")}, trueType: "webm", outRel: "not-importable/clip.webm", when: dated},
		{members: []member{testMember("Photos from 2019", "broken.pdf")}, trueType: "unknown", failErr: "crc"},
	}
	var entries []zipindex.Entry
	for _, g := range groups {
		for _, m := range g.members {
			entries = append(entries, m.Entry)
		}
	}
	fates := ledger(entries, groups, nil, nil, false)
	want := []string{"not-importable", "not-importable", "not-importable", "library", "library", "not-importable", "error"}
	if !state.Balanced(fates, len(entries)) {
		t.Fatal("ledger not balanced")
	}
	for i, f := range fates {
		if f.Fate != want[i] {
			t.Errorf("%s: fate %s, want %s", f.Entry, f.Fate, want[i])
		}
	}
	rep := Report{Years: map[string]int{}, Sources: map[string]int{}, TZ: map[string]int{}}
	fillReport(&rep, groups)
	if rep.NotImportable != 3 || rep.Library != 1 || rep.Unknown != 1 || rep.Failed != 1 {
		t.Errorf("not_importable %d library %d unknown %d failed %d", rep.NotImportable, rep.Library, rep.Unknown, rep.Failed)
	}
	if len(rep.LegacyNonMedia) != 1 || rep.LegacyNonMedia[0] != "2019/old.txt" {
		t.Errorf("legacy %v", rep.LegacyNonMedia)
	}
}

// A placeholder whose extraction failed is counted by pickCanon in
// placeholders and here in failed; the report identity subtracts it once.
func TestFillReportFailedPlaceholder(t *testing.T) {
	groups := []group{
		{members: []member{testMember("A", "p.jpg"), testMember("B", "p.jpg"), testMember("C", "p.jpg")}, trueType: "jpeg", placeholder: true, failErr: "crc"},
		{members: []member{testMember("A", "q.jpg")}, trueType: "jpeg", placeholder: true, outRel: "placeholders/q.jpg"},
	}
	rep := Report{Years: map[string]int{}, Sources: map[string]int{}, TZ: map[string]int{}}
	fillReport(&rep, groups)
	if rep.Failed != 1 || rep.Library != 0 || rep.Unknown != 0 || rep.NotImportable != 0 {
		t.Errorf("failed %d library %d unknown %d not_importable %d", rep.Failed, rep.Library, rep.Unknown, rep.NotImportable)
	}
}

// Two sidecars whose names differ only in normal form each belong to the
// photo spelled the same way; neither takes the other's date.
func TestSidecarsDifferingOnlyInNormalForm(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	tk := testgen.New()
	nfc, nfd := "été.jpg", "été.jpg"
	tk.Photo(1, "Photos from 2019", nfc, testgen.JPEG(1), &testgen.Side{Taken: time.Date(2018, 5, 1, 9, 0, 0, 0, time.UTC)})
	tk.Photo(1, "Photos from 2019", nfd, testgen.JPEG(2), &testgen.Side{Taken: time.Date(2019, 5, 1, 9, 0, 0, 0, time.UTC)})
	arch := filepath.Join(dir, "archives")
	if _, err := tk.Write(arch); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "results")
	if code, rep, err := Run(context.Background(), testOptions(arch, out)); code != ExitOK {
		t.Fatalf("exit %d: %v %v", code, err, rep.Errors)
	}
	for _, y := range []string{"2018", "2019"} {
		if _, err := os.Stat(filepath.Join(out, y, nfc)); err != nil {
			t.Errorf("%s: %v", y, err)
		}
	}
}

// check plans the same outcomes the run reports: a photo in Trash is left
// out, not counted as "unknown date".
func TestCheckLeavesTrashOutLikeRun(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "archives")
	if _, err := testgen.Corpus().Write(arch); err != nil {
		t.Fatal(err)
	}
	for _, trash := range []bool{false, true} {
		opt := testOptions(arch, filepath.Join(dir, "results"))
		opt.DryRun, opt.IncludeTrash = true, trash
		code, rep, err := Run(context.Background(), opt)
		if code != ExitOK {
			t.Fatalf("check exit %d: %v", code, err)
		}
		left := 1 // Trash/trashed.jpg
		if trash {
			left = 0
		}
		if sum := rep.Library + rep.Unknown + rep.NotImportable + rep.Placeholders + left; sum != rep.Unique || rep.Unknown != 3 {
			t.Errorf("include-trash %v: unique %d, library %d unknown %d not_importable %d placeholders %d",
				trash, rep.Unique, rep.Library, rep.Unknown, rep.NotImportable, rep.Placeholders)
		}
	}
}
