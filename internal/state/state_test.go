package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestJournalResume(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "state.jsonl")
	j, err := OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Put(Rec{ID: "1:abcd", Stage: "staged", SHA: "abc"}); err != nil {
		t.Fatal(err)
	}
	if err := j.Put(Rec{ID: "1:abcd", Stage: "placed", Path: "2016/a.jpg"}); err != nil {
		t.Fatal(err)
	}
	j.Close()

	j, err = OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	r, ok := j.Get("1:abcd")
	if !ok || r.Stage != "placed" || r.Path != "2016/a.jpg" {
		t.Fatalf("%+v %v", r, ok)
	}
}

func TestLedger(t *testing.T) {
	fates := []Fate{{Fate: "library"}, {Fate: "skipped-json"}}
	if !Balanced(fates, 2) {
		t.Fatal("sum")
	}
	if Balanced(fates, 3) {
		t.Fatal("mismatch")
	}
	_ = os.Remove("")
}
