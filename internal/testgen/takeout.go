package testgen

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// ExportID is the timestamp Google puts in every zip name of one export.
const ExportID = "20240101T000000Z"

// Takeout is an export being built: zip part -> entry name -> bytes.
type Takeout struct {
	parts map[int]map[string][]byte
	// Rows names each scenario row, for reports and failure messages.
	Rows []string
}

// New starts an empty export.
func New() *Takeout { return &Takeout{parts: map[int]map[string][]byte{}} }

// Root is where Google puts photos inside every zip.
const Root = "Takeout/Google Photos/"

// Put adds one zip entry under Takeout/Google Photos/folder in zip part n.
func (t *Takeout) Put(n int, folder, name string, data []byte) {
	if t.parts[n] == nil {
		t.parts[n] = map[string][]byte{}
	}
	t.parts[n][Root+folder+"/"+name] = data
}

// PutRaw adds an entry with an exact path, for files outside Google Photos or
// odd layouts.
func (t *Takeout) PutRaw(n int, entry string, data []byte) {
	if t.parts[n] == nil {
		t.parts[n] = map[string][]byte{}
	}
	t.parts[n][entry] = data
}

// Side is the content of one JSON sidecar.
type Side struct {
	Title       string
	Description string
	Taken       time.Time // photoTakenTime; zero leaves it out
	Created     time.Time // creationTime; zero leaves it out
	Lat, Lon    float64   // geoData; 0,0 is Google's "no location"
	NoGeoExif   bool      // leave geoDataExif out
}

// JSON renders a sidecar the way Google writes it, with string timestamps.
func (s Side) JSON() []byte {
	m := map[string]any{"title": s.Title, "description": s.Description, "imageViews": "0"}
	stamp := func(t time.Time) map[string]any {
		return map[string]any{
			"timestamp": strconv.FormatInt(t.Unix(), 10),
			"formatted": t.UTC().Format("Jan 2, 2006, 3:04:05 PM UTC"),
		}
	}
	if !s.Taken.IsZero() {
		m["photoTakenTime"] = stamp(s.Taken)
	}
	if !s.Created.IsZero() {
		m["creationTime"] = stamp(s.Created)
	}
	geo := map[string]any{"latitude": s.Lat, "longitude": s.Lon, "altitude": 0.0, "latitudeSpan": 0.0, "longitudeSpan": 0.0}
	m["geoData"] = geo
	if !s.NoGeoExif {
		m["geoDataExif"] = geo
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		panic(err)
	}
	return b
}

// Photo adds a media file and, when side is not nil, its sidecar named
// "<name>.supplemental-metadata.json" (the current Google convention).
func (t *Takeout) Photo(n int, folder, name string, data []byte, side *Side) {
	t.Put(n, folder, name, data)
	if side != nil {
		t.SideAs(n, folder, name+".supplemental-metadata.json", name, *side)
	}
}

// SideAs adds a sidecar with an exact file name. An empty Title becomes title.
func (t *Takeout) SideAs(n int, folder, jsonName, title string, side Side) {
	if side.Title == "" {
		side.Title = title
	}
	t.Put(n, folder, jsonName, side.JSON())
}

// Row records a scenario name.
func (t *Takeout) Row(name string) { t.Rows = append(t.Rows, name) }

// Write writes every part as takeout-<ExportID>-1-00N.zip in dir and returns
// the paths in part order.
func (t *Takeout) Write(dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var nums []int
	for n := range t.parts {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var paths []string
	for _, n := range nums {
		p := filepath.Join(dir, fmt.Sprintf("takeout-%s-1-%03d.zip", ExportID, n))
		if err := writeZip(p, t.parts[n]); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, nil
}

func writeZip(p string, entries map[string][]byte) error {
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	var names []string
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)})
		if err != nil {
			f.Close()
			return err
		}
		if _, err := w.Write(entries[name]); err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
