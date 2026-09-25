package dates

import (
	"testing"
	"time"
)

func TestFilenameBounds(t *testing.T) {
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	if !FromFilename("IMG_20190509_154733.jpg", now).OK {
		t.Fatal("img")
	}
	if !FromFilename("Screenshot_20190919-053857.jpg", now).OK {
		t.Fatal("screenshot")
	}
	if !FromFilename("2016_01_30_11_49_15.mp4", now).OK {
		t.Fatal("underscores")
	}
	if FromFilename("VID_99550228_013014_396.mp4", now).OK {
		t.Fatal("impossible year must be rejected")
	}
	if FromFilename("-2869162354743691867.mp4", now).OK {
		t.Fatal("leading dash")
	}
	w := FromFilename("1483121459293.jpg", now)
	if !w.OK || w.Year != 2016 {
		t.Fatalf("unix ms %+v", w)
	}
	w = FromFilename("IMG_20161230_120000.jpg", now)
	if w.Year != 2016 || w.OffsetKnown {
		t.Fatalf("filename offset %+v", w)
	}
}

func TestDTOMinusUTC(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	utc := time.Date(2016, 12, 30, 14, 55, 37, 0, time.UTC)
	wall := time.Date(2016, 12, 30, 17, 55, 37, 0, time.UTC)
	w := FromSidecar(Sidecar{Taken: &utc}, Embedded{HasDTO: true, DTO: &wall}, now)
	if w.TZStep != TZDelta || w.Offset != 3*time.Hour || w.Year != 2016 {
		t.Fatalf("%+v", w)
	}
	// not a 15-minute step
	bad := wall.Add(7 * time.Minute)
	w = FromSidecar(Sidecar{Taken: &utc}, Embedded{HasDTO: true, DTO: &bad}, now)
	if w.TZStep == TZDelta {
		t.Fatal("non 15-minute delta accepted")
	}
}

func TestGPSZoneHistorical(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Saint Petersburg-like point. 2012 was UTC+4, 2016 is UTC+3.
	old := time.Date(2012, 6, 1, 12, 0, 0, 0, time.UTC)
	w := FromSidecar(Sidecar{Taken: &old, HasGeo: true, Lat: 55.75, Lon: 37.62}, Embedded{}, now)
	if w.TZStep != TZGPS || w.Offset != 4*time.Hour {
		t.Fatalf("2012 %+v", w)
	}
	newer := time.Date(2016, 6, 1, 12, 0, 0, 0, time.UTC)
	w = FromSidecar(Sidecar{Taken: &newer, HasGeo: true, Lat: 55.75, Lon: 37.62}, Embedded{}, now)
	if w.Offset != 3*time.Hour {
		t.Fatalf("2016 %+v", w)
	}
}

func TestNewYearLocal(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 2017-01-01 00:30 at +03 is still 2016-12-31 21:30 UTC
	utc := time.Date(2016, 12, 31, 21, 30, 0, 0, time.UTC)
	w := FromSidecar(Sidecar{Taken: &utc, HasGeo: true, Lat: 55.75, Lon: 37.62}, Embedded{}, now)
	if w.Year != 2017 {
		t.Fatalf("year %d", w.Year)
	}
}

func TestNeighborAndDefault(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	knownAt := time.Date(2016, 6, 1, 12, 0, 0, 0, time.UTC)
	known := FromSidecar(Sidecar{Taken: &knownAt, HasGeo: true, Lat: 55.75, Lon: 37.62}, Embedded{}, now)
	lonelyAt := knownAt.Add(2 * time.Hour)
	lonely := FromSidecar(Sidecar{Taken: &lonelyAt}, Embedded{}, now)
	if lonely.OffsetKnown {
		t.Fatal("should wait for fallback")
	}
	files := []When{known, lonely}
	ApplyFallback(files, "")
	if files[1].TZStep != TZNeighbor || files[1].Offset != 3*time.Hour {
		t.Fatalf("neighbor %+v", files[1])
	}
	far := FromSidecar(Sidecar{Taken: ptr(time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC))}, Embedded{}, now)
	ApplyFallback([]When{far}, "Europe/Moscow")
	if far.TZStep != TZDefault {
		// ApplyFallback mutates the slice copy's elements if we pass slice of values... we pass []When{far} which copies far
	}
	one := []When{far}
	ApplyFallback(one, "Europe/Moscow")
	if one[0].TZStep != TZDefault || !one[0].OffsetKnown {
		t.Fatalf("default %+v", one[0])
	}
	none := []When{far}
	ApplyFallback(none, "")
	if none[0].TZStep != TZUTC {
		t.Fatalf("utc last %+v", none[0])
	}
}

