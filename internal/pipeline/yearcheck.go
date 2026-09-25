package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
)

// YearMismatches lists files under a four-digit year folder whose embedded
// capture date is a different year. A file in results/2018 must say 2018.
func YearMismatches(clients []*exiftool.Client, root string) ([]string, error) {
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
	rows, errs := exiftool.ReadAll(clients, files, []string{"DateTimeOriginal", "CreationDate", "XMP:DateCreated"}, false)
	if len(errs) > 0 {
		return nil, fmt.Errorf("exiftool: %w", errs[0])
	}
	var bad []string
	for _, p := range files {
		row, ok := rows[exiftool.PathKey(p)]
		if !ok {
			bad = append(bad, fmt.Sprintf("%s is in %s but has no readable date", filepath.Base(p), years[p]))
			continue
		}
		got := tagYear(fmt.Sprint(row["DateTimeOriginal"]))
		if got == "" {
			got = tagYear(fmt.Sprint(row["CreationDate"]))
		}
		if got == "" {
			got = tagYear(fmt.Sprint(row["DateCreated"]))
		}
		if got != years[p] {
			bad = append(bad, fmt.Sprintf("%s is in %s but its date is %s", filepath.Base(p), years[p], orMissing(got)))
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
