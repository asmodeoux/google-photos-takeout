package pipeline

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/testgen"
)

// GooglePhotosTakeoutHelper #461, #480: an export in a language whose
// "Google Photos" folder name is not known must still be processed.
func TestUnknownLanguageRootIsFound(t *testing.T) {
	requireTools(t, "exiftool")
	dir := t.TempDir()
	tk := testgen.New()
	side := testgen.Side{Taken: time.Date(2019, 3, 4, 9, 0, 0, 0, time.UTC)}
	tk.PutRaw(1, "Takeout/Fotoj Google/Fotoj de 2019/a.jpg", testgen.JPEG(1))
	tk.PutRaw(1, "Takeout/Fotoj Google/Fotoj de 2019/a.jpg.supplemental-metadata.json", withTitle(side, "a.jpg").JSON())
	tk.PutRaw(1, "Takeout/Fotoj Google/Ferioj/b.jpg", testgen.JPEG(2))
	tk.PutRaw(1, "Takeout/Fotoj Google/Ferioj/b.jpg.supplemental-metadata.json", withTitle(side, "b.jpg").JSON())
	arch := filepath.Join(dir, "archives")
	if _, err := tk.Write(arch); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "results")
	code, rep, err := Run(context.Background(), Options{Archives: arch, Results: out, Albums: "copy", DefaultTZ: "UTC", Quiet: true, Stdout: &bytes.Buffer{}})
	if code != ExitOK {
		t.Fatalf("exit %d: %v %v", code, err, rep.Errors)
	}
	if rep.RootFolder != "Fotoj Google" || rep.Library != 2 {
		t.Fatalf("root %q library %d", rep.RootFolder, rep.Library)
	}
	for _, p := range []string{"2019/a.jpg", "2019/b.jpg", "albums/Ferioj/b.jpg"} {
		if _, err := os.Stat(filepath.Join(out, p)); err != nil {
			t.Errorf("missing %s", p)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "albums", "Photos from 2019")); err == nil {
		t.Error("year folder became an album")
	}
}

func withTitle(s testgen.Side, title string) testgen.Side {
	s.Title = title
	return s
}