func TestOutOfRangeJSON(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	old := time.Unix(0, 0)
	w := FromSidecar(Sidecar{Taken: &old}, Embedded{}, now)
	if w.OK {
		t.Fatal("epoch must fail bounds")
	}
}

func ptr(t time.Time) *time.Time { return &t }

func TestFromFilenameDateOnly(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"IMG-20170203-WA0026.jpg":                             "2017-02-03 12:00",
		"VID-20180101-WA0001.mp4":                             "2018-01-01 12:00",
		"2022-04-21_640fea6c-bb0a-cf02-951c-00d09ac2d3cc.jpg": "2022-04-21 12:00",
		"2021-12-31 party.jpg":                                "2021-12-31 12:00",
		"20200615_beach.jpg":                                  "2020-06-15 12:00",
	}
	for name, want := range cases {
		w := FromFilename(name, now)
		if !w.OK || w.Source != SrcFilenameDate || w.Instant.Format("2006-01-02 15:04") != want || w.OffsetKnown {
			t.Errorf("%s: %+v", name, w)
		}
	}
	for _, name := range []string{
		"0bca7b90-299e-4000-b29a-d97037b18456.jpg", "20200615_101010.jpg" + "x", "2022-13-45_x.jpg",
		"IMG-99990101-WA0001.jpg", "photo-2022-04-21.jpg", "20200615123.jpg",
	} {
		w := FromFilename(name, now)
		if w.OK && w.Source == SrcFilenameDate {
			t.Errorf("%s matched as date-only: %+v", name, w)
		}
	}
	// A full timestamp still wins over the date-only rule.
	if w := FromFilename("20200615_101010.jpg", now); w.Source != SrcFilename {
		t.Fatalf("full timestamp: %+v", w)
	}
}

func TestPinWallClockKeepsWallClock(t *testing.T) {
	w := FromFilename("VID_20190914_160000.mp4", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !w.OK || w.OffsetKnown {
		t.Fatalf("want a floating filename date, got %+v", w)
	}
	PinWallClock(&w, "Europe/Berlin")
	if !w.OffsetKnown || w.Offset != 2*time.Hour || w.TZStep != TZDefault {
		t.Fatalf("offset %v known %v step %s", w.Offset, w.OffsetKnown, w.TZStep)
	}
	if got := w.Local().Format("2006-01-02 15:04"); got != "2019-09-14 16:00" {
		t.Fatalf("wall clock %s", got)
	}
	if got := w.Instant.UTC().Format("15:04"); got != "14:00" {
		t.Fatalf("instant %s", got)
	}
	u := FromFilename("VID_20190914_160000.mp4", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	PinWallClock(&u, "")
	if u.TZStep != TZUTC || u.Offset != 0 || u.Local().Hour() != 16 {
		t.Fatalf("UTC fallback %+v", u)
	}
}

func TestTakenInBoundsAcceptsOldScans(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if !TakenInBounds(time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC), now) {
		t.Error("1965 rejected")
	}
	if TakenInBounds(time.Unix(0, 0), now) {
		t.Error("Unix time 0 accepted")
	}
	if TakenInBounds(time.Date(1799, 12, 31, 0, 0, 0, 0, time.UTC), now) {
		t.Error("1799 accepted")
	}
	if InBounds(time.Date(1965, 6, 1, 12, 0, 0, 0, time.UTC), now) {
		t.Error("InBounds must still reject 1965 for file names and camera clocks")
	}
}

// A camera date with no offset is a wall clock. The fallback zone must not
// shift it into the next year.
func TestFallbackKeepsEmbeddedWallClock(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	dto := time.Date(2019, 12, 31, 23, 30, 0, 0, time.UTC)
	w := FromEmbedded(Embedded{DTO: &dto, HasDTO: true}, now)
	ws := []When{w}
	ApplyFallback(ws, "Europe/Moscow")
	if ws[0].Year != 2019 || ws[0].Local().Format("2006-01-02 15:04") != "2019-12-31 23:30" {
		t.Fatalf("year %d local %s", ws[0].Year, ws[0].Local())
	}
	PinWallClock(&ws[0], "Europe/Moscow")
	if got := ws[0].Local().Format("2006-01-02 15:04 -07:00"); got != "2019-12-31 23:30 +03:00" {
		t.Fatalf("pinned %s", got)
	}
}
