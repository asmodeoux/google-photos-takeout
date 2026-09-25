// Package dates turns a Takeout sidecar, embedded tags, or a filename into a
// capture instant and the timezone Apple Photos should see.
package dates

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/ringsaturn/tzf"
)

const (
	SrcTaken    = "json-taken"
	SrcCreation = "json-creation"
	SrcGroup    = "group"
	SrcLive     = "live"
	SrcEmbedded = "embedded"
	SrcFilename = "filename"
	// SrcFilenameDate is a name with a day but no time, such as WhatsApp's
	// IMG-20170203-WA0026.jpg. The time is set to 12:00 so no timezone moves
	// the photo to another day.
	SrcFilenameDate = "filename-date"
	SrcUnknown      = "unknown"

	TZGPS      = "gps"
	TZEmbedded = "embedded-offset"
	TZDelta    = "dto-minus-utc"
	TZNeighbor = "neighbor"
	TZDefault  = "default"
	TZCommon   = "common"
	TZUTC      = "utc"
	TZFilename = "filename"
	TZNone     = "none"
)

// When is the capture time written into a file and used for the year folder.
type When struct {
	Instant     time.Time
	Offset      time.Duration
	OffsetKnown bool
	// Year is the calendar year at the place the photo was taken.
	// A UTC instant on New Year's Eve can be the next year locally.
	Year        int
	Source      string
	TZStep      string
	Lat, Lon    float64
	Alt         float64
	HasGPS      bool
	HasAlt      bool
	Description string
	OK          bool
}

// Local is the wall clock to write into DateTimeOriginal.
func (w When) Local() time.Time {
	if !w.OK {
		return time.Time{}
	}
	if w.OffsetKnown {
		return w.Instant.In(time.FixedZone("", int(w.Offset.Seconds())))
	}
	return w.Instant.UTC()
}

// Sidecar is the subset of a Takeout JSON file used for dating.
type Sidecar struct {
	Title       string
	Description string
	Taken       *time.Time
	Creation    *time.Time
	Lat, Lon    float64
	Alt         float64
	HasGeo      bool
	HasAlt      bool
	URL         string
}

// Embedded is what ExifTool already found in the file.
type Embedded struct {
	DTO       *time.Time // wall clock, location unknown
	Offset    *time.Duration
	HasDTO    bool
	Create    *time.Time
	Lat, Lon  float64
	HasGPS    bool
	ContentID string
}

// Finder looks up a timezone from coordinates. Tests can substitute it.
type Finder interface {
	GetTimezoneName(lng, lat float64) string
}

var defaultFinder Finder

func finder() Finder {
	if defaultFinder != nil {
		return defaultFinder
	}
	f, err := tzf.NewDefaultFinder()
	if err != nil {
		return nil
	}
	defaultFinder = f
	return f
}

// SetFinder replaces the timezone lookup. Tests use this.
func SetFinder(f Finder) { defaultFinder = f }

// InBounds is 1990-01-01 through now+1 day.
func InBounds(t time.Time, now time.Time) bool {
	if t.IsZero() {
		return false
	}
	start := time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	end := now.UTC().Add(24 * time.Hour)
	u := t.UTC()
	return !u.Before(start) && !u.After(end)
}

// TakenInBounds accepts a sidecar photoTakenTime from 1800 on. Google stores
// the date a user set on a scanned photo there, so it can be long before any
// camera. Unix time 0 means Google had no date.
func TakenInBounds(t time.Time, now time.Time) bool {
	if t.IsZero() || t.Unix() == 0 {
		return false
	}
	u := t.UTC()
	return !u.Before(time.Date(1800, 1, 1, 0, 0, 0, 0, time.UTC)) && !u.After(now.UTC().Add(24*time.Hour))
}

