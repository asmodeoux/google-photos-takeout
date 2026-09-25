package media

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SetTimes sets the modification time everywhere and the creation time where
// the system has one (macOS, Windows), which Finder, Explorer and photo apps
// fall back to.
func TestSetTimesSetsModifiedAndCreated(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.jpg")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := time.Date(2019, 9, 1, 10, 0, 0, 0, time.UTC)
	if err := SetTimes(p, want); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if !st.ModTime().Equal(want) {
		t.Errorf("modified %s, want %s", st.ModTime().UTC(), want)
	}
	if got, ok := birthTime(t, p); ok && !got.Equal(want) {
		t.Errorf("created %s, want %s", got.UTC(), want)
	}
	// macOS moves the creation date back by itself when the modified date is
	// set earlier, so a later date is what shows that SetTimes sets it.
	later := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	if err := SetTimes(p, later); err != nil {
		t.Fatal(err)
	}
	if got, ok := birthTime(t, p); ok && !got.Equal(later) {
		t.Errorf("created %s, want %s", got.UTC(), later.UTC())
	}
}
