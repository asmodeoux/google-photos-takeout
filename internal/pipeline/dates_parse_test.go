package pipeline

import "testing"

func TestParseCreationInstant(t *testing.T) {
	got, ok := parseCreationInstant("2013:01:08 18:36:28+03:00")
	if !ok || got.UTC().Format("2006-01-02 15:04:05") != "2013-01-08 15:36:28" {
		t.Fatalf("%v %v", got, ok)
	}
	if _, ok := parseCreationInstant("not a date"); ok {
		t.Fatal("accepted junk")
	}
}

// Google writes altitude 0 when it does not know it; that is not sea level.
func TestSidecarAltitudeZeroIsUnknown(t *testing.T) {
	sc, err := parseSidecar("a.jpg.json", []byte(`{"geoData":{"latitude":48.1,"longitude":11.5,"altitude":0.0}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !sc.HasGeo || sc.HasAlt {
		t.Fatalf("geo %v alt %v", sc.HasGeo, sc.HasAlt)
	}
	sc, _ = parseSidecar("a.jpg.json", []byte(`{"geoData":{"latitude":48.1,"longitude":11.5,"altitude":519.2}}`))
	if !sc.HasAlt || sc.Alt != 519.2 {
		t.Fatalf("alt %v %v", sc.HasAlt, sc.Alt)
	}
}