// FromSidecar applies GPS, an embedded offset, and the camera-clock minus JSON UTC.
// Neighbor, default, and UTC are filled in later by ResolveChain.
func FromSidecar(sc Sidecar, emb Embedded, now time.Time) When {
	var instant time.Time
	src := ""
	if sc.Taken != nil && TakenInBounds(*sc.Taken, now) {
		instant = sc.Taken.UTC()
		src = SrcTaken
	} else if sc.Creation != nil && InBounds(*sc.Creation, now) {
		instant = sc.Creation.UTC()
		src = SrcCreation
	}
	w := When{Source: src, Description: sc.Description}
	if sc.HasGeo {
		w.HasGPS = true
		w.Lat, w.Lon, w.Alt, w.HasAlt = sc.Lat, sc.Lon, sc.Alt, sc.HasAlt
	} else if emb.HasGPS {
		w.HasGPS = true
		w.Lat, w.Lon = emb.Lat, emb.Lon
	}
	if src == "" {
		return w
	}
	w.Instant = instant
	w.OK = true
	if w.HasGPS {
		if name := zoneName(w.Lon, w.Lat); name != "" {
			if loc, err := time.LoadLocation(name); err == nil {
				_, off := instant.In(loc).Zone()
				w.Offset = time.Duration(off) * time.Second
				w.OffsetKnown = true
				w.TZStep = TZGPS
				w.Year = instant.In(loc).Year()
				return w
			}
		}
	}
	if emb.Offset != nil && validOffset(*emb.Offset) {
		w.Offset = *emb.Offset
		w.OffsetKnown = true
		w.TZStep = TZEmbedded
		w.Year = w.Local().Year()
		return w
	}
	if emb.HasDTO && emb.DTO != nil {
		// DTO is a naive wall clock. Compare its numbers to the UTC instant.
		wall := time.Date(emb.DTO.Year(), emb.DTO.Month(), emb.DTO.Day(), emb.DTO.Hour(), emb.DTO.Minute(), emb.DTO.Second(), 0, time.UTC)
		delta := wall.Sub(instant)
		if validOffset(delta) {
			w.Offset = delta
			w.OffsetKnown = true
			w.TZStep = TZDelta
			w.Year = w.Local().Year()
			return w
		}
	}
	w.TZStep = ""
	w.Year = instant.UTC().Year()
	return w
}

func zoneName(lon, lat float64) string {
	f := finder()
	if f == nil {
		return ""
	}
	return f.GetTimezoneName(lon, lat)
}

func validOffset(d time.Duration) bool {
	if d < -14*time.Hour || d > 14*time.Hour {
		return false
	}
	if d%time.Minute != 0 {
		return false
	}
	m := int(d.Minutes())
	if m < 0 {
		m = -m
	}
	return m%15 == 0
}

// ApplyFallback fills timezone steps 4-6 for instants that still have no zone.
// files is every When in the library. defaultTZ is --default-tz, or "" to use
// the most common zone already resolved.
func ApplyFallback(files []When, defaultTZ string) {
	type known struct {
		at  time.Time
		off time.Duration
		loc string
	}
	var knowns []known
	counts := map[string]int{}
	for _, w := range files {
		if w.OK && w.OffsetKnown && w.TZStep != TZFilename {
			knowns = append(knowns, known{at: w.Instant, off: w.Offset})
			counts[w.TZStep+"|"+w.Offset.String()]++
		}
	}
	modeOff := time.Duration(0)
	modeN := 0
	for _, k := range knowns {
		n := 0
		for _, o := range knowns {
			if o.off == k.off {
				n++
			}
		}
		if n > modeN {
			modeN = n
			modeOff = k.off
		}
	}
	for i := range files {
		w := &files[i]
		// A wall clock with no zone (a file name, or a camera date without an
		// offset) is stored as if it were UTC. Adding an offset here would
		// move it; it stays a wall clock, and videos pin it with PinWallClock.
		if !w.OK || w.OffsetKnown || w.TZStep == TZFilename || w.Source == SrcFilename || w.Source == SrcFilenameDate || w.Source == SrcUnknown || w.Source == "" {
			continue
		}
		best := time.Duration(1 << 62)
		var off time.Duration
		found := false
		for _, k := range knowns {
			d := w.Instant.Sub(k.at)
			if d < 0 {
				d = -d
			}
			if d <= 36*time.Hour && d < best {
				best = d
				off = k.off
				found = true
			}
		}
		if found {
			w.Offset = off
			w.OffsetKnown = true
			w.TZStep = TZNeighbor
			w.Year = w.Local().Year()
			continue
		}
		if defaultTZ != "" {
			if loc, err := time.LoadLocation(defaultTZ); err == nil {
				_, s := w.Instant.In(loc).Zone()
				w.Offset = time.Duration(s) * time.Second
				w.OffsetKnown = true
				w.TZStep = TZDefault
				w.Year = w.Local().Year()
				continue
			}
		}
		if modeN > 0 {
			w.Offset = modeOff
			w.OffsetKnown = true
			w.TZStep = TZCommon
			w.Year = w.Local().Year()
			continue
		}
		w.Offset = 0
		w.OffsetKnown = true
		w.TZStep = TZUTC
		w.Year = w.Instant.UTC().Year()
	}
}

