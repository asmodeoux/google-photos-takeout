package pipeline

import (
	"os"
	"strings"
	"testing"
)

func TestYearName(t *testing.T) {
	if !yearName("2018") || yearName("albums") || yearName("201") {
		t.Fatal("year folder names")
	}
	if tagYear("2018:06:01 12:00:00") != "2018" {
		t.Fatal("tag year")
	}
	if tagYear("<nil>") != "" {
		t.Fatal("nil tag")
	}
}

func TestYearFoldersMatchDates(t *testing.T) {
	root := os.Getenv("TAKEOUT_RESULTS")
	if root == "" {
		t.Skip("set TAKEOUT_RESULTS to check a library")
	}
	bad, err := yearMismatchesForTest(t, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(bad) > 0 {
		n := len(bad)
		if n > 20 {
			n = 20
		}
		t.Fatalf("%d files are in the wrong year folder:\n%s", len(bad), strings.Join(bad[:n], "\n"))
	}
}

func yearMismatchesForTest(t *testing.T, root string) ([]string, error) {
	t.Helper()
	clients, err := startClients("", 1)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClients(clients)
	return YearMismatches(clients, root, nil)
}
