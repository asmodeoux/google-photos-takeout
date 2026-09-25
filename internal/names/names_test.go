package names

import "testing"

func TestCollisionKey(t *testing.T) {
	if Key("IMG.JPG") != Key("img.jpg") {
		t.Fatal("case")
	}
	if Key("Café.jpg") != Key("Cafe\u0301.jpg") {
		t.Fatal("nfc")
	}
}

func TestSanitizeLength(t *testing.T) {
	long := stringsRepeat("я", 200) + ".jpg"
	got, changed := Sanitize(long)
	if !changed && len(long) > 255 {
		t.Fatal("expected trim")
	}
	if len(got) > 255 {
		t.Fatalf("len %d", len(got))
	}
	s, ch := Sanitize("a:b.jpg")
	if !ch || s != "a-b.jpg" {
		t.Fatal(s)
	}
	s, _ = Sanitize("trail.jpg.")
	if s != "trail.jpg" {
		t.Fatal(s)
	}
}

func TestScreenshot(t *testing.T) {
	if !IsScreenshot("Screenshot_20190606-142331.jpg") {
		t.Fatal("android")
	}
	if !IsScreenshot("Screen Shot 2016-06-10 at 00.33.07.png") {
		t.Fatal("macos")
	}
	if IsScreenshot("IMG_20190606.jpg") {
		t.Fatal("camera")
	}
}

func TestOutputExt(t *testing.T) {
	if OutputExt("jpeg", "IMG.HEIC", false) != ".jpg" {
		t.Fatal("heic name jpeg bytes")
	}
	if OutputExt("png", "a.jpg", false) != ".png" {
		t.Fatal("jpg name png bytes")
	}
	if OutputExt("mp4", "IMG.MP4", true) != ".MOV" {
		t.Fatal("live")
	}
	if OutputExt("mp4", "v.mp4", false) != ".mp4" {
		t.Fatal("keep mp4")
	}
	// WebM bytes that were not converted must not be named .mov.
	if OutputExt("webm", "clip.webm", false) != ".webm" || OutputExt("webm", "old.MKV", false) != ".mkv" {
		t.Fatal("untranscoded webm/mkv")
	}
	for _, n := range []string{"scan.bmp", "old.avi", "tape.MPG", "clip.wmv", "cam.MTS"} {
		if got := ReplaceExt(n, OutputExt("unknown", n, false)); got != n {
			t.Errorf("ReplaceExt(%q) = %q", n, got)
		}
	}
	if ReplaceExt("18.12.12 - 8", ".png") != "18.12.12 - 8.png" {
		t.Fatal("extensionless")
	}
}

func stringsRepeat(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}

func TestSanitizeRules(t *testing.T) {
	cases := []struct {
		in           string
		apple, porta string
	}{
		{"a?.jpg", "a?.jpg", "a-.jpg"},
		{`Trip: "A|B"?`, `Trip- "A|B"?`, "Trip- -A-B--"},
		{"CON.jpg", "CON.jpg", "CON_.jpg"},
		{"con", "con", "con_"},
		{"LPT9.tar.gz", "LPT9.tar.gz", "LPT9_.tar.gz"},
		{"CONOUT$.png", "CONOUT$.png", "CONOUT$_.png"},
		{"COM10.jpg", "COM10.jpg", "COM10.jpg"},
		{"console.jpg", "console.jpg", "console.jpg"},
		{"trailing dot.", "trailing dot", "trailing dot"},
		{"tab\there.jpg", "tab-here.jpg", "tab-here.jpg"},
		{"Фото 🌅.jpg", "Фото 🌅.jpg", "Фото 🌅.jpg"},
		{"..", "file", "file"},
	}
	for _, c := range cases {
		if got, _ := SanitizeWith(c.in, Apple); got != c.apple {
			t.Errorf("apple %q = %q, want %q", c.in, got, c.apple)
		}
		if got, _ := SanitizeWith(c.in, Portable); got != c.porta {
			t.Errorf("portable %q = %q, want %q", c.in, got, c.porta)
		}
	}
}

func TestSanitizeDirPerSegment(t *testing.T) {
	got, changed := SanitizeDir("Trip?/CON/ok", Portable)
	if got != "Trip-/CON_/ok" || !changed {
		t.Fatalf("%q %v", got, changed)
	}
}

func TestWithIndexStaysUnder255(t *testing.T) {
	name := stringsRepeat("a", 250) + ".jpg" // 254 bytes
	if got, _ := Sanitize(name); got != name {
		t.Fatal("a name under the limit must not change")
	}
	got := WithIndex(name, 2)
	if len(got) > 255 {
		t.Fatalf("len %d", len(got))
	}
	if got[len(got)-8:] != " (2).jpg" {
		t.Fatalf("suffix lost: %q", got[len(got)-10:])
	}
	if WithIndex("x.jpg", 1) != "x.jpg" {
		t.Fatal("n=1")
	}
}

func TestChooseRule(t *testing.T) {
	cases := []struct {
		flag, fs string
		windows  bool
		want     Rule
		err      bool
	}{
		{"auto", "apfs", false, Apple, false},
		{"auto", "ext4", false, Apple, false},
		{"auto", "btrfs", false, Apple, false},
		{"auto", "exfat", false, Portable, false},
		{"auto", "msdos", false, Portable, false},
		{"auto", "vfat", false, Portable, false},
		{"auto", "NTFS", true, Portable, false},
		{"auto", "unknown", true, Portable, false},
		{"", "apfs", false, Apple, false},
		{"portable", "apfs", false, Portable, false},
		{"apple", "apfs", false, Apple, false},
		{"apple", "exFAT", false, Apple, true},
		{"apple", "NTFS", true, Apple, true},
		{"weird", "apfs", false, Apple, true},
	}
	for _, c := range cases {
		got, _, err := ChooseRule(c.flag, c.fs, c.windows)
		if (err != nil) != c.err || (err == nil && got != c.want) {
			t.Errorf("ChooseRule(%q,%q,%v) = %v, %v", c.flag, c.fs, c.windows, got, err)
		}
	}
}