// PinWallClock gives a date with no known offset the offset of defaultTZ (UTC
// when empty or invalid) at the same wall-clock time. Videos need it: their
// creation date must carry a zone, and without one ExifTool would use the
// computer's zone.
func PinWallClock(w *When, defaultTZ string) {
	if !w.OK || w.OffsetKnown {
		return
	}
	wall := w.Instant.UTC()
	loc, step := time.UTC, TZUTC
	if defaultTZ != "" {
		if l, err := time.LoadLocation(defaultTZ); err == nil {
			loc, step = l, TZDefault
		}
	}
	t := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), wall.Second(), wall.Nanosecond(), loc)
	_, s := t.Zone()
	w.Instant = t
	w.Offset = time.Duration(s) * time.Second
	w.OffsetKnown = true
	w.TZStep = step
	w.Year = wall.Year()
}

var patterns = []*regexp.Regexp{
	regexp.MustCompile(`(?P<d>(?:20|19|18)\d{2}(?:0[1-9]|1[0-2])[0-3]\d-[0-2]\d[0-5]\d[0-5]\d)`),
	regexp.MustCompile(`(?P<d>(?:20|19|18)\d{2}(?:0[1-9]|1[0-2])[0-3]\d_[0-2]\d[0-5]\d[0-5]\d)`),
	regexp.MustCompile(`(?P<d>(?:20|19|18)\d{2}-(?:0[1-9]|1[0-2])-[0-3]\d-[0-2]\d-[0-5]\d-[0-5]\d)`),
	regexp.MustCompile(`(?P<d>(?:20|19|18)\d{2}-(?:0[1-9]|1[0-2])-[0-3]\d-[0-2]\d[0-5]\d[0-5]\d)`),
	regexp.MustCompile(`(?P<d>(?:20|19|18)\d{2}(?:0[1-9]|1[0-2])[0-3]\d[0-2]\d[0-5]\d[0-5]\d)`),
	regexp.MustCompile(`(?P<d>(?:20|19|18)\d{2}_(?:0[1-9]|1[0-2])_[0-3]\d_[0-2]\d_[0-5]\d_[0-5]\d)`),
}

var layouts = []string{
	"20060102-150405",
	"20060102_150405",
	"2006-01-02-15-04-05",
	"2006-01-02-150405",
	"20060102150405",
	"2006_01_02_15_04_05",
}

