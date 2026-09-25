package pipeline

import (
	"strings"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
)

type scriptedRunner struct {
	replies []exiftool.Reply
	calls   int
}

func (s *scriptedRunner) Run(args []string, id int) (exiftool.Reply, error) {
	r := s.replies[min(s.calls, len(s.replies)-1)]
	s.calls++
	return r, nil
}

var locked = exiftool.Reply{Out: "    0 image files updated\n    1 files weren't updated due to errors\n", Err: "Error renaming temporary file to C:/r/x.jpg\n"}
var ok = exiftool.Reply{Out: "    1 image files updated\n"}

func TestRunWriteRetriesLockedFileThenSucceeds(t *testing.T) {
	r := &scriptedRunner{replies: []exiftool.Reply{locked, ok}}
	var waits []time.Duration
	if err := runWrite(r, []string{"x"}, 1, true, func(d time.Duration) { waits = append(waits, d) }); err != nil {
		t.Fatal(err)
	}
	if r.calls != 2 || len(waits) != 1 {
		t.Fatalf("calls %d waits %v", r.calls, waits)
	}
}

func TestRunWriteGivesUpWithStderr(t *testing.T) {
	r := &scriptedRunner{replies: []exiftool.Reply{locked}}
	err := runWrite(r, []string{"x"}, 1, true, func(time.Duration) {})
	if err == nil || !strings.Contains(err.Error(), "Error renaming temporary file") {
		t.Fatalf("err %v", err)
	}
	if r.calls != 1+len(writeRetryDelays) {
		t.Fatalf("calls %d", r.calls)
	}
}

func TestRunWriteDoesNotRetryOtherErrors(t *testing.T) {
	bad := exiftool.Reply{Out: "    0 image files updated\n", Err: "Error: Not a valid JPG - x.jpg\n"}
	r := &scriptedRunner{replies: []exiftool.Reply{bad, ok}}
	err := runWrite(r, []string{"x"}, 1, true, func(time.Duration) { t.Fatal("slept") })
	if err == nil || !strings.Contains(err.Error(), "Not a valid JPG") || r.calls != 1 {
		t.Fatalf("err %v calls %d", err, r.calls)
	}
}

func TestRunWriteUnchangedIsSuccess(t *testing.T) {
	r := &scriptedRunner{replies: []exiftool.Reply{{Out: "    1 image files unchanged\n"}}}
	if err := runWrite(r, []string{"x"}, 1, true, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
}
