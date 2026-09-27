package exiftool

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"

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
	args, err := readArgs(paths, tags, numeric)
	if err != nil {
		return nil, err
	}
	r, err := c.Run(args, id)
	if err != nil {
		return nil, err
	}
	return parseRows(r.Out)
}

// readArgs builds a -json read of paths. Paths travel as absolute paths.
func readArgs(paths, tags []string, numeric bool) ([]string, error) {
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
	return args, nil
}

// parseRows decodes the -json output of one read.
func parseRows(out string) ([]map[string]any, error) {
	out = strings.TrimSpace(out)
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

// PathKey is the one form used to match a path Go built against the SourceFile
// ExifTool reports. ReadJSON sends absolute paths, so a relative path is made
// absolute first. ExifTool turns backslashes into forward slashes on Windows,
// and NTFS and APFS compare names without regard to case or normalization.
func PathKey(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
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
