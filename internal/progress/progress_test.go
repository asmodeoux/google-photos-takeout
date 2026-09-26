package progress

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLiveLineFitsAndOverwrites(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, "tty", false)
	r.width = func() int { return 30 }
	r.phase = "tags"
	r.Tick(1, 10, "Очень длинное имя файла 写真写真写真写真写真.jpg")
	r.Tick(2, 10, "a.jpg")
	frames := strings.Split(buf.String(), "\r")[1:]
	if len(frames) != 2 {
		t.Fatalf("frames %q", frames)
	}
	for _, f := range frames {
		w := 0
		for _, c := range f {
			w += runeWidth(c)
		}
		if w > 29 {
			t.Errorf("frame %q is %d columns wide", f, w)
		}
	}
	long, short := frames[0], frames[1]
	if !strings.HasPrefix(short, "tags  2/10  a.jpg") {
		t.Errorf("short frame %q", short)
	}
	lw, sw := 0, 0
	for _, c := range long {
		lw += runeWidth(c)
	}
	for _, c := range short {
		sw += runeWidth(c)
	}
	if sw < lw || strings.TrimRight(short, " ") != "tags  2/10  a.jpg" {
		t.Errorf("short frame does not cover the long one: %d < %d (%q)", sw, lw, short)
	}
}

func TestRuneWidth(t *testing.T) {
	for c, want := range map[rune]int{'a': 1, 'Ф': 1, '写': 2, 'Ａ': 2, '́': 0} {
		if got := runeWidth(c); got != want {
			t.Errorf("runeWidth(%q) = %d, want %d", c, got, want)
		}
	}
}

// The state file is written even in a quiet run, removed when the run
// finishes, and kept when it stops early.
func TestStateFileQuietFinishAndKeep(t *testing.T) {
	for _, remove := range []bool{true, false} {
		path := filepath.Join(t.TempDir(), ".takeout", "progress.json")
		var out bytes.Buffer
		r := New(&out, "plain", true)
		r.SetStateFile(path)
		r.Phase("confirm duplicates")
		r.Tick(3, 10, "")
		r.Tick(10, 10, "")
		st, ok := ReadState(path)
		if !ok || st.Phase != "confirm duplicates" || st.Done != 10 || st.Total != 10 || st.Pid != os.Getpid() {
			t.Fatalf("state %+v %v", st, ok)
		}
		if out.Len() != 0 {
			t.Fatalf("quiet run printed %q", out.String())
		}
		r.Finish(remove)
		_, err := os.Stat(path)
		if remove != os.IsNotExist(err) {
			t.Fatalf("remove=%v: stat %v", remove, err)
		}
	}
}

func TestReadStateIgnoresPartialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	os.WriteFile(path, []byte(`{"phase":"tags","do`), 0o644)
	if _, ok := ReadState(path); ok {
		t.Fatal("partial file read as state")
	}
	if _, ok := ReadState(filepath.Join(t.TempDir(), "missing.json")); ok {
		t.Fatal("missing file read as state")
	}
}

// The heartbeat keeps updated_at fresh while a long step sends no ticks.
func TestStateHeartbeat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	r := New(&bytes.Buffer{}, "plain", true)
	r.beatPeriod = 50 * time.Millisecond
	r.SetStateFile(path)
	r.Phase("tags")
	first, _ := ReadState(path)
	time.Sleep(300 * time.Millisecond)
	later, _ := ReadState(path)
	r.Finish(true)
	if !later.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("not refreshed: %v then %v", first.UpdatedAt, later.UpdatedAt)
	}
}
