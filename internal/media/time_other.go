//go:build !darwin && !windows

package media

import "time"

func setBirth(path string, t time.Time) error { return nil }
