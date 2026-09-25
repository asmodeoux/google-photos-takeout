package progress

import (
	"bytes"
	"strings"
	"testing"
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
