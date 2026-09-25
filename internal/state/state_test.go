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

// A crash mid-write leaves a partial last line; the next record must survive.
func TestJournalRecordAfterTornLineSurvives(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.jsonl")
	if err := os.WriteFile(p, []byte(`{"id":"a","stage":"placed","path":"2019/a.jpg"}`+"\n"+`{"id":"torn","sta`), 0o644); err != nil {
		t.Fatal(err)
	}
	j, err := OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Put(Rec{ID: "b", Stage: "placing", Path: "2019/b.jpg"}); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j, err = OpenJournal(p)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	for _, id := range []string{"a", "b"} {
		if _, ok := j.Get(id); !ok {
			t.Errorf("record %s lost", id)
		}
	}
}
