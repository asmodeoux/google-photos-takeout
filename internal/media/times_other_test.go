//go:build !darwin && !windows

package media

import (
	"testing"
	"time"
)

// Linux has no settable creation time.
func birthTime(t *testing.T, p string) (time.Time, bool) { return time.Time{}, false }
