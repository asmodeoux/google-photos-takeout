package match

import "testing"

func has(c []string, want string) bool {
	for _, s := range c {
		if s == want {
			return true
		}
	}
	return false
}

func TestCandidates(t *testing.T) {
	long := "00100sPORTRAIT_00100_BURST20181105180120932_COVER.jpg"
	c := Candidates(long)
	if !has(c, long+".supplemental-metadata.json") {
		t.Fatal("untruncated supplemental")
	}
	if !has(c, fit51(long+".supplemental-metadata")) {
		t.Fatal("truncated form")
	}

	c = Candidates("photo(1).jpg")
	if !has(c, "photo.jpg.supplemental-metadata(1).json") {
		t.Fatalf("bracket swap missing: %v", c)
	}
	if !has(c, "photo.jpg(1).json") {
		t.Fatal("legacy bracket")
	}

	c = Candidates("24.02(1).14 - 1")
	if !has(c, "24.02.14 - 1.supplemental-metadata(1).json") {
		t.Fatalf("middle (N): %v", c)
	}

	c = Candidates("2014-03-05(1).png")
	if !has(c, "2014-03-05.supplemental-metadata(1).json") {
		t.Fatalf("ext drop +(N): %v", c)
	}

	c = Candidates("20.06.13 - 14.jpg")
	if !has(c, "20.06.13 - 14.supplemental-metadata.json") {
		t.Fatal("ext dropped")
	}
	// exact names only: the -9 candidate must not be the -90 name
	c9 := Candidates("20.06.13 - 9.jpg")
	if has(c9, "20.06.13 - 90.supplemental-metadata.json") {
		t.Fatal("prefix steal")
	}

	c = Candidates("2014-04-04-edited.jpg")
	if !has(c, "2014-04-04.supplemental-metadata.json") && !has(c, "2014-04-04.jpg.supplemental-metadata.json") {
		t.Fatalf("edited: %v", c)
	}
}

func TestTitleOmitsNumber(t *testing.T) {
	if !TitleAgrees("IMG_20161116_112442(1).jpg", "IMG_20161116_112442.jpg") {
		t.Fatal("title omits (N)")
	}
	if TitleAgrees("other.jpg", "IMG.jpg") {
		t.Fatal("different title")
	}
}

func TestSameNameDifferentFolder(t *testing.T) {
	if Key("Album", "IMG.jpg") == Key("Photos from 2016", "IMG.jpg") {
		t.Fatal("folders must stay apart")
	}
}

func TestCandidatesMetadataVariant(t *testing.T) {
	c := Candidates("LIVE.MP4")
	if !has(c, "LIVE.MP4.metadata.json") {
		t.Fatalf("metadata.json missing: %v", c)
	}
	if FoldKey("A", "IMG.JPG.json") != FoldKey("A", "img.jpg.JSON") || FoldKey("A", "x") == FoldKey("B", "x") {
		t.Fatal("fold key")
	}
}

func TestLiveHalvesShareTitle(t *testing.T) {
	for _, c := range [][2]string{
		{"IMG_0012.MOV", "IMG_0012.HEIC"},
		{"PXL_1.MP", "PXL_1.MP.jpg"},
		{"PXL_1.MP~2", "PXL_1.MP.jpg"},
	} {
		if !TitleAgrees(c[0], c[1]) {
			t.Errorf("TitleAgrees(%q, %q) = false", c[0], c[1])
		}
	}
	if TitleAgrees("IMG_0013.MOV", "IMG_0012.HEIC") {
		t.Error("different stems agree")
	}
	for name, want := range map[string]string{
		"PXL_1.MP.jpg": "PXL_1", "PXL_1.MP": "PXL_1", "PXL_1.MP~2": "PXL_1",
		"PXL_1.mv.jpg": "PXL_1", "IMG_1.HEIC": "IMG_1", "clip.mp4": "clip",
	} {
		if got := LiveStem(name); got != want {
			t.Errorf("LiveStem(%q) = %q, want %q", name, got, want)
		}
	}
}
