// Package exiftool builds ExifTool arguments and talks to a stay_open process.
package exiftool

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/dates"
)

// Plan is one file's tag write. Dates are omitted when WriteDates is false
// (fill, don't clobber). ContentIdentifier is never cleared.
type Plan struct {
	Path         string
	Kind         string // jpeg, png, gif, webp, heic, mp4, mov
	WriteDates   bool
	WriteGPS     bool
	When         dates.When
	Description  string
	CopyIDFrom   string // absolute path to copy ContentIdentifier from
	SetContentID string
}

// Args returns one stay_open block, without the trailing -execute.
// Paths are absolute so a name that starts with "-" is not an option.
func Args(p Plan) ([]string, error) {
	abs, err := filepath.Abs(p.Path)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(abs) {
		return nil, fmt.Errorf("path must be absolute: %s", p.Path)
	}
	// -m ignores minor metadata damage already in the file, such as a broken
	// IFD pointer. The caller still treats "0 image files updated" as a failure.
	a := []string{"-m", "-overwrite_original", "-api", "QuickTimeUTC=1"}

	if p.CopyIDFrom != "" {
		from, err := filepath.Abs(p.CopyIDFrom)
		if err != nil {
			return nil, err
		}
		a = append(a, "-TagsFromFile", from, "-Keys:ContentIdentifier<ContentIdentifier")
	}
	if p.WriteDates && p.When.OK {
		local := p.When.Local().Format("2006:01:02 15:04:05")
		utc := p.When.Instant.UTC().Format("2006:01:02 15:04:05")
		off := ""
		if p.When.OffsetKnown {
			off = dates.FormatOffset(p.When.Offset)
		}
		video := p.Kind == "mp4" || p.Kind == "mov"
		switch {
		case p.Kind == "gif":
			if off != "" {
				a = append(a, "-XMP:DateCreated="+local+off)
			} else {
				a = append(a, "-XMP:DateCreated="+local)
			}
		case video:
			if off != "" {
				a = append(a, "-Keys:CreationDate="+local+off)
			} else {
				a = append(a, "-Keys:CreationDate="+local)
			}
			a = append(a,
				"-QuickTime:CreateDate="+utc,
				"-QuickTime:ModifyDate="+utc,
				"-QuickTime:TrackCreateDate="+utc,
				"-QuickTime:TrackModifyDate="+utc,
				"-QuickTime:MediaCreateDate="+utc,
				"-QuickTime:MediaModifyDate="+utc,
			)
		default:
			a = append(a,
				"-ExifIFD:DateTimeOriginal="+local,
				"-ExifIFD:CreateDate="+local,
				"-IFD0:ModifyDate="+local,
			)
			if off != "" {
				a = append(a,
					"-ExifIFD:OffsetTimeOriginal="+off,
					"-ExifIFD:OffsetTimeDigitized="+off,
					"-ExifIFD:OffsetTime="+off,
				)
			}
			if p.Kind == "png" {
				x := local
				if off != "" {
					x = local + off
				}
				a = append(a,
					"-XMP-exif:DateTimeOriginal="+x,
					"-XMP-photoshop:DateCreated="+x,
				)
			}
		}
	}
	if p.WriteGPS && p.When.HasGPS {
		latRef, lonRef := "N", "E"
		lat, lon := p.When.Lat, p.When.Lon
		if lat < 0 {
			latRef = "S"
			lat = -lat
		}
		if lon < 0 {
			lonRef = "W"
			lon = -lon
		}
		video := p.Kind == "mp4" || p.Kind == "mov"
		if video {
			a = append(a, fmt.Sprintf("-Keys:GPSCoordinates=%.6f %.6f", p.When.Lat, p.When.Lon))
		} else if p.Kind != "gif" {
			a = append(a,
				fmt.Sprintf("-GPSLatitude=%.6f", lat),
				"-GPSLatitudeRef="+latRef,
				fmt.Sprintf("-GPSLongitude=%.6f", lon),
				"-GPSLongitudeRef="+lonRef,
			)
			if p.When.HasAlt {
				ref := "0"
				alt := p.When.Alt
				if alt < 0 {
					ref = "1"
					alt = -alt
				}
				a = append(a, fmt.Sprintf("-GPSAltitude=%.3f", alt), "-GPSAltitudeRef="+ref)
			}
		}
	}
	if id := cleanValue(p.SetContentID); id != "" {
		if videoKind(p.Kind) {
			a = append(a, "-Keys:ContentIdentifier="+id)
		} else {
			a = append(a, "-Apple:ContentIdentifier="+id)
		}
	}
	if d := cleanValue(p.Description); d != "" && p.Kind != "gif" {
		a = append(a, "-ImageDescription="+d, "-XMP:Description="+d)
	}
	a = append(a, abs)
	if err := CheckArgs(a); err != nil {
		return nil, err
	}
	return a, nil
}

// cleanValue turns line breaks and other control characters into single spaces.
// Each stay_open argument is one line, so a line break would start a new argument.
func cleanValue(s string) string {
	var b strings.Builder
	broke := false // inside a run of control characters and the spaces around it
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			broke = true
			continue
		}
		if broke {
			if r == ' ' {
				continue
			}
			out := strings.TrimRight(b.String(), " ")
			b.Reset()
			b.WriteString(out)
			if out != "" {
				b.WriteByte(' ')
			}
			broke = false
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// CheckArgs refuses any argument that would split into two stay_open lines.
func CheckArgs(args []string) error {
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return fmt.Errorf("unsafe exiftool argument %q contains a line break", a)
		}
	}
	return nil
}

func videoKind(k string) bool { return k == "mp4" || k == "mov" }

// ShouldWriteDates is the fill-don't-clobber rule.
// Missing or more than 60s away from the sidecar instant: write.
// Within 2s: leave the existing date alone.
func ShouldWriteDates(existing *time.Time, want dates.When) bool {
	if !want.OK {
		return false
	}
	if existing == nil || existing.IsZero() {
		return true
	}
	d := existing.UTC().Sub(want.Instant)
	if d < 0 {
		d = -d
	}
	if d <= 2*time.Second {
		return false
	}
	return d > 60*time.Second
}

// ParseReady finds {readyN} in an ExifTool stay_open reply.
func ParseReady(out string) (id int, ok bool) {
	i := strings.LastIndex(out, "{ready")
	if i < 0 {
		return 0, false
	}
	rest := out[i+len("{ready"):]
	j := strings.IndexByte(rest, '}')
	if j < 0 {
		return 0, false
	}
	num := rest[:j]
	if num == "" {
		return 0, true
	}
	fmt.Sscanf(num, "%d", &id)
	return id, true
}

// Updated reports whether ExifTool changed or accepted the file.
func Updated(out string) bool {
	return strings.Contains(out, "1 image files updated") ||
		strings.Contains(out, "1 image files unchanged") ||
		strings.Contains(out, "1 image files created")
}
