package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/dates"
	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

// blockingRunner holds every write until release is closed.
type blockingRunner struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingRunner) Run(args []string, id int) (exiftool.Reply, error) {
	b.started <- struct{}{}
	<-b.release
	return exiftool.Reply{Out: "    1 image files updated\n"}, nil
}

func cancelGroups(t *testing.T, n int) []group {
	dir := t.TempDir()
	var gs []group
	for i := range n {
		p := dir + "/f" + string(rune('a'+i)) + ".jpg"
		write(t, p, "x")
		gs = append(gs, group{
			id: "g" + string(rune('a'+i)), staged: p, trueType: "jpeg",
			when:    dates.When{OK: true, Instant: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
			members: []member{{Entry: zipindex.Entry{Name: "f.jpg"}}},
		})
	}
	return gs
}

func TestTagAllFinishesInFlightWriteAfterCancel(t *testing.T) {
	results := t.TempDir()
	j := openTestJournal(t, results)
	groups := cancelGroups(t, 3)
	for i := range groups {
		groups[i].staged = results + "/.takeout/staging/" + groups[i].id
		write(t, groups[i].staged, "x")
	}
	r := &blockingRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	var rep Report
	result := make(chan bool)
	go func() {
		result <- tagAll(ctx, nil, 5*time.Second, []tagRunner{r}, func() { t.Error("killed") }, groups, results, j, &rep, &timeLog{}, nil)
	}()
	<-r.started
	cancel()
	close(r.release)
	select {
	case stopped := <-result:
		if !stopped {
			t.Fatal("not reported as stopped")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("producer stayed blocked after cancel")
	}
	if rep.TagErrors != 0 {
		t.Fatalf("tag errors %d", rep.TagErrors)
	}
	tagged := 0
	for _, g := range groups {
		if rec, ok := j.Get(g.id); ok && rec.Stage == "tagged" {
			tagged++
		}
	}
	if tagged != 1 {
		t.Fatalf("want exactly the in-flight file journaled, got %d", tagged)
	}
}

func TestTagAllKillsAfterGraceOrForce(t *testing.T) {
	for _, useForce := range []bool{false, true} {
		results := t.TempDir()
		j := openTestJournal(t, results)
		groups := cancelGroups(t, 2)
		for i := range groups {
			groups[i].staged = results + "/.takeout/staging/" + groups[i].id
			write(t, groups[i].staged, "x")
		}
		r := &blockingRunner{started: make(chan struct{}, 1), release: make(chan struct{})}
		ctx, cancel := context.WithCancel(context.Background())
		force := make(chan struct{})
		grace := 200 * time.Millisecond
		if useForce {
			grace = time.Hour
		}
		killed := make(chan struct{})
		result := make(chan bool)
		var rep Report
		go func() {
			result <- tagAll(ctx, force, grace, []tagRunner{r}, func() { close(killed) }, groups, results, j, &rep, &timeLog{}, nil)
		}()
		<-r.started
		cancel()
		if useForce {
			close(force)
		}
		select {
		case <-killed:
		case <-time.After(3 * time.Second):
			t.Fatalf("force=%v: never killed", useForce)
		}
		if !<-result {
			t.Fatal("not stopped")
		}
		close(r.release)
	}
}
