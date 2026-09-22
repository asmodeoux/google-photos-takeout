package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// YearMismatches lists files under a four-digit year folder whose embedded
// capture date is a different year. A file in results/2018 must say 2018.
func YearMismatches(root string) ([]string, error) {
	var files []string
	years := map[string]string{}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() || !yearName(e.Name()) {
			continue
		}
		err := filepath.WalkDir(filepath.Join(root, e.Name()), func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			abs, err := filepath.Abs(p)
			if err != nil {
				abs = p
			}
			files = append(files, abs)
			years[abs] = e.Name()
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	var bad []string
	for start := 0; start < len(files); start += 40 {
		end := start + 40
		if end > len(files) {
			end = len(files)
		}
		args := append([]string{"-api", "QuickTimeUTC=1", "-json", "-DateTimeOriginal", "-CreationDate", "-XMP:DateCreated"}, files[start:end]...)
		out, err := exec.Command("exiftool", args...).Output()
		if err != nil {
			return bad, fmt.Errorf("exiftool: %w", err)
		}
		var rows []map[string]any
		if json.Unmarshal(out, &rows) != nil {
			return bad, fmt.Errorf("exiftool returned unreadable JSON")
		}
		seen := map[string]bool{}
		for _, row := range rows {
			src, _ := row["SourceFile"].(string)
			seen[src] = true
			got := tagYear(fmt.Sprint(row["DateTimeOriginal"]))
			if got == "" {
				got = tagYear(fmt.Sprint(row["CreationDate"]))
			}
			if got == "" {
				got = tagYear(fmt.Sprint(row["DateCreated"]))
			}
			want := years[src]
			if got == "" || got != want {
				bad = append(bad, fmt.Sprintf("%s is in %s but its date is %s", filepath.Base(src), want, orMissing(got)))
			}
		}
		for _, p := range files[start:end] {
			if !seen[p] {
				bad = append(bad, fmt.Sprintf("%s is in %s but has no readable date", filepath.Base(p), years[p]))
			}
		}
	}
	return bad, nil
}

func yearName(name string) bool {
	if len(name) != 4 {
		return false
	}
	n, err := strconv.Atoi(name)
	return err == nil && n >= 1800 && n <= 2100
}

func tagYear(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "<nil>" {
		return ""
	}
	if len(s) >= 4 && yearName(s[:4]) {
		return s[:4]
	}
	return ""
}

func orMissing(y string) string {
	if y == "" {
		return "missing"
	}
	return y
}