// FromFilename reads a capture wall clock from a media name.
// Names that start with "-" are ignored. Impossible years are rejected.
func FromFilename(name string, now time.Time) When {
	base := name
	if len(base) > 0 && base[0] == '-' {
		return When{}
	}
	for i, re := range patterns {
		m := re.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		s := m[1]
		if layouts[i] == "20060102150405" && len(s) > 14 {
			s = s[:14]
		}
		t, err := time.ParseInLocation(layouts[i], s, time.UTC)
		if err != nil || !InBounds(t, now) {
			continue
		}
		return When{
			Instant: t, OK: true, Year: t.Year(),
			Source: SrcFilename, TZStep: TZFilename,
		}
	}
	// 13-digit unix milliseconds, not part of a longer digit run we already tried
	ms := regexp.MustCompile(`(?:^|[^\d])(\d{13})(?:[^\d]|$)`)
	if m := ms.FindStringSubmatch(base); m != nil {
		n, err := strconv.ParseInt(m[1], 10, 64)
		if err == nil {
			t := time.UnixMilli(n).UTC()
			if InBounds(t, now) {
				return When{Instant: t, OK: true, Year: t.Year(), Source: SrcFilename, TZStep: TZFilename, OffsetKnown: false}
			}
		}
	}
	return fromDateOnlyName(base, now)
}

// dateOnly are names that carry a day but no time. Each must anchor at the
// start of the name, so hex IDs and UUIDs never match.
var dateOnly = []struct {
	re     *regexp.Regexp
	layout string
}{
	{regexp.MustCompile(`^(?:IMG|VID|AUD|PTT|STK)-((?:19|20)\d{6})-WA\d+`), "20060102"},
	{regexp.MustCompile(`^((?:19|20)\d{2}-[01]\d-[0-3]\d)[_ ]`), "2006-01-02"},
	{regexp.MustCompile(`^((?:19|20)\d{2}[01]\d[0-3]\d)_[^\d]`), "20060102"},
}

func fromDateOnlyName(base string, now time.Time) When {
	for _, d := range dateOnly {
		m := d.re.FindStringSubmatch(base)
		if m == nil {
			continue
		}
		day, err := time.ParseInLocation(d.layout, m[1], time.UTC)
		if err != nil {
			continue
		}
		t := day.Add(12 * time.Hour)
		if !InBounds(t, now) {
			continue
		}
		return When{Instant: t, OK: true, Year: t.Year(), Source: SrcFilenameDate, TZStep: TZFilename}
	}
	return When{}
}

// FromEmbedded uses a date already stored in the file when no sidecar exists.
func FromEmbedded(emb Embedded, now time.Time) When {
	if emb.HasDTO && emb.DTO != nil && InBounds(*emb.DTO, now) {
		w := When{Source: SrcEmbedded, OK: true, TZStep: TZEmbedded}
		if emb.Offset != nil && validOffset(*emb.Offset) {
			wall := time.Date(emb.DTO.Year(), emb.DTO.Month(), emb.DTO.Day(), emb.DTO.Hour(), emb.DTO.Minute(), emb.DTO.Second(), 0, time.UTC)
			w.Instant = wall.Add(-*emb.Offset)
			w.Offset = *emb.Offset
			w.OffsetKnown = true
			w.Year = wall.Year()
		} else {
			wall := time.Date(emb.DTO.Year(), emb.DTO.Month(), emb.DTO.Day(), emb.DTO.Hour(), emb.DTO.Minute(), emb.DTO.Second(), 0, time.UTC)
			w.Instant = wall
			w.Year = wall.Year()
			w.TZStep = TZFilename
			w.OffsetKnown = false
		}
		if emb.HasGPS {
			w.HasGPS = true
			w.Lat, w.Lon = emb.Lat, emb.Lon
		}
		return w
	}
	return When{}
}

// SameInstant reports whether two times are within slack.
func SameInstant(a, b time.Time, slack time.Duration) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d <= slack
}

// FormatOffset renders +HH:MM.
func FormatOffset(d time.Duration) string {
	sec := int(d.Seconds())
	sign := "+"
	if sec < 0 {
		sign = "-"
		sec = -sec
	}
	return fmt.Sprintf("%s%02d:%02d", sign, sec/3600, (sec%3600)/60)
}

// DistinctTimes counts timestamps more than slack apart.
func DistinctTimes(ts []time.Time, slack time.Duration) int {
	var kept []time.Time
	for _, t := range ts {
		found := false
		for _, k := range kept {
			if SameInstant(t, k, slack) {
				found = true
				break
			}
		}
		if !found {
			kept = append(kept, t)
		}
	}
	return len(kept)
}
