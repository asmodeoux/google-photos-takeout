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
