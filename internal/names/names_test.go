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
