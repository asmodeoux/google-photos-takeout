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
