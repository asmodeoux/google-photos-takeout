package exiftool

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/text/unicode/norm"
)

// ReadBatch is how many files one -execute block reads.
const ReadBatch = 40

// ReadJSON reads tags from paths through the stay_open process. Paths travel
// in the argument stream, so non-ASCII names reach ExifTool as UTF-8 on every OS.
// Files ExifTool cannot read are missing from the result, not an error.
func (c *Client) ReadJSON(paths, tags []string, numeric bool, id int) ([]map[string]any, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	args := []string{"-api", "QuickTimeUTC=1", "-json"}
	if numeric {
		args = append(args, "-n")
	}
	for _, t := range tags {
		args = append(args, "-"+strings.TrimPrefix(t, "-"))
	}
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		args = append(args, abs)
	}
	r, err := c.Run(args, id)
	if err != nil {
		return nil, err
	}
	out := strings.TrimSpace(r.Out)
	if out == "" {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(out), &rows); err != nil {
		if len(out) > 200 {
			out = out[:200]
		}
		return nil, fmt.Errorf("exiftool returned unreadable JSON: %s", out)
	}
	return rows, nil
}

// ReadAll reads paths in batches spread over every client and returns rows keyed
// by PathKey(SourceFile). Batches that fail are reported; the others still count.
func ReadAll(clients []*Client, paths, tags []string, numeric bool) (map[string]map[string]any, []error) {
	rows := map[string]map[string]any{}
	if len(clients) == 0 || len(paths) == 0 {
		return rows, nil
	}
	var (
		mu     sync.Mutex
		errs   []error
		wg     sync.WaitGroup
		nextID atomic.Int64
	)
	batches := make(chan []string)
	for _, c := range clients {
		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			for b := range batches {
				got, err := c.ReadJSON(b, tags, numeric, int(nextID.Add(1)))
				mu.Lock()
				if err != nil {
					errs = append(errs, err)
				}
				for _, row := range got {
					if src, ok := row["SourceFile"].(string); ok {
						rows[PathKey(src)] = row
					}
				}
				mu.Unlock()
			}
		}(c)
	}
	for start := 0; start < len(paths); start += ReadBatch {
		end := min(start+ReadBatch, len(paths))
		batches <- paths[start:end]
	}
	close(batches)
	wg.Wait()
	return rows, errs
}

// PathKey is the one form used to match a path Go built against the SourceFile
// ExifTool reports. ExifTool turns backslashes into forward slashes on Windows,
// and NTFS and APFS compare names without regard to case or normalization.
func PathKey(p string) string {
	return pathKey(p, runtime.GOOS == "windows")
}

func pathKey(p string, windows bool) string {
	if !windows {
		// On macOS and Linux a backslash is an ordinary filename character.
		return norm.NFC.String(filepath.Clean(p))
	}
	p = strings.ReplaceAll(p, `\`, "/")
	switch {
	case strings.HasPrefix(p, "//?/UNC/"):
		p = "//" + p[len("//?/UNC/"):]
	case strings.HasPrefix(p, "//?/"):
		p = p[len("//?/"):]
	}
	unc := strings.HasPrefix(p, "//")
	p = path.Clean(p)
	if unc && !strings.HasPrefix(p, "//") {
		p = "/" + p
	}
	return strings.ToLower(norm.NFC.String(p))
}
