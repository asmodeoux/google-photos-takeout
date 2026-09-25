package exiftool

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/dates"
)

func TestArgsAbsoluteAndOffset(t *testing.T) {
	when := dates.When{
		Instant: time.Date(2016, 12, 30, 14, 55, 37, 0, time.UTC), OK: true,
		Offset: 3 * time.Hour, OffsetKnown: true, Year: 2016,
		HasGPS: true, Lat: 55.75, Lon: 37.62, HasAlt: true, Alt: 12,
	}
	path := filepath.Join(t.TempDir(), "-file.jpg")
	args, err := Args(Plan{Path: path, Kind: "jpeg", WriteDates: true, WriteGPS: true, When: when, Description: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(joined, abs) {
		t.Fatal("absolute path")
	}
	if !strings.Contains(joined, "OffsetTimeOriginal=+03:00") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "DateTimeOriginal=2016:12:30 17:55:37") {
		t.Fatal(joined)
	}
	if strings.Contains(joined, "\n-file") {
		t.Fatal("leading dash treated as flag")
	}
}

func TestVideoCreationDateHasOffset(t *testing.T) {
	when := dates.When{
		Instant: time.Date(2016, 12, 30, 14, 55, 37, 0, time.UTC), OK: true,
		Offset: 3 * time.Hour, OffsetKnown: true, HasGPS: true, Lat: 1, Lon: 2,
	}
	args, err := Args(Plan{Path: "/tmp/v.mov", Kind: "mov", WriteDates: true, WriteGPS: true, When: when})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "Keys:CreationDate=2016:12:30 17:55:37+03:00") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "QuickTime:CreateDate=2016:12:30 14:55:37") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "Keys:GPSCoordinates=") {
		t.Fatal("gps")
	}
}

func TestPNGAndGIF(t *testing.T) {
	when := dates.When{Instant: time.Date(2016, 1, 2, 3, 4, 5, 0, time.UTC), OK: true, OffsetKnown: true, Offset: time.Hour}
	args, _ := Args(Plan{Path: "/tmp/a.png", Kind: "png", WriteDates: true, When: when})
	if !strings.Contains(strings.Join(args, "\n"), "XMP-exif:DateTimeOriginal=") {
		t.Fatal("png xmp")
	}
	args, _ = Args(Plan{Path: "/tmp/a.gif", Kind: "gif", WriteDates: true, When: when})
	j := strings.Join(args, "\n")
	if strings.Contains(j, "DateTimeOriginal=") {
		t.Fatal("gif should not get exif datetime")
	}
	if !strings.Contains(j, "XMP:DateCreated=") {
		t.Fatal(j)
	}
}

func TestFillDontClobber(t *testing.T) {
	want := dates.When{Instant: time.Date(2016, 6, 1, 12, 0, 0, 0, time.UTC), OK: true}
	same := want.Instant.Add(time.Second)
	if ShouldWriteDates(&same, want) {
		t.Fatal("within 2s should be kept")
	}
	far := want.Instant.Add(2 * time.Hour)
	if !ShouldWriteDates(&far, want) {
		t.Fatal("disagreement should write")
	}
	if !ShouldWriteDates(nil, want) {
		t.Fatal("missing should write")
	}
}

func TestParseReady(t *testing.T) {
	id, ok := ParseReady("1 image files updated\n{ready7}\n")
	if !ok || id != 7 {
		t.Fatal(id, ok)
	}
	if !Updated("1 image files updated\n") {
		t.Fatal("updated")
	}
}

func TestWithin(t *testing.T) {
	if !Within("/tmp/results", "/tmp/results/2016/a.jpg") {
		t.Fatal("inside")
	}
	if Within("/tmp/results", "/tmp/other/a.jpg") {
		t.Fatal("outside")
	}
}

func TestDescriptionLineBreaksCannotInjectArguments(t *testing.T) {
	when := dates.When{Instant: time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), OK: true}
	desc := "first line\n-execute\r\n-o\n/tmp/evil\x00tail"
	args, err := Args(Plan{Path: "/tmp/p.jpg", Kind: "jpeg", WriteDates: true, When: when, Description: desc, SetContentID: "id\nx"})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			t.Fatalf("argument %q still has a line break", a)
		}
		if a == "-execute" || a == "-o" {
			t.Fatalf("injected argument %q", a)
		}
	}
	joined := strings.Join(args, "\n")
	if !strings.Contains(joined, "-ImageDescription=first line -execute -o /tmp/evil tail") {
		t.Fatal(joined)
	}
	if !strings.Contains(joined, "-Apple:ContentIdentifier=id x") {
		t.Fatal(joined)
	}
}

func TestCleanValue(t *testing.T) {
	cases := map[string]string{
		"":                 "",
		"plain":            "plain",
		"  a\n\n b  ":      "a b",
		"tab\there":        "tab here",
		"Фото\r\né":        "Фото é",
		"\x7fdel":          "del",
		"keep  two spaces": "keep  two spaces",
	}
	for in, want := range cases {
		if got := cleanValue(in); got != want {
			t.Errorf("cleanValue(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckArgsAndExecRefuseLineBreaks(t *testing.T) {
	if err := CheckArgs([]string{"-m", "/tmp/a\n-execute"}); err == nil {
		t.Fatal("expected error")
	}
	if err := CheckArgs([]string{"-m", "/tmp/ok.jpg"}); err != nil {
		t.Fatal(err)
	}
	var c Client
	if _, err := c.Exec([]string{"/tmp/x\n-execute"}, 1); err == nil {
		t.Fatal("Exec must refuse before writing")
	}
}
