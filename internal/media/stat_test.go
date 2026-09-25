package media

import "testing"

func TestStatReportsFreeSpaceAndType(t *testing.T) {
	fs, err := Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if fs.Type == "" || fs.Type == "unknown" {
		t.Fatalf("type %q", fs.Type)
	}
	if fs.Free == 0 {
		t.Fatal("free space is zero")
	}
}
