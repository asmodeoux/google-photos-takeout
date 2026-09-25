// Package pipeline runs a Takeout: index the zips, match sidecars, write
// Apple Photos tags, and file a year library plus album clones.
package pipeline

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/dates"
	"github.com/asmodeoux/google-photos-takeout/internal/exiftool"
	"github.com/asmodeoux/google-photos-takeout/internal/match"
	"github.com/asmodeoux/google-photos-takeout/internal/media"
	"github.com/asmodeoux/google-photos-takeout/internal/names"
	"github.com/asmodeoux/google-photos-takeout/internal/proc"
	"github.com/asmodeoux/google-photos-takeout/internal/progress"
	"github.com/asmodeoux/google-photos-takeout/internal/state"
	"github.com/asmodeoux/google-photos-takeout/internal/zipindex"
)

const (
	ExitOK        = 0
	ExitPreflight = 2
	ExitReconcile = 3
	ExitTagErrors = 4
	ExitInterrupt = 130
)

// Options is a non-interactive run.
type Options struct {
	Archives           string
	Results            string
	DefaultTZ          string
	Sample             int
	DryRun             bool
	KeepUnzipped       bool
	UnzipOnly          bool
	Albums             string // clone, copy, none
	IncludeTrash       bool
	ExcludeScreenshots bool
	Quiet              bool
	Progress           string
	Exiftool           string
	FFmpeg             string
	FailAfter          int
	Names              string // auto, apple, portable
	// Launcher is how the user starts takeout, for commands in fix lines.
	Launcher string
	// Force is closed on a second Ctrl+C: stop ExifTool and ffmpeg at once
	// instead of letting the files in flight finish.
	Force  <-chan struct{}
	Stdout io.Writer
	Now    time.Time
}

// Report is takeout-report.json.
type Report struct {
	SchemaVersion  int            `json:"schema_version"`
	Media          int            `json:"media"`
	Sidecars       int            `json:"sidecars"`
	Library        int            `json:"library"`
	Unknown        int            `json:"unknown"`
	Placeholders   int            `json:"placeholders"`
	LivePairs      int            `json:"live_pairs"`
	IDCopied       int            `json:"identifier_copied"`
	TagErrors      int            `json:"tag_errors"`
	Motions        int            `json:"motion_photos"`
	Untagged       int            `json:"untagged"` // placed with file dates only: CR3, AVI and other formats ExifTool does not write
	Screenshots    int            `json:"screenshots_excluded"`
	Unique         int            `json:"unique"`
	WithGPS        int            `json:"with_gps"`
	Formats        map[string]int `json:"formats"`
	Years          map[string]int `json:"years"`
	Sources        map[string]int `json:"date_sources"`
	TZ             map[string]int `json:"timezone_steps"`
	ExtensionFixes []string       `json:"extension_fixes,omitempty"`
	NearDuplicates []string       `json:"near_duplicates,omitempty"`
	Conflicts      []string       `json:"conflicts,omitempty"`
	PlaceholderURL []string       `json:"placeholder_urls,omitempty"`
	Errors         []string       `json:"errors,omitempty"`
	Filesystem     string         `json:"filesystem"`
	ExiftoolVer    string         `json:"exiftool_version,omitempty"`
	BirthTimeErrs  int            `json:"birth_time_errors,omitempty"`
	BirthTimeList  []string       `json:"birth_time_error_files,omitempty"`
	ExportIDs      []string       `json:"export_ids,omitempty"`
	RootFolder     string         `json:"root_folder,omitempty"`
	SystemFiles    int            `json:"system_files_ignored,omitempty"`
	Symlinks       int            `json:"symlinks_skipped,omitempty"`
	NamesRule      string         `json:"names_rule,omitempty"`
	NamesReason    string         `json:"names_reason,omitempty"`
	AlbumRenames   []string       `json:"album_renames,omitempty"`
	TagErrorFiles  []TagErrorFile `json:"tag_error_files,omitempty"`
	// TagErrorPaths lists every file with a tag error, without the cap, so
	// verify can tell them from files in the wrong year folder.
	TagErrorPaths []string `json:"tag_error_paths,omitempty"`
	// AlbumErrors counts files whose album copy could not be made.
	AlbumErrors int `json:"album_errors,omitempty"`
	// Failed counts media that never reached the library; FailedFiles says why.
	Failed      int          `json:"failed"`
	FailedFiles []FailedFile `json:"failed_files,omitempty"`
	Retries     Retries      `json:"retries"`
	// Seconds is the wall time of each phase. FilesPerSecond is for tags.
	Seconds        map[string]float64 `json:"seconds,omitempty"`
	FilesPerSecond float64            `json:"files_per_second,omitempty"`
	Import         string             `json:"import"`
}

// TagErrorFile is one file whose tags could not be written or read back.
type TagErrorFile struct {
	Path   string `json:"path"`
	Stderr string `json:"stderr"`
}

// FailedFile is one zip entry that could not be read or placed.
type FailedFile struct {
	Entry string `json:"entry"`
	Error string `json:"error"`
}

// maxTagErrorFiles bounds tag_error_files and failed_files in the report.
const maxTagErrorFiles = 40

// Retries counts waits for another process, usually antivirus or the search
// indexer, to let go of a file.
type Retries struct {
	Rename int64 `json:"rename"`
	Tag    int64 `json:"tag"`
}

// tagRetries counts ExifTool writes retried after a file-lock error.
var tagRetries atomic.Int64

// phaseTimer records how long each phase takes.
type phaseTimer struct {
	name  string
	start time.Time
	secs  map[string]float64
}

func (t *phaseTimer) next(name string) {
	now := time.Now()
	if t.secs == nil {
		t.secs = map[string]float64{}
	}
	if t.name != "" {
		t.secs[t.name] += math.Round(now.Sub(t.start).Seconds()*100) / 100
	}
	t.name, t.start = name, now
}

type scInfo struct {
	dates.Sidecar
	Title string
	Name  string
	URL   string
}

type member struct {
	zipindex.Entry
	sc *scInfo
}

type group struct {
	id          string
	members     []member
	canon       int
	sha         string
	staged      string
	when        dates.When
	trueType    string
	placeholder bool
	outRel      string
	live        int
	pairKey     string
	video       bool
	contentID   string
	setID       string
	embAt       time.Time
	haveEmb     bool
	idCopied    bool
	motion      bool
	tagErr      string
	// failErr is set when the file never reached the library: the zip entry
	// could not be read or the file could not be placed.
	failErr string
	// albumsOnly marks a copy made by pending: the file is placed and only its
	// album copies still need space.
	albumsOnly bool
	skip       bool
}

// Run executes check, unzip, or the full organize.
func Run(ctx context.Context, opt Options) (int, Report, error) {
	if opt.Stdout == nil {
		opt.Stdout = os.Stdout
	}
	if opt.Albums == "" {
		opt.Albums = "clone"
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	rep := Report{SchemaVersion: 1, Years: map[string]int{}, Sources: map[string]int{}, TZ: map[string]int{}, Formats: map[string]int{}, Import: importText()}
	if opt.Launcher == "" {
		opt.Launcher = DefaultLauncher(runtime.GOOS)
	}
	pr := progress.New(opt.Stdout, opt.Progress, opt.Quiet)
	if err := checkRoot("archives", opt.Archives); err != nil {
		return ExitPreflight, rep, err
	}
	if err := checkRoot("results", opt.Results); err != nil {
		return ExitPreflight, rep, err
	}

	zips, err := listZips(opt.Archives)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return ExitPreflight, rep, err
	}
	if len(zips) == 0 {
		return ExitPreflight, rep, errNoZips(opt.Archives, opt.Launcher, runtime.GOOS)
	}
	timer := &phaseTimer{}
	renameStart, tagStart := media.RenameRetries.Load(), tagRetries.Load()
	phase := func(name, label string) {
		timer.next(name)
		pr.Phase(label)
	}
	phase("index", fmt.Sprintf("index  %d zip(s)", len(zips)))
	idx, err := zipindex.Open(zips)
	if err != nil {
		return ExitPreflight, rep, err
	}
	if len(idx.ExportIDs) > 1 {
		rep.Errors = append(rep.Errors, "mixed export ids: "+strings.Join(idx.ExportIDs, ", "))
	}
	rep.ExportIDs = idx.ExportIDs
	rep.RootFolder = idx.FallbackRoot
	if len(idx.MissingByExport) > 0 {
		var parts []string
		for _, id := range idx.ExportIDs {
			if miss := idx.MissingByExport[id]; len(miss) > 0 {
				parts = append(parts, fmt.Sprintf("export %s is missing part(s) %v", id, miss))
			}
		}
		return ExitPreflight, rep, errMissingParts(strings.Join(parts, "; "))
	}
	if !opt.UnzipOnly {
		v, err := exiftool.Version(opt.Exiftool)
		if err != nil {
			return ExitPreflight, rep, errExiftool(err)
		}
		if runtime.GOOS == "windows" && !exiftool.AtLeast(v, exiftool.MinWindowsVersion) {
			return ExitPreflight, rep, errExiftoolOld(v, runtime.GOOS)
		}
		rep.ExiftoolVer = v
	}

	if opt.UnzipOnly || opt.KeepUnzipped {
		dest, err := unzipDest(opt.Archives)
		if err != nil {
			return ExitPreflight, rep, err
		}
		if err := checkUnzipDest(dest, idx); err != nil {
			return ExitPreflight, rep, err
		}
		phase("unzip", "unzip")
		if err := unzipAll(ctx, zips, dest); err != nil {
			if ctx.Err() != nil {
				return ExitInterrupt, rep, err
			}
			return ExitPreflight, rep, err
		}
		if opt.UnzipOnly {
			fmt.Fprintln(opt.Stdout, "unzipped")
			return ExitOK, rep, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return ExitInterrupt, rep, err
	}

	readers, err := openZips(zips)
	if err != nil {
		return ExitPreflight, rep, err
	}
	defer closeZips(readers)

	sidecars := map[string]*scInfo{}
	// sidecarsFolded finds a sidecar whose name differs only in case. It is
	// used only when exactly one sidecar has that name.
	sidecarsFolded := map[string][]*scInfo{}
	var items []member
	var skippedJSON []zipindex.Entry
	phase("sidecars", "read sidecars")
	var systemFiles, symlinks []zipindex.Entry
	for _, e := range idx.Entries {
		if e.System {
			systemFiles = append(systemFiles, e)
			continue
		}
		if e.Symlink {
			symlinks = append(symlinks, e)
			continue
		}
		if e.JSON {
			if zipindex.IsSkippedJSON(e.Name) {
				skippedJSON = append(skippedJSON, e)
				continue
			}
			sc, err := readSidecar(readers, e)
			if err != nil {
				rep.Errors = append(rep.Errors, e.Name+": "+err.Error())
				skippedJSON = append(skippedJSON, e)
				continue
			}
			sidecars[match.Key(e.RelFolder, e.Name)] = sc
			fold := match.FoldKey(e.RelFolder, e.Name)
			sidecarsFolded[fold] = append(sidecarsFolded[fold], sc)
			skippedJSON = append(skippedJSON, e)
			rep.Sidecars++
			continue
		}
		items = append(items, member{Entry: e})
	}
	rep.Media = len(items)
	var screenshots []zipindex.Entry
	if opt.ExcludeScreenshots {
		kept := items[:0]
		for _, m := range items {
			if names.IsScreenshot(m.Name) {
				screenshots = append(screenshots, m.Entry)
				continue
			}
			kept = append(kept, m)
		}
		items = kept
	}
	rep.Screenshots = len(screenshots)

	for i := range items {
		m := &items[i]
		if zipindex.Classify(m.RelFolder) == zipindex.ClassTrash && !opt.IncludeTrash {
			continue
		}
		cands := match.Candidates(m.Name)
		for _, c := range cands {
			if sc, ok := sidecars[match.Key(m.RelFolder, c)]; ok {
				m.sc = sc
				if !match.TitleAgrees(m.Name, sc.Title) {
					rep.Errors = append(rep.Errors, "title mismatch "+m.Name+" vs "+sc.Title)
				}
				break
			}
		}
		if m.sc == nil {
			for _, c := range cands {
				if found := sidecarsFolded[match.FoldKey(m.RelFolder, c)]; len(found) == 1 {
					m.sc = found[0]
					break
				}
			}
		}
	}

	groups := groupBy(items)
	phase("duplicates", "confirm duplicates")
	groups, err = confirmDuplicates(ctx, readers, groups)
	if err != nil {
		return ExitInterrupt, rep, err
	}
	rep.Unique = len(groups)
	noteNear(&rep, groups)
	for i := range groups {
		pickCanon(&groups[i], &rep)
	}

	if opt.Sample > 0 && opt.Sample < len(groups) {
		groups = sampleGroups(groups, opt.Sample)
	}

	phase("plan", fmt.Sprintf("plan  %d unique  %d media  %d sidecars", len(groups), rep.Media, rep.Sidecars))
	if opt.DryRun {
		summarizeDry(opt, idx, groups, &rep)
		fmt.Fprintln(opt.Stdout, summaryText(rep))
		return ExitOK, rep, nil
	}

	if err := os.MkdirAll(opt.Results, 0o755); err != nil {
		return ExitPreflight, rep, err
	}
	fs, err := media.Stat(opt.Results)
	if err != nil {
		return ExitPreflight, rep, err
	}
	release, err := state.Lock(filepath.Join(opt.Results, ".takeout"))
	if errors.Is(err, state.ErrNoLocking) {
		fmt.Fprintf(opt.Stdout, "note: %v. Do not start a second takeout on %s while this one runs.\n", err, opt.Results)
		err = nil
	}
	if err != nil {
		if errors.Is(err, state.ErrLocked) {
			err = errLocked(err)
		}
		return ExitPreflight, rep, err
	}
	defer release()
	journal, err := state.OpenJournal(filepath.Join(opt.Results, ".takeout", "state.jsonl"))
	if err != nil {
		return ExitPreflight, rep, err
	}
	defer journal.Close()
	// A report from an earlier run must not answer verify for this one.
	_ = os.Remove(filepath.Join(opt.Results, ".takeout", "report.json"))
	rep.Filesystem = fs.Type
	// On resume, files already in results take no more space.
	need := estimate(pending(groups, journal, opt.Results), fs.APFS, opt.Albums)
	if fs.Free > 0 && fs.Free < need {
		return ExitPreflight, rep, errDiskSpace(need/1e9, fs.Free/1e9, opt.Results, fs.Type, runtime.GOOS)
	}
	if n := tooBigForFAT(fs.Type, groups); n > 0 {
		return ExitPreflight, rep, errFAT(n, opt.Results, fs.Type)
	}
	rule, reason, err := names.ChooseRule(opt.Names, fs.Type, runtime.GOOS == "windows")
	if err != nil {
		return ExitPreflight, rep, errNames(err)
	}
	rep.NamesRule, rep.NamesReason = rule.String(), reason
	if !opt.Quiet {
		fmt.Fprintf(opt.Stdout, "names: %s (%s)\n", rule, reason)
	}
	if !fs.APFS && opt.Albums == "clone" {
		opt.Albums = "copy"
		extra := estimate(groups, false, "copy") - estimate(groups, false, "none")
		fmt.Fprintf(opt.Stdout, "note: %s is not APFS, so album folders are full copies (about %.1f GB more). --albums none skips them.\n", fs.Type, float64(extra)/1e9)
	}

	cleanPartial(filepath.Join(opt.Results, ".takeout", "staging"))

	phase("copy", "copy")
	placed := 0
	for i := range groups {
		if err := ctx.Err(); err != nil {
			return ExitInterrupt, rep, nil
		}
		g := &groups[i]
		if g.placeholder {
			if err := extractGroup(readers, g, opt.Results, journal); err != nil {
				rep.Errors = append(rep.Errors, err.Error())
				g.failErr = err.Error()
			}
			continue
		}
		if zipindex.Classify(g.members[g.canon].RelFolder) == zipindex.ClassTrash && !opt.IncludeTrash {
			g.skip = true
			continue
		}
		if err := extractGroup(readers, g, opt.Results, journal); err != nil {
			rep.Errors = append(rep.Errors, err.Error())
			g.failErr = err.Error()
			continue
		}
		placed++
		pr.Tick(placed, len(groups), g.members[g.canon].Name)
		if opt.FailAfter > 0 && placed >= opt.FailAfter {
			_ = journal.Put(state.Rec{ID: g.id, Stage: "staged", SHA: g.sha})
			return ExitInterrupt, rep, fmt.Errorf("stopped after %d files; run the same command to resume", placed)
		}
	}
	for i := range groups {
		if groups[i].trueType == "webm" && groups[i].staged != "" && groups[i].outRel == "" {
			if err := transcodeOrKeep(ctx, opt, &groups[i]); err != nil {
				if ctx.Err() != nil {
					return ExitInterrupt, rep, nil
				}
				// The original stays, unconverted, in not-importable/.
				rep.Errors = append(rep.Errors, groups[i].members[groups[i].canon].Name+": "+err.Error())
				groups[i].outRel = "not-importable"
			}
		}
	}

	const tagWorkers = 4
	clients, err := startClients(opt.Exiftool, tagWorkers)
	if err != nil {
		return ExitPreflight, rep, err
	}
	defer closeClients(clients)

	readEmbedded(clients, opt, groups, &rep)
	if ctx.Err() != nil {
		return ExitInterrupt, rep, nil
	}
	resolveDates(groups, opt)
	pairLive(groups, &rep)
	applyZones(groups, opt.DefaultTZ)

	times := &timeLog{}
	toTag := 0
	for i := range groups {
		g := &groups[i]
		if rec, ok := journal.Get(g.id); needsTags(g) && !(ok && rec.Stage == "tagged") {
			toTag++
		}
	}
	phase("tags", "tags")
	runners := make([]tagRunner, len(clients))
	for i, c := range clients {
		runners[i] = c
	}
	stopped := tagAll(ctx, opt.Force, tagGrace, runners, func() {
		for _, c := range clients {
			c.Kill()
		}
	}, groups, opt.Results, journal, &rep, times, pr)
	if stopped {
		return ExitInterrupt, rep, nil
	}

	phase("place", "place")
	pl := newPlacer(opt.Results, journal)
	pl.times = times
	for i := range groups {
		if ctx.Err() != nil {
			return ExitInterrupt, rep, nil
		}
		g := &groups[i]
		if g.skip || g.staged == "" || g.failErr != "" {
			continue
		}
		if err := place(opt, g, pl, rule, journal); err != nil {
			g.failErr = err.Error()
			rep.Errors = append(rep.Errors, err.Error())
		}
	}
	phase("albums", "albums")
	if opt.Albums != "none" {
		dirs, renames := albumDirs(groups, rule)
		rep.AlbumRenames = renames
		for i := range groups {
			if ctx.Err() != nil {
				return ExitInterrupt, rep, nil
			}
			if err := albums(opt, groups, i, dirs, pl, rule, journal); err != nil {
				rep.AlbumErrors++
				rep.Errors = append(rep.Errors, err.Error())
			}
		}
	}

	rep.BirthTimeErrs, rep.BirthTimeList = times.count, times.paths
	rep.SystemFiles, rep.Symlinks = len(systemFiles), len(symlinks)
	fates := ledger(idx.Entries, groups, skippedJSON, screenshots, opt.IncludeTrash)
	other := map[string]string{}
	for _, e := range systemFiles {
		other[e.ZipPath+"\x00"+e.EntryName] = "system-file"
	}
	for _, e := range symlinks {
		other[e.ZipPath+"\x00"+e.EntryName] = "symlink-skipped"
	}
	for i := range fates {
		if f, ok := other[fates[i].Zip+"\x00"+fates[i].Entry]; ok {
			fates[i].Fate = f
		}
	}
	if opt.Sample == 0 && !state.Balanced(fates, len(idx.Entries)) {
		rep.Errors = append(rep.Errors, fmt.Sprintf("ledger %d != zip entries %d", len(fates), len(idx.Entries)))
		fillReport(&rep, groups)
		writeReport(opt.Results, rep)
		fmt.Fprintln(opt.Stdout, summaryText(rep))
		return ExitReconcile, rep, fmt.Errorf("reconciliation failed: %d fates, %d entries", len(fates), len(idx.Entries))
	}
	fillReport(&rep, groups)
	if ctx.Err() != nil {
		return ExitInterrupt, rep, nil
	}
	timer.next("verify")
	verifyTags(clients, opt.Results, groups, &rep)
	timer.next("")
	rep.Seconds = timer.secs
	if s := rep.Seconds["tags"]; s > 0 {
		rep.FilesPerSecond = math.Round(float64(toTag)/s*10) / 10
	}
	for _, g := range groups {
		if g.tagErr != "" && g.outRel != "" {
			rep.TagErrorPaths = append(rep.TagErrorPaths, filepath.ToSlash(g.outRel))
		}
	}
	rep.TagErrorFiles = append(tagErrorFiles(groups), rep.TagErrorFiles...)
	if len(rep.TagErrorFiles) > maxTagErrorFiles {
		rep.TagErrorFiles = rep.TagErrorFiles[:maxTagErrorFiles]
	}
	rep.Retries = Retries{Rename: media.RenameRetries.Load() - renameStart, Tag: tagRetries.Load() - tagStart}
	if runtime.GOOS == "windows" && rep.Retries.Rename+rep.Retries.Tag > 20 {
		fmt.Fprintln(opt.Stdout, defenderHint(opt.Results))
	}
	writeReport(opt.Results, rep)
	fmt.Fprintln(opt.Stdout, summaryText(rep))
	if rep.Failed > 0 {
		return ExitReconcile, rep, errFailed(rep.Failed, opt.Results)
	}
	if rep.AlbumErrors > 0 {
		return ExitReconcile, rep, errAlbums(rep.AlbumErrors)
	}
	if rep.TagErrors > 0 {
		return ExitTagErrors, rep, nil
	}
	return ExitOK, rep, nil
}

// errAlbums says that some album copies are missing.
func errAlbums(n int) error {
	return fmt.Errorf("%d album copies could not be made; errors in report.json says why (often a full disk). The library itself is complete. Run the same command to try them again", n)
}

// listZips returns the .zip files in dir, in name order. It reads the folder
// instead of globbing, so a folder named "Takeout [2024]" works, and it
// accepts .ZIP.
func listZips(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".zip") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

// errFailed says that some media never reached the library.
func errFailed(n int, results string) error {
	return fmt.Errorf("%d file(s) from the zips are not in the library. failed_files in %s lists each one and why; a damaged zip part must be downloaded again. Run the same command after fixing it",
		n, filepath.Join(results, ".takeout", "report.json"))
}

func whens(gs []group) []dates.When {
	out := make([]dates.When, len(gs))
	for i := range gs {
		out[i] = gs[i].when
	}
	return out
}

func groupBy(media []member) []group {
	m := map[string]*group{}
	var order []string
	for _, mem := range media {
		id := fmt.Sprintf("%d:%08x", mem.Size, mem.CRC32)
		g, ok := m[id]
		if !ok {
			g = &group{id: id, live: -1}
			m[id] = g
			order = append(order, id)
		}
		g.members = append(g.members, mem)
	}
	out := make([]group, 0, len(order))
	for _, id := range order {
		out = append(out, *m[id])
	}
	return out
}

// confirmDuplicates splits groups whose members share size and CRC32 but not
// bytes. CRC32 is only a quick filter: two different photos can share it, and
// merging them would lose one. Subgroup ids come from content, not zip order,
// so they are the same on every run: the subgroup with the smallest hash keeps
// the group id, the others add a short hash. An unreadable member gets its own
// group, and extraction reports the error.
func confirmDuplicates(ctx context.Context, readers map[string]*zipSet, groups []group) ([]group, error) {
	out := make([]group, 0, len(groups))
	for _, g := range groups {
		if len(g.members) < 2 {
			out = append(out, g)
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		by := map[string]*group{}
		var order []string
		for _, m := range g.members {
			key, err := hashMember(readers, m.Entry)
			if err != nil {
				sum := sha256.Sum256([]byte("unreadable:" + m.ZipPath + "\x00" + m.EntryName))
				key = "~" + hex.EncodeToString(sum[:])
			}
			sg, ok := by[key]
			if !ok {
				sg = &group{live: -1}
				by[key] = sg
				order = append(order, key)
			}
			sg.members = append(sg.members, m)
		}
		sort.Strings(order)
		for i, key := range order {
			sg := by[key]
			sg.id = g.id
			if i > 0 {
				sg.id = g.id + ":" + shortKey(strings.TrimPrefix(key, "~"))
			}
			out = append(out, *sg)
		}
	}
	return out, nil
}

func shortKey(k string) string {
	if len(k) > 12 {
		return k[:12]
	}
	return k
}

func hashMember(readers map[string]*zipSet, e zipindex.Entry) (string, error) {
	f, err := zipFile(readers, e)
	if err != nil {
		return "", err
	}
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func pickCanon(g *group, rep *Report) {
	best := 0
	bestScore := 9
	for i, m := range g.members {
		score := 3
		switch zipindex.Classify(m.RelFolder) {
		case zipindex.ClassLibrary:
			if strings.Contains(strings.ToLower(m.RelFolder), "photo") || strings.HasPrefix(m.RelFolder, "Фото") || strings.Contains(m.RelFolder, "Foto") {
				score = 0
			} else {
				score = 1
			}
		case zipindex.ClassTrash:
			score = 4
		}
		if score < bestScore {
			bestScore = score
			best = i
		}
	}
	g.canon = best
	var times []time.Time
	for _, m := range g.members {
		if m.sc != nil && m.sc.Taken != nil {
			times = append(times, *m.sc.Taken)
		}
	}
	if len(g.members) >= 3 && dates.DistinctTimes(times, time.Minute) >= 3 && g.members[0].Size <= 16*1024 {
		g.placeholder = true
		rep.Placeholders++
		for _, m := range g.members {
			if m.sc != nil && m.sc.URL != "" {
				rep.PlaceholderURL = append(rep.PlaceholderURL, m.Name+" "+m.sc.URL)
			}
		}
	}
	chosen := g.members[g.canon]
	var chosenT time.Time
	if chosen.sc != nil && chosen.sc.Taken != nil {
		chosenT = *chosen.sc.Taken
	}
	for _, m := range g.members {
		if m.sc != nil && m.sc.Taken != nil && !chosenT.IsZero() && !dates.SameInstant(*m.sc.Taken, chosenT, 60*time.Second) {
			rep.Conflicts = append(rep.Conflicts, m.Name)
			break
		}
	}
}

func noteNear(rep *Report, groups []group) {
	type hit struct{ id, folder string }
	by := map[string][]hit{}
	for _, g := range groups {
		for _, m := range g.members {
			k := names.Key(m.Name)
			by[k] = append(by[k], hit{g.id, m.RelFolder})
		}
	}
	for name, hits := range by {
		ids := map[string]bool{}
		for _, h := range hits {
			ids[h.id] = true
		}
		if len(ids) > 1 {
			rep.NearDuplicates = append(rep.NearDuplicates, name)
		}
	}
	sort.Strings(rep.NearDuplicates)
}

func sampleGroups(gs []group, n int) []group {
	if n >= len(gs) {
		return gs
	}
	step := len(gs) / n
	if step < 1 {
		step = 1
	}
	var out []group
	for i := 0; i < len(gs) && len(out) < n; i += step {
		out = append(out, gs[i])
	}
	return out
}

type zipSet struct {
	r  *zip.ReadCloser
	by map[string]*zip.File
}

func openZips(paths []string) (map[string]*zipSet, error) {
	m := map[string]*zipSet{}
	for _, p := range paths {
		r, err := zip.OpenReader(p)
		if err != nil {
			closeZips(m)
			return nil, fmt.Errorf("open %s: %w. Re-download this zip if it is truncated", p, err)
		}
		by := make(map[string]*zip.File, len(r.File))
		for _, f := range r.File {
			by[f.Name] = f
		}
		m[p] = &zipSet{r: r, by: by}
	}
	return m, nil
}

func closeZips(m map[string]*zipSet) {
	for _, r := range m {
		r.r.Close()
	}
}

func zipFile(readers map[string]*zipSet, e zipindex.Entry) (*zip.File, error) {
	r := readers[e.ZipPath]
	if r == nil {
		return nil, fmt.Errorf("zip not open: %s", e.ZipPath)
	}
	f := r.by[e.EntryName]
	if f == nil {
		return nil, fmt.Errorf("missing %s in %s", e.EntryName, e.ZipPath)
	}
	return f, nil
}

func readSidecar(readers map[string]*zipSet, e zipindex.Entry) (*scInfo, error) {
	f, err := zipFile(readers, e)
	if err != nil {
		return nil, err
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxSidecar+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w. Re-download the zip if the CRC check failed", e.Name, err)
	}
	if len(b) > maxSidecar {
		return nil, fmt.Errorf("%s: sidecar is larger than %d MB, not a Google Photos sidecar", e.Name, maxSidecar>>20)
	}
	return parseSidecar(e.Name, b)
}

// maxSidecar bounds how much of one JSON sidecar is read. Real ones are about
// a kilobyte; a damaged zip must not make takeout read gigabytes into memory.
const maxSidecar = 16 << 20

func parseSidecar(name string, b []byte) (*scInfo, error) {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	sc := &scInfo{Name: name}
	if t, ok := raw["title"].(string); ok {
		sc.Title = t
	}
	if d, ok := raw["description"].(string); ok {
		sc.Description = d
	}
	if u, ok := raw["url"].(string); ok {
		sc.URL = u
	}
	sc.Taken = jsonTime(raw, "photoTakenTime")
	sc.Creation = jsonTime(raw, "creationTime")
	if lat, lon, alt, ok := jsonGeo(raw, "geoData"); ok {
		sc.Lat, sc.Lon, sc.Alt, sc.HasGeo, sc.HasAlt = lat, lon, alt, true, alt != 0
	} else if lat, lon, alt, ok := jsonGeo(raw, "geoDataExif"); ok {
		sc.Lat, sc.Lon, sc.Alt, sc.HasGeo, sc.HasAlt = lat, lon, alt, true, alt != 0
	}
	sc.Sidecar = dates.Sidecar{
		Title: sc.Title, Description: sc.Description,
		Taken: sc.Taken, Creation: sc.Creation,
		Lat: sc.Lat, Lon: sc.Lon, Alt: sc.Alt, HasGeo: sc.HasGeo, HasAlt: sc.HasAlt, URL: sc.URL,
	}
	return sc, nil
}

func jsonTime(raw map[string]any, key string) *time.Time {
	obj, _ := raw[key].(map[string]any)
	if obj == nil {
		return nil
	}
	var sec int64
	switch v := obj["timestamp"].(type) {
	case string:
		sec, _ = strconv.ParseInt(v, 10, 64)
	case float64:
		sec = int64(v)
	default:
		return nil
	}
	// 0 is Google's "no date". Negative is before 1970, such as a dated scan.
	if sec == 0 {
		return nil
	}
	t := time.Unix(sec, 0).UTC()
	return &t
}

func jsonGeo(raw map[string]any, key string) (lat, lon, alt float64, ok bool) {
	obj, _ := raw[key].(map[string]any)
	if obj == nil {
		return 0, 0, 0, false
	}
	lat, _ = asFloat(obj["latitude"])
	lon, _ = asFloat(obj["longitude"])
	alt, _ = asFloat(obj["altitude"])
	if lat == 0 && lon == 0 {
		return 0, 0, 0, false
	}
	return lat, lon, alt, true
}

func asFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func extractGroup(readers map[string]*zipSet, g *group, results string, journal *state.Journal) error {
	if rec, ok := journal.Get(g.id); ok && rec.SHA != "" && rec.Stage != "" && rec.Stage != "staged" {
		p := filepath.Join(results, ".takeout", "staging", rec.SHA)
		outRel := ""
		if rec.Path != "" && (rec.Stage != "placing" || movedBeforeCrash(results, rec)) {
			p, outRel = filepath.Join(results, filepath.FromSlash(rec.Path)), filepath.FromSlash(rec.Path)
		}
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			g.staged = p
			g.sha = rec.SHA
			g.outRel = outRel
			hf, err := os.Open(p)
			if err == nil {
				buf := make([]byte, 32)
				n, _ := hf.Read(buf)
				hf.Close()
				g.trueType = kindOf(buf[:n], g.members[g.canon].Name)
			}
			if g.trueType == "" || g.trueType == "unknown" {
				g.trueType = kindFromExt(g.members[g.canon].Name)
			}
			g.motion = isMotionPhoto(g.trueType, p, g.members[g.canon].Name)
			return nil
		}
	}
	m := g.members[g.canon]
	f, err := zipFile(readers, m.Entry)
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	staging := filepath.Join(results, ".takeout", "staging")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	partial := filepath.Join(staging, g.id+".partial")
	partial = strings.ReplaceAll(partial, ":", "_")
	out, err := os.Create(partial)
	if err != nil {
		return err
	}
	h := sha256.New()
	head := make([]byte, 0, 32)
	buf := make([]byte, 1024*1024)
	first := true
	for {
		n, rerr := rc.Read(buf)
		if n > 0 {
			if first {
				take := n
				if take > 32 {
					take = 32
				}
				head = append(head, buf[:take]...)
				first = false
			}
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				return err
			}
			_, _ = h.Write(buf[:n])
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return fmt.Errorf("%s: %w. Re-download the zip if this is a CRC error", m.Name, rerr)
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	// CRC is checked by zip.Reader when the file is fully read. A short read would have errored.
	g.trueType = kindOf(head, m.Name)
	g.sha = hex.EncodeToString(h.Sum(nil))
	final := filepath.Join(staging, g.sha)
	// The staged name is the content hash, so an existing file there is the same bytes.
	if err := media.Rename(partial, final); errors.Is(err, fs.ErrExist) {
		_ = os.Remove(partial)
	} else if err != nil {
		return err
	}
	g.staged = final
	g.motion = isMotionPhoto(g.trueType, final, m.Name)
	return journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "staged"})
}

// isMotionPhoto reports a Google Motion Photo: a JPEG with a video inside,
// marked "MotionPhoto" in its XMP near the start of the file.
func isMotionPhoto(kind, p, name string) bool {
	if kind != "jpeg" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(name), ".mp") {
		return true
	}
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	b := make([]byte, 256*1024)
	n, _ := io.ReadFull(f, b)
	return bytesContains(b[:n], []byte("MotionPhoto"))
}

func bytesContains(b, sub []byte) bool {
	return len(sub) == 0 || (len(b) >= len(sub) && func() bool { return strings.Contains(string(b), string(sub)) }())
}

// canTag reports the kinds ExifTool can write dates and GPS into. Others, such
// as CR3, AVI, MPEG, BMP and WMV, are placed with their file dates only.
// WebM is converted to MOV first, when ffmpeg is there.
func canTag(kind string) bool {
	switch kind {
	case "jpeg", "png", "gif", "webp", "heic", "tiff", "raw", "mov", "mp4":
		return true
	}
	return false
}

// needsTags is the one rule for which groups go to ExifTool for tags.
func needsTags(g *group) bool {
	return !g.skip && !g.placeholder && g.failErr == "" && g.staged != "" && canTag(g.trueType)
}

// kindOf sniffs a file's type from its first bytes. A TIFF whose name has a
// camera RAW extension is raw.
func kindOf(head []byte, name string) string {
	k := zipindex.Sniff(head)
	if k == "tiff" && kindFromExt(name) == "raw" {
		return "raw"
	}
	return k
}

func kindFromExt(p string) string {
	ext := strings.ToLower(filepath.Ext(p))
	// Pixel motion videos: PXL_x.MP, PXL_x.MV, and copies named PXL_x.MP~2.
	if ext == ".mp" || ext == ".mv" || strings.HasPrefix(ext, ".mp~") {
		return "mp4"
	}
	switch ext {
	case ".dng", ".cr2", ".nef", ".nrw", ".arw", ".srw", ".pef", ".orf", ".rw2", ".raf":
		return "raw"
	case ".cr3":
		return "cr3"
	case ".avi":
		return "avi"
	case ".mpg", ".mpeg", ".vob":
		return "mpg"
	case ".wmv", ".asf":
		return "wmv"
	case ".mts", ".m2ts":
		return "mts"
	case ".bmp":
		return "bmp"
	case ".tif", ".tiff":
		return "tiff"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".png":
		return "png"
	case ".gif":
		return "gif"
	case ".webp":
		return "webp"
	case ".heic":
		return "heic"
	case ".mov":
		return "mov"
	case ".mp4", ".m4v", ".3gp", ".3g2":
		return "mp4"
	case ".webm", ".mkv":
		return "webm"
	default:
		return "unknown"
	}
}

func cleanPartial(dir string) {
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".partial") || strings.HasSuffix(e.Name(), "_exiftool_tmp") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func readEmbedded(clients []*exiftool.Client, opt Options, groups []group, rep *Report) {
	// A file ExifTool cannot read is treated as having no embedded tags.
	var paths []string
	index := map[string]int{}
	for i := range groups {
		if groups[i].staged == "" {
			continue
		}
		paths = append(paths, groups[i].staged)
		index[exiftool.PathKey(groups[i].staged)] = i
	}
	byKey, errs := exiftool.ReadAll(clients, paths, []string{
		"DateTimeOriginal", "OffsetTimeOriginal", "CreationDate", "CreateDate",
		"GPSLatitude", "GPSLongitude", "ContentIdentifier"}, true)
	for _, err := range errs {
		rep.Errors = append(rep.Errors, "read embedded tags: "+err.Error())
	}
	for key, row := range byKey {
		i, ok := index[key]
		if !ok {
			continue
		}
		g := &groups[i]
		if s, _ := row["ContentIdentifier"].(string); s != "" {
			g.contentID = s
		}
		emb := dates.Embedded{}
		if s, _ := row["DateTimeOriginal"].(string); s != "" {
			if t, ok := parseExifTime(s); ok {
				emb.DTO = &t
				emb.HasDTO = true
				g.haveEmb = true
				g.embAt = t
				if d, ok := parseOffset(fmt.Sprint(row["OffsetTimeOriginal"])); ok {
					wall := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
					g.embAt = wall.Add(-d)
				}
			}
		}
		if s, _ := row["OffsetTimeOriginal"].(string); s != "" {
			if d, ok := parseOffset(s); ok {
				emb.Offset = &d
			}
		}
		// Videos store the capture time in CreationDate, not DateTimeOriginal.
		// Without this, every later run rewrites the video and Finder shows today's date.
		if !g.haveEmb {
			if s, _ := row["CreationDate"].(string); s != "" {
				if inst, ok := parseCreationInstant(s); ok {
					g.haveEmb = true
					g.embAt = inst
					emb.HasDTO = true
					wall := inst
					emb.DTO = &wall
				}
			}
		}
		if lat, ok := asFloat(row["GPSLatitude"]); ok {
			if lon, ok2 := asFloat(row["GPSLongitude"]); ok2 && !(lat == 0 && lon == 0) {
				emb.HasGPS = true
				emb.Lat, emb.Lon = lat, lon
			}
		}
		g.when = dates.FromSidecar(side(g), emb, opt.Now)
		if !g.when.OK {
			if w := dates.FromEmbedded(emb, opt.Now); w.OK {
				g.when = w
			}
		}
	}
}

func side(g *group) dates.Sidecar {
	m := g.members[g.canon]
	if m.sc != nil {
		return m.sc.Sidecar
	}
	for _, mem := range g.members {
		if mem.sc != nil {
			return mem.sc.Sidecar
		}
	}
	return dates.Sidecar{}
}

func parseExifTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 19 {
		t, err := time.Parse("2006:01:02 15:04:05", s[:19])
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseCreationInstant(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 25 {
		if t, err := time.Parse("2006:01:02 15:04:05Z07:00", s[:25]); err == nil {
			return t.UTC(), true
		}
	}
	if t, ok := parseExifTime(s); ok {
		return t.UTC(), true
	}
	return time.Time{}, false
}

func parseOffset(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 6 || (s[0] != '+' && s[0] != '-') {
		return 0, false
	}
	h, err1 := strconv.Atoi(s[1:3])
	m, err2 := strconv.Atoi(s[4:6])
	if err1 != nil || err2 != nil {
		return 0, false
	}
	d := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute
	if s[0] == '-' {
		d = -d
	}
	return d, true
}

func pairLive(groups []group, rep *Report) {
	byID := map[string][]int{}
	byStem := map[string][]int{}
	for i := range groups {
		g := &groups[i]
		if g.placeholder || g.skip {
			continue
		}
		if g.staged == "" && g.trueType == "" {
			continue
		}
		if g.contentID != "" {
			byID[g.contentID] = append(byID[g.contentID], i)
		}
		folder := g.members[g.canon].RelFolder
		stem := match.LiveStem(g.members[g.canon].Name)
		byStem[folder+"\x00"+names.Key(stem)] = append(byStem[folder+"\x00"+names.Key(stem)], i)
	}
	link := func(a, b int) {
		if groups[a].live >= 0 || groups[b].live >= 0 {
			return
		}
		ia, ib := imageSide(groups[a].trueType), imageSide(groups[b].trueType)
		va, vb := videoSide(groups[a].trueType), videoSide(groups[b].trueType)
		if ia && vb {
			groups[a].live, groups[b].live = b, a
			groups[b].video = true
		} else if ib && va {
			groups[b].live, groups[a].live = a, b
			groups[a].video = true
		} else {
			return
		}
		pk := groups[a].id
		if groups[b].id < pk {
			pk = groups[b].id
		}
		groups[a].pairKey, groups[b].pairKey = pk, pk
	}
	for _, idxs := range byID {
		if len(idxs) == 2 {
			link(idxs[0], idxs[1])
		}
	}
	for _, idxs := range byStem {
		var imgs, vids []int
		for _, i := range idxs {
			if imageSide(groups[i].trueType) {
				imgs = append(imgs, i)
			}
			if videoSide(groups[i].trueType) {
				vids = append(vids, i)
			}
		}
		if len(vids) != 1 || len(imgs) == 0 {
			continue
		}
		best := imgs[0]
		for _, i := range imgs {
			if groups[i].trueType == "heic" {
				best = i
			}
		}
		link(best, vids[0])
	}
	// A camera's RAW+JPEG pair keeps one name, so Apple Photos imports it as
	// one photo with a RAW original.
	for _, idxs := range byStem {
		var raws, stills []int
		for _, i := range idxs {
			switch groups[i].trueType {
			case "raw", "cr3":
				raws = append(raws, i)
			case "jpeg", "heic":
				if groups[i].live < 0 {
					stills = append(stills, i)
				}
			}
		}
		if len(raws) == 1 && len(stills) == 1 {
			a, b := raws[0], stills[0]
			pk := "raw:" + min(groups[a].id, groups[b].id)
			groups[a].pairKey, groups[b].pairKey = pk, pk
		}
	}
	for i := range groups {
		g := &groups[i]
		if g.live < 0 {
			continue
		}
		rep.LivePairs++
		other := &groups[g.live]
		if g.video {
			continue // count pairs once, from the still
		}
		// Both halves are one moment. A half dated only by its file name takes
		// the other half's sidecar or embedded date, with its GPS.
		if weakDate(g.when) && !weakDate(other.when) {
			g.when = other.when
			g.when.Source = dates.SrcLive
		}
		if other.video && weakDate(other.when) && !weakDate(g.when) {
			other.when = g.when
			other.when.Source = dates.SrcLive
		}
		switch {
		case g.contentID == "" && other.contentID != "":
			g.setID = other.contentID
			g.idCopied = true
			rep.IDCopied++
		case other.contentID == "" && g.contentID != "":
			other.setID = g.contentID
			other.idCopied = true
			rep.IDCopied++
		case g.contentID == "" && other.contentID == "":
			id := newID()
			g.setID, other.setID = id, id
			g.idCopied, other.idCopied = true, true
			rep.IDCopied++
		}
	}
	rep.LivePairs /= 2
}

// applyZones picks an offset for every date that lacks one. Stills dated from
// a file name stay without one; videos always get one.
func applyZones(groups []group, defaultTZ string) {
	ws := whens(groups)
	dates.ApplyFallback(ws, defaultTZ)
	for i := range groups {
		groups[i].when = ws[i]
		if videoSide(groups[i].trueType) || groups[i].video {
			dates.PinWallClock(&groups[i].when, defaultTZ)
		}
	}
}

// weakDate is a date that is missing or only read from a file name.
func weakDate(w dates.When) bool {
	return !w.OK || w.Source == dates.SrcFilename || w.Source == dates.SrcFilenameDate
}

func imageSide(t string) bool {
	switch t {
	case "jpeg", "png", "gif", "webp", "heic":
		return true
	}
	return false
}
func videoSide(t string) bool {
	return t == "mp4" || t == "mov" || t == "webm"
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	s := hex.EncodeToString(b[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func resolveDates(groups []group, opt Options) {
	for i := range groups {
		g := &groups[i]
		if g.skip || g.placeholder {
			continue
		}
		if !g.when.OK {
			if w := dates.FromSidecar(side(g), dates.Embedded{}, opt.Now); w.OK {
				g.when = w
			}
		}
		if !g.when.OK {
			g.when = dates.FromFilename(g.members[g.canon].Name, opt.Now)
		}
	}
}

func writeTags(c tagRunner, results string, g *group, id int, times *timeLog) error {
	if !exiftool.Within(results, g.staged) && !strings.Contains(g.staged, ".takeout") {
		return fmt.Errorf("refusing to write outside results: %s", g.staged)
	}
	var existing *time.Time
	if g.haveEmb {
		existing = &g.embAt
	}
	writeDates := exiftool.ShouldWriteDates(existing, g.when)
	plan := exiftool.Plan{
		Path: g.staged, Kind: g.trueType, When: g.when,
		WriteDates:   writeDates,
		WriteGPS:     g.when.HasGPS && writeDates,
		Description:  g.when.Description,
		SetContentID: g.setID,
	}
	if !plan.WriteDates && !plan.WriteGPS && plan.SetContentID == "" && plan.Description == "" {
		return nil
	}
	args, err := exiftool.Args(plan)
	if err != nil {
		return err
	}
	// A forced stop can leave ExifTool's temporary copy next to a file already
	// in the library; ExifTool then refuses to write that file again.
	_ = os.Remove(g.staged + "_exiftool_tmp")
	needUpdate := plan.WriteDates || plan.WriteGPS || plan.SetContentID != ""
	if err := runWrite(c, args, id+1, needUpdate, time.Sleep); err != nil {
		return err
	}
	// ExifTool replaces the file, which clears the macOS creation date.
	if g.when.OK {
		times.set(g.staged, g.when.Local())
	}
	return nil
}

// timeLog sets file times and remembers the files where that failed. A missing
// creation date is not a tag error, but the report says where it happened.
type timeLog struct {
	mu    sync.Mutex
	count int
	paths []string
}

func (l *timeLog) set(p string, t time.Time) {
	if err := media.SetTimes(p, t); err != nil && l != nil {
		l.mu.Lock()
		l.count++
		if len(l.paths) < 10 {
			l.paths = append(l.paths, filepath.Base(p)+": "+err.Error())
		}
		l.mu.Unlock()
	}
}

// tagGrace is how long an interrupted run waits for files in flight before
// stopping ExifTool.
var tagGrace = 30 * time.Second

// tagAll writes tags with one worker per runner. On cancel it stops feeding
// work, lets writes in flight finish for up to grace, then calls kill. A write
// that succeeds after cancel is journaled; one that fails is not counted, so
// the next run redoes it. It reports whether the run was stopped.
func tagAll(ctx context.Context, force <-chan struct{}, grace time.Duration, runners []tagRunner, kill func(),
	groups []group, results string, journal *state.Journal, rep *Report, times *timeLog, pr *progress.Reporter) bool {
	jobs := make(chan int)
	var wg sync.WaitGroup
	var repMu sync.Mutex
	var tagged int
	for _, c := range runners {
		wg.Add(1)
		go func(c tagRunner) {
			defer wg.Done()
			for i := range jobs {
				g := &groups[i]
				err := writeTags(c, results, g, i, times)
				switch {
				case err != nil && ctx.Err() != nil:
					// Interrupted mid-write: leave it for the next run.
				case err != nil:
					g.tagErr = err.Error()
					repMu.Lock()
					rep.TagErrors++
					if len(rep.Errors) < 40 {
						rep.Errors = append(rep.Errors, g.members[g.canon].Name+": "+err.Error())
					}
					repMu.Unlock()
				default:
					// A file already on its way into the library keeps its path.
					if rec, ok := journal.Get(g.id); !ok || rec.Stage == "" || rec.Stage == "staged" || rec.Stage == "tagged" {
						_ = journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "tagged"})
					}
				}
				repMu.Lock()
				tagged++
				n := tagged
				repMu.Unlock()
				if pr != nil {
					pr.Tick(n, len(groups), g.members[g.canon].Name)
				}
			}
		}(c)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
feed:
	for i := range groups {
		g := &groups[i]
		if !needsTags(g) {
			continue
		}
		if rec, ok := journal.Get(g.id); ok && rec.Stage == "tagged" {
			continue
		}
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	select {
	case <-done:
		return ctx.Err() != nil
	case <-ctx.Done():
	}
	// Stopping: let writes in flight finish, unless they take longer than the
	// grace period or a second Ctrl+C says to stop now. Either way, wait for
	// the workers, so none writes to the journal after Run has closed it.
	select {
	case <-done:
	case <-time.After(grace):
		kill()
		<-done
	case <-force:
		kill()
		<-done
	}
	return true
}

// tagRunner is the part of an ExifTool client that writeTags needs.
type tagRunner interface {
	Run(args []string, id int) (exiftool.Reply, error)
}

// writeRetryDelays are the waits before each retry of a write that failed
// because another process held the file (Windows Defender, the search indexer).
var writeRetryDelays = []time.Duration{200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}

// runWrite sends one write and retries it only for transient file-lock errors.
// Other failures are reported at once, with ExifTool's own error text.
func runWrite(c tagRunner, args []string, id int, needUpdate bool, sleep func(time.Duration)) error {
	for attempt := 0; ; attempt++ {
		r, err := c.Run(args, id)
		if err != nil {
			return err
		}
		if !needUpdate || exiftool.Updated(r.Out) {
			return nil
		}
		if attempt < len(writeRetryDelays) && transientWriteError(r.Err) {
			tagRetries.Add(1)
			sleep(writeRetryDelays[attempt])
			continue
		}
		msg := strings.TrimSpace(r.Err)
		if msg == "" {
			msg = strings.TrimSpace(r.Out)
		}
		if len(msg) > 500 {
			msg = msg[:500]
		}
		return fmt.Errorf("exiftool did not update: %s", msg)
	}
}

// transientWriteError reports ExifTool errors caused by another process
// briefly holding the file or its temporary copy.
func transientWriteError(stderr string) bool {
	s := strings.ToLower(stderr)
	for _, k := range []string{"error renaming", "temporary file", "sharing violation", "being used by another process"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// placer picks output names. A name is free only when no earlier file in this
// run or in the journal took it and nothing exists at that path, so a file is
// never overwritten, on resume or after two names sanitize to the same string.
type placer struct {
	results string
	taken   map[string]bool // names.Key of results-relative slash paths
	next    map[string]int  // last index used per folder + stem
	pairN   map[string]int  // index chosen for a Live Photo pair
	times   *timeLog
}

func newPlacer(results string, journal *state.Journal) *placer {
	p := &placer{results: results, taken: map[string]bool{}, next: map[string]int{}, pairN: map[string]int{}}
	// Only names that exist are reserved: a journal path whose move or album
	// link never happened, because of a crash, is free to use again.
	exists := func(rel string) bool {
		_, err := os.Lstat(filepath.Join(results, filepath.FromSlash(rel)))
		return err == nil
	}
	for _, rec := range journal.All() {
		if rec.Path != "" && exists(rec.Path) {
			p.taken[names.Key(rec.Path)] = true
		}
		for _, a := range rec.Albums {
			if exists(a) {
				p.taken[names.Key(a)] = true
			}
		}
	}
	return p
}

func (p *placer) free(rel string) bool {
	if p.taken[names.Key(filepath.ToSlash(rel))] {
		return false
	}
	_, err := os.Lstat(filepath.Join(p.results, rel))
	return errors.Is(err, fs.ErrNotExist)
}

// pick returns the first free "base", "base (2)", ... in dir. Both halves of a
// Live Photo pair get the same number when they can.
func (p *placer) pick(dir, base, pairKey string) string {
	if n, ok := p.pairN[pairKey]; ok && pairKey != "" {
		if rel := filepath.Join(dir, names.WithIndex(base, n)); p.free(rel) {
			return rel
		}
	}
	stemKey := names.Key(filepath.ToSlash(dir) + "/" + names.Stem(base))
	for n := p.next[stemKey] + 1; ; n++ {
		rel := filepath.Join(dir, names.WithIndex(base, n))
		if p.free(rel) {
			p.next[stemKey] = n
			if pairKey != "" {
				if _, ok := p.pairN[pairKey]; !ok {
					p.pairN[pairKey] = n
				}
			}
			return rel
		}
	}
}

func (p *placer) take(rel string) {
	p.taken[names.Key(filepath.ToSlash(rel))] = true
}

// moveInto places src at a free name picked by pick, retrying when another file
// appears at the chosen path. Moves fall back to a copy across volumes.
// intent, when set, is called with the chosen name before the move, so a crash
// between the move and the caller's journal write can be recovered.
func (p *placer) moveInto(src, dir, base, pairKey string, intent func(rel string) error) (string, error) {
	for {
		rel := p.pick(dir, base, pairKey)
		dest := filepath.Join(p.results, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if intent != nil {
			if err := intent(rel); err != nil {
				return "", err
			}
		}
		err := media.Rename(src, dest)
		if errors.Is(err, fs.ErrExist) {
			p.take(rel)
			continue
		}
		if err != nil {
			if err := media.Copy(src, dest); err != nil {
				if errors.Is(err, fs.ErrExist) {
					p.take(rel)
					continue
				}
				return "", err
			}
			_ = os.Remove(src)
		}
		p.take(rel)
		return rel, nil
	}
}

func place(opt Options, g *group, pl *placer, rule names.Rule, journal *state.Journal) error {
	prev, hadPrev := journal.Get(g.id)
	if hadPrev && prev.Path != "" {
		rel := filepath.FromSlash(prev.Path)
		done := prev.Stage == "placed" || prev.Stage == "cloned"
		// A "placing" record counts only when extractGroup found that the
		// move happened (its staged file is gone and the file is there).
		adopted := prev.Stage == "placing" && g.outRel == rel
		if done || adopted {
			if _, err := os.Stat(filepath.Join(opt.Results, rel)); err == nil {
				g.outRel = rel
				if adopted {
					if g.when.OK {
						pl.times.set(filepath.Join(opt.Results, rel), g.when.Local())
					}
					_ = journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "placed", Path: prev.Path, Year: filepath.Dir(rel), Name: filepath.Base(rel)})
				}
				return nil
			}
		}
	}
	orig := g.members[g.canon].Name
	ext := names.OutputExt(g.trueType, orig, g.video)
	base, _ := names.SanitizeWith(names.ReplaceExt(orig, ext), rule)
	var dir string
	switch {
	case g.placeholder:
		dir = "placeholders"
	case g.trueType == "webm" && strings.HasPrefix(g.outRel, "not-importable"):
		dir = "not-importable"
	case !g.when.OK:
		dir = "unknown"
	default:
		dir = strconv.Itoa(g.when.Year)
	}
	if g.trueType == "webm" && g.outRel == "" && !ffmpegOK(opt.FFmpeg) {
		dir = "not-importable"
	}
	rel, err := pl.moveInto(g.staged, dir, base, g.pairKey, func(rel string) error {
		return journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "placing", Path: filepath.ToSlash(rel)})
	})
	if err != nil {
		// The move did not happen: withdraw the intent, so no later run
		// takes whatever file ends up at that name for this one.
		if hadPrev {
			_ = journal.Put(prev)
		} else {
			_ = journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "staged"})
		}
	}
	if err != nil {
		return err
	}
	dest := filepath.Join(opt.Results, rel)
	g.outRel = rel
	g.staged = dest
	if g.when.OK {
		pl.times.set(dest, g.when.Local())
	}
	_ = journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "placed", Path: filepath.ToSlash(rel), Year: dir, Name: filepath.Base(rel)})
	return nil
}

// movedBeforeCrash reports whether a "placing" record's move happened: the
// staged copy is gone and a file is at the recorded path. If the staged copy
// is still there, the file at that path, if any, belongs to another group.
func movedBeforeCrash(results string, rec state.Rec) bool {
	placed := filepath.Join(results, filepath.FromSlash(rec.Path))
	pst, err := os.Stat(placed)
	if err != nil {
		return false
	}
	staging := filepath.Join(results, ".takeout", "staging", rec.SHA)
	for _, staged := range []string{staging, staging + ".mov"} {
		sst, err := os.Stat(staged)
		if err != nil {
			continue
		}
		// A crash after a copy but before the staged file was removed leaves
		// two identical files: the move did happen.
		if sst.Size() == pst.Size() && fileCRC(staged) == fileCRC(placed) {
			_ = os.Remove(staged)
			return true
		}
		return false
	}
	return true
}

func recStage(j *state.Journal, id string) string {
	r, ok := j.Get(id)
	if !ok {
		return ""
	}
	return r.Stage
}

// albumDirs maps each Takeout album folder to its folder under results/albums.
// Two albums whose names become equal after sanitizing, or differ only in case,
// get "name", "name (2)", ... in sorted order, so the result is the same on
// every run and every OS.
func albumDirs(groups []group, rule names.Rule) (map[string]string, []string) {
	var folders []string
	seen := map[string]bool{}
	for _, g := range groups {
		for _, m := range g.members {
			if zipindex.Classify(m.RelFolder) == zipindex.ClassAlbum && !seen[m.RelFolder] {
				seen[m.RelFolder] = true
				folders = append(folders, m.RelFolder)
			}
		}
	}
	sort.Strings(folders)
	out := map[string]string{}
	// taken holds every folder name chosen so far, compared the way the disk
	// compares names, so "trip (2)" cannot land on an album already called
	// "Trip (2)".
	taken := map[string]bool{}
	var renames []string
	for _, f := range folders {
		clean, _ := names.SanitizeDir(f, rule)
		dir := clean
		for n := 2; taken[names.Key(dir)]; n++ {
			dir = names.DirWithIndex(clean, n)
		}
		taken[names.Key(dir)] = true
		out[f] = dir
		if dir != f {
			renames = append(renames, f+" -> "+dir)
		}
	}
	return out, renames
}

func albums(opt Options, groups []group, i int, dirs map[string]string, pl *placer, rule names.Rule, journal *state.Journal) error {
	g := &groups[i]
	if g.outRel == "" || g.placeholder || g.failErr != "" || strings.HasPrefix(g.outRel, "not-importable") {
		return nil
	}
	rec, _ := journal.Get(g.id)
	if rec.Stage == "cloned" {
		return nil
	}
	// Album links from an earlier, interrupted run are kept. Each link is
	// journaled before it is made, so a crash never leaves a second copy.
	var made []string
	done := map[string]bool{}
	for _, a := range rec.Albums {
		if _, err := os.Lstat(filepath.Join(opt.Results, filepath.FromSlash(a))); err == nil {
			made = append(made, a)
			done[path.Dir(a)] = true
		}
	}
	seen := map[string]bool{}
	src := filepath.Join(opt.Results, g.outRel)
	for _, m := range g.members {
		if zipindex.Classify(m.RelFolder) != zipindex.ClassAlbum || seen[m.RelFolder] {
			continue
		}
		seen[m.RelFolder] = true
		ext := names.OutputExt(g.trueType, m.Name, g.video)
		base, _ := names.SanitizeWith(names.ReplaceExt(m.Name, ext), rule)
		dir := filepath.Join("albums", filepath.FromSlash(dirs[m.RelFolder]))
		if done[filepath.ToSlash(dir)] {
			continue
		}
		for {
			rel := pl.pick(dir, base, "")
			dest := filepath.Join(opt.Results, rel)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			intent := append(append([]string{}, made...), filepath.ToSlash(rel))
			if err := journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "placed", Path: filepath.ToSlash(g.outRel), Albums: intent}); err != nil {
				return err
			}
			err := linkAlbum(opt.Albums, src, dest)
			pl.take(rel)
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			if err != nil {
				// Withdraw the intent; the next run tries this album again.
				_ = journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "placed", Path: filepath.ToSlash(g.outRel), Albums: made})
				return err
			}
			if g.when.OK {
				pl.times.set(dest, g.when.Local())
			}
			made = append(made, filepath.ToSlash(rel))
			break
		}
	}
	return journal.Put(state.Rec{ID: g.id, SHA: g.sha, Stage: "cloned", Path: filepath.ToSlash(g.outRel), Albums: made})
}

// linkAlbum puts a clone or copy of src at dst. It never replaces dst.
func linkAlbum(mode, src, dst string) error {
	if mode == "copy" {
		return media.Copy(src, dst)
	}
	return media.Clone(src, dst)
}

func transcodeOrKeep(ctx context.Context, opt Options, g *group) error {
	if !ffmpegOK(opt.FFmpeg) {
		return nil
	}
	bin := opt.FFmpeg
	if bin == "" {
		bin = "ffmpeg"
	}
	// The staged name is a short content hash, so paths stay short on Windows.
	dest := strings.TrimSuffix(g.staged, filepath.Ext(g.staged)) + ".mov"
	cmd := exec.CommandContext(ctx, bin, "-nostdin", "-y", "-i", g.staged, "-map", "0:v:0", "-map", "0:a?",
		"-c:v", "libx264", "-crf", "18", "-pix_fmt", "yuv420p", "-c:a", "aac", "-b:a", "160k",
		"-movflags", "+faststart", dest)
	proc.Isolate(cmd)
	cmd.Cancel = func() error { proc.Kill(cmd); return nil }
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(dest)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %v %s", err, out)
	}
	_ = os.Remove(g.staged)
	g.staged = dest
	g.trueType = "mov"
	return nil
}

func ffmpegOK(bin string) bool {
	if bin == "" {
		bin = "ffmpeg"
	}
	_, err := exec.LookPath(bin)
	return err == nil
}

// fatMax is the largest file FAT32 can store.
const fatMax = 4<<30 - 1

func isFAT(fsType string) bool {
	switch strings.ToLower(fsType) {
	case "fat", "fat12", "fat16", "fat32", "vfat", "msdos":
		return true
	}
	return false
}

// tooBigForFAT counts files that FAT32 cannot hold. A WebM is converted to
// MOV, which can grow, so its size is counted one and a half times.
func tooBigForFAT(fsType string, groups []group) int {
	if !isFAT(fsType) {
		return 0
	}
	n := 0
	for _, g := range groups {
		if len(g.members) == 0 {
			continue
		}
		size := g.members[g.canon].Size
		if kindFromExt(g.members[g.canon].Name) == "webm" {
			size += size / 2
		}
		if size > fatMax {
			n++
		}
	}
	return n
}

// checkUnzipDest runs before any extraction: the destination needs room for
// every entry and, on FAT32, no single file of 4 GiB or more.
func checkUnzipDest(dest string, idx *zipindex.Index) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	fs, err := media.Stat(dest)
	if err != nil {
		return err
	}
	var total uint64
	big := 0
	for _, e := range idx.Entries {
		total += e.Size
		if e.Size > fatMax {
			big++
		}
	}
	if isFAT(fs.Type) && big > 0 {
		return errFAT(big, dest, fs.Type)
	}
	if fs.Free > 0 && fs.Free < total {
		return errDiskSpace(total/1e9, fs.Free/1e9, dest, fs.Type, runtime.GOOS)
	}
	return nil
}

// pending leaves out groups that an earlier run finished, including their
// album copies, and keeps album members only for groups whose file is placed
// but whose albums are not done.
func pending(gs []group, journal *state.Journal, results string) []group {
	var out []group
	for _, g := range gs {
		rec, ok := journal.Get(g.id)
		if !ok || rec.Path == "" {
			out = append(out, g)
			continue
		}
		if _, err := os.Stat(filepath.Join(results, filepath.FromSlash(rec.Path))); err != nil {
			out = append(out, g)
			continue
		}
		if rec.Stage == "cloned" {
			continue
		}
		// The file is placed; only album copies may still be made.
		albumsOnly := g
		albumsOnly.albumsOnly = true
		albumsOnly.members = nil
		for _, m := range g.members {
			if zipindex.Classify(m.RelFolder) == zipindex.ClassAlbum {
				albumsOnly.members = append(albumsOnly.members, m)
			}
		}
		if len(albumsOnly.members) > 0 {
			out = append(out, albumsOnly)
		}
	}
	return out
}

func estimate(gs []group, apfs bool, albums string) uint64 {
	var n uint64
	for _, g := range gs {
		if len(g.members) == 0 {
			continue
		}
		if !g.albumsOnly {
			n += g.members[0].Size
		}
		if !apfs && albums != "none" {
			for _, m := range g.members {
				if zipindex.Classify(m.RelFolder) == zipindex.ClassAlbum {
					n += m.Size
				}
			}
		}
	}
	return n + n/10
}

func ledger(entries []zipindex.Entry, groups []group, skipped, screenshots []zipindex.Entry, includeTrash bool) []state.Fate {
	fateOf := map[string]string{}
	for _, g := range groups {
		fate := "library"
		switch {
		case g.failErr != "":
			fate = "error"
		case g.placeholder:
			fate = "placeholder"
		case g.skip:
			fate = "excluded-trash"
		case strings.HasPrefix(g.outRel, "not-importable"):
			fate = "not-importable"
		}
		for _, m := range g.members {
			key := m.ZipPath + "\x00" + m.EntryName
			if fate == "error" {
				fateOf[key] = fate
				continue
			}
			if zipindex.Classify(m.RelFolder) == zipindex.ClassTrash && !includeTrash {
				fateOf[key] = "excluded-trash"
				continue
			}
			if zipindex.Classify(m.RelFolder) == zipindex.ClassAlbum && !g.placeholder {
				fateOf[key] = "album-clone"
				continue
			}
			fateOf[key] = fate
		}
	}
	for _, e := range skipped {
		fateOf[e.ZipPath+"\x00"+e.EntryName] = "skipped-json"
	}
	for _, e := range screenshots {
		fateOf[e.ZipPath+"\x00"+e.EntryName] = "excluded-screenshot"
	}
	var out []state.Fate
	for _, e := range entries {
		f := fateOf[e.ZipPath+"\x00"+e.EntryName]
		if f == "" {
			f = "error"
		}
		out = append(out, state.Fate{Zip: e.ZipPath, Entry: e.EntryName, Fate: f})
	}
	return out
}

func fillReport(rep *Report, groups []group) {
	for _, g := range groups {
		if g.motion {
			rep.Motions++
		}
		if g.failErr != "" {
			rep.Failed++
			if len(rep.FailedFiles) < maxTagErrorFiles {
				m := g.members[g.canon]
				rep.FailedFiles = append(rep.FailedFiles, FailedFile{Entry: m.RelFolder + "/" + m.Name, Error: g.failErr})
			}
			continue
		}
		if g.placeholder || g.skip {
			continue
		}
		kind := g.trueType
		if kind == "" && len(g.members) > 0 {
			kind = kindFromExt(g.members[g.canon].Name)
		}
		if kind == "" {
			kind = "unknown"
		}
		if rep.Formats == nil {
			rep.Formats = map[string]int{}
		}
		rep.Formats[kind]++
		if !canTag(kind) {
			rep.Untagged++
		}
		if g.when.HasGPS {
			rep.WithGPS++
		}
		if !g.when.OK || strings.HasPrefix(g.outRel, "not-importable") {
			rep.Unknown++
			continue
		}
		if strings.HasPrefix(g.outRel, "unknown") {
			rep.Unknown++
			continue
		}
		rep.Library++
		rep.Years[strconv.Itoa(g.when.Year)]++
		rep.Sources[g.when.Source]++
		if g.when.TZStep != "" {
			rep.TZ[g.when.TZStep]++
		}
		orig := g.members[g.canon].Name
		if filepath.Ext(orig) != filepath.Ext(g.outRel) && filepath.Ext(g.outRel) != "" {
			rep.ExtensionFixes = append(rep.ExtensionFixes, orig+" -> "+filepath.Base(g.outRel))
		}
	}
}

func verifyTags(clients []*exiftool.Client, results string, groups []group, rep *Report) {
	type item struct {
		i    int
		path string
		day  string
	}
	var items []item
	for i := range groups {
		g := &groups[i]
		// A file whose write failed is already a tag error; reading it back
		// would count it twice.
		if g.outRel == "" || !g.when.OK || g.placeholder || g.tagErr != "" || g.trueType == "gif" || !canTag(g.trueType) {
			continue
		}
		if g.when.Source == dates.SrcEmbedded {
			continue
		}
		items = append(items, item{i, filepath.Join(results, g.outRel), g.when.Local().Format("2006:01:02")})
	}
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.path
	}
	rows, errs := exiftool.ReadAll(clients, paths, []string{"DateTimeOriginal", "CreationDate"}, false)
	for _, err := range errs {
		rep.Errors = append(rep.Errors, "readback: "+err.Error())
	}
	for _, it := range items {
		row := rows[exiftool.PathKey(it.path)]
		dto, _ := row["DateTimeOriginal"].(string)
		cre, _ := row["CreationDate"].(string)
		if !strings.Contains(dto+" "+cre, it.day) {
			rep.TagErrors++
			if rel, err := filepath.Rel(results, it.path); err == nil {
				rep.TagErrorPaths = append(rep.TagErrorPaths, filepath.ToSlash(rel))
			}
			if len(rep.TagErrorFiles) < maxTagErrorFiles {
				rel, _ := filepath.Rel(results, it.path)
				rep.TagErrorFiles = append(rep.TagErrorFiles, TagErrorFile{Path: filepath.ToSlash(rel),
					Stderr: fmt.Sprintf("date did not read back: want %s, DateTimeOriginal %q, CreationDate %q", it.day, dto, cre)})
			}
			if len(rep.Errors) < 30 {
				rep.Errors = append(rep.Errors, "readback "+filepath.Base(it.path))
			}
		}
	}
}

// startClients starts n stay_open ExifTool processes, or none on error.
func startClients(bin string, n int) ([]*exiftool.Client, error) {
	clients := make([]*exiftool.Client, 0, n)
	for range n {
		c, err := exiftool.Start(bin)
		if err != nil {
			closeClients(clients)
			return nil, err
		}
		clients = append(clients, c)
	}
	return clients, nil
}

func closeClients(clients []*exiftool.Client) {
	for _, c := range clients {
		c.Close()
	}
}

func summarizeDry(opt Options, idx *zipindex.Index, groups []group, rep *Report) {
	resolveDates(groups, opt)
	for i := range groups {
		if groups[i].trueType == "" && len(groups[i].members) > 0 {
			groups[i].trueType = kindFromExt(groups[i].members[groups[i].canon].Name)
		}
	}
	pairLive(groups, rep)
	applyZones(groups, opt.DefaultTZ)
	fillReport(rep, groups)
	fs, _ := media.Stat(opt.Results)
	if fs.Type == "" {
		fs, _ = media.Stat(opt.Archives)
	}
	rep.Filesystem = fs.Type
	fmt.Fprintf(opt.Stdout, "parts %d  missing %v  exports %v\n", len(idx.Zips), idx.MissingByExport, idx.ExportIDs)
	fmt.Fprintf(opt.Stdout, "filesystem %s  free %d GB  need about %d GB\n", fs.Type, fs.Free/1e9, estimate(groups, fs.APFS, opt.Albums)/1e9)
}

// tagErrorFiles lists files whose tag write failed, by their place in results.
func tagErrorFiles(groups []group) []TagErrorFile {
	var out []TagErrorFile
	for _, g := range groups {
		if g.tagErr == "" || len(out) >= maxTagErrorFiles {
			continue
		}
		p := filepath.ToSlash(g.outRel)
		if p == "" {
			p = g.members[g.canon].RelFolder + "/" + g.members[g.canon].Name
		}
		out = append(out, TagErrorFile{Path: p, Stderr: g.tagErr})
	}
	return out
}

func defenderHint(results string) string {
	abs, err := filepath.Abs(results)
	if err != nil {
		abs = results
	}
	return "note: Windows Defender or the search indexer is holding new files. Excluding " + abs +
		" from scanning makes runs faster. See README.md#antivirus"
}

// maxErrors bounds the errors list written to the report.
const maxErrors = 200

func writeReport(results string, rep Report) {
	if results == "" {
		return
	}
	if n := len(rep.Errors); n > maxErrors {
		rep.Errors = append(rep.Errors[:maxErrors:maxErrors], fmt.Sprintf("... and %d more", n-maxErrors))
	}
	dir := filepath.Join(results, ".takeout")
	_ = os.MkdirAll(dir, 0o755)
	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(filepath.Join(dir, "report.json"), b, 0o644)
	_ = os.WriteFile(filepath.Join(dir, "report.txt"), []byte(summaryText(rep)), 0o644)
}

func summaryText(rep Report) string {
	var b strings.Builder
	b.WriteString("\nReview\n")
	fmt.Fprintf(&b, "  media in zips     %d\n", rep.Media)
	fmt.Fprintf(&b, "  sidecars          %d\n", rep.Sidecars)
	fmt.Fprintf(&b, "  unique files      %d\n", rep.Unique)
	fmt.Fprintf(&b, "  dated             %d\n", rep.Library)
	fmt.Fprintf(&b, "  unknown date      %d\n", rep.Unknown)
	fmt.Fprintf(&b, "  with GPS          %d\n", rep.WithGPS)
	without := rep.Library + rep.Unknown - rep.WithGPS
	if without < 0 {
		without = 0
	}
	fmt.Fprintf(&b, "  without GPS       %d\n", without)
	fmt.Fprintf(&b, "  live photo pairs  %d\n", rep.LivePairs)
	fmt.Fprintf(&b, "  placeholders      %d\n", rep.Placeholders)
	fmt.Fprintf(&b, "  screenshots out   %d\n", rep.Screenshots)
	fmt.Fprintf(&b, "  tag errors        %d\n", rep.TagErrors)
	if rep.Filesystem != "" {
		fmt.Fprintf(&b, "  filesystem        %s\n", rep.Filesystem)
	}
	b.WriteString("Years\n")
	for _, y := range sortedKeys(rep.Years) {
		fmt.Fprintf(&b, "  %s  %d\n", y, rep.Years[y])
	}
	b.WriteString("Formats\n")
	for _, k := range sortedKeys(rep.Formats) {
		fmt.Fprintf(&b, "  %s  %d\n", k, rep.Formats[k])
	}
	n := len(rep.Errors)
	if n > 8 {
		n = 8
	}
	if n > 0 {
		b.WriteString("Errors\n")
		for _, e := range rep.Errors[:n] {
			fmt.Fprintf(&b, "  %s\n", e)
		}
	}
	fmt.Fprintf(&b, "\n%s\n", rep.Import)
	return b.String()
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func importText() string {
	return "Import results/<year> and results/unknown into Apple Photos, not results/albums. import-photos --library <Photos library> uploads to iCloud when that library is the system one; pass --confirm-icloud to allow that."
}

// unzipDest is the unzipped/ folder next to the archives folder. A trailing
// separator or "." must not put it inside the archives.
func unzipDest(archives string) (string, error) {
	abs, err := filepath.Abs(archives)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(filepath.Dir(filepath.Clean(abs)), "unzipped")
	if rel, err := filepath.Rel(abs, dest); err == nil && !strings.HasPrefix(rel, "..") {
		return "", &PreflightError{Problem: "the unzip folder would be inside the archives folder", Value: dest,
			Fix: "pass the archives folder by name, for example --archives archives", Anchor: "paths"}
	}
	return dest, nil
}

// unzipAll extracts every zip into dest/<zip name>/. It never replaces a file:
// an existing file of the same size is taken as already extracted (resume),
// and a different one keeps its place while the new file gets " (2)".
func unzipAll(ctx context.Context, zips []string, dest string) error {
	for _, z := range zips {
		if err := ctx.Err(); err != nil {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(z), ".zip")
		root := filepath.Join(dest, base)
		if _, err := os.Stat(filepath.Join(root, ".complete")); err == nil {
			continue
		}
		if err := unzipOne(ctx, z, root); err != nil {
			return err
		}
		_ = os.WriteFile(filepath.Join(root, ".complete"), []byte("ok\n"), 0o644)
	}
	return nil
}

func unzipOne(ctx context.Context, z, root string) error {
	r, err := zip.OpenReader(z)
	if err != nil {
		return err
	}
	defer r.Close()
	taken := map[string]bool{}
	for _, f := range r.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := zipindex.CheckPath(f.Name); err != nil {
			return err
		}
		if f.Mode()&fs.ModeSymlink != 0 || zipindex.IsSystemFile(f.Name) {
			continue
		}
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(filepath.Join(root, unzipRel(f.Name, "", nil)), 0o755)
			continue
		}
		rel := unzipRel(f.Name, "file", taken)
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			return err
		}
		if err := unzipFile(f, root, rel); err != nil {
			return fmt.Errorf("%s: %w. Re-download the zip if this is a CRC error", f.Name, err)
		}
	}
	return nil
}

func unzipFile(f *zip.File, root, rel string) error {
	dir, file := filepath.Split(filepath.FromSlash(rel))
	partial := filepath.Join(root, dir, file+".partial")
	written := false
	defer func() {
		if !written {
			os.Remove(partial)
		}
	}()
	for n := 1; ; n++ {
		target := filepath.Join(root, dir, names.WithIndex(file, n))
		if st, err := os.Stat(target); err == nil {
			if uint64(st.Size()) == f.UncompressedSize64 && fileCRC(target) == f.CRC32 {
				return nil // extracted by an earlier, interrupted run
			}
			continue
		}
		if !written {
			if err := extractTo(f, partial); err != nil {
				return err
			}
			written = true
		}
		err := media.Rename(partial, target)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			written = false
		}
		return err
	}
}

// fileCRC is the CRC32 of a file, or 0 if it cannot be read.
func fileCRC(p string) uint32 {
	f, err := os.Open(p)
	if err != nil {
		return 0
	}
	defer f.Close()
	h := crc32.NewIEEE()
	if _, err := io.Copy(h, f); err != nil {
		return 0
	}
	return h.Sum32()
}

func extractTo(f *zip.File, p string) error {
	out, err := os.Create(p)
	if err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		out.Close()
		return err
	}
	_, err = io.Copy(out, rc)
	rc.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return err
}

// unzipRel turns a zip entry name into a relative path that every filesystem
// accepts. Unzipped files are for viewing, so the portable rule always applies.
// Files whose names become equal in one folder get " (2)", " (3)", ...
func unzipRel(name, kind string, taken map[string]bool) string {
	parts := strings.Split(strings.TrimSuffix(name, "/"), "/")
	for i, p := range parts {
		parts[i], _ = names.SanitizeWith(p, names.Portable)
	}
	rel := strings.Join(parts, "/")
	if kind == "file" && taken != nil {
		dir, file := path.Split(rel)
		for n := 1; ; n++ {
			cand := dir + names.WithIndex(file, n)
			if !taken[names.Key(cand)] {
				rel = cand
				break
			}
		}
		taken[names.Key(rel)] = true
	}
	return filepath.FromSlash(rel)
}

// Verify checks that every journal path still exists and the report has no tag errors.
func Verify(ctx context.Context, opt Options) (int, Report, error) {
	var rep Report
	b, err := os.ReadFile(filepath.Join(opt.Results, ".takeout", "report.json"))
	if err != nil {
		abs, _ := filepath.Abs(opt.Results)
		return ExitPreflight, rep, &PreflightError{
			Problem: "no report in the results folder", Value: abs,
			Fix: "run takeout run first, or pass the folder it wrote with --results", Anchor: "verify",
		}
	}
	if err := json.Unmarshal(b, &rep); err != nil {
		return ExitReconcile, rep, err
	}
	recs, err := state.ReadJournal(filepath.Join(opt.Results, ".takeout", "state.jsonl"))
	if err != nil {
		return ExitPreflight, rep, err
	}
	// Every file a finished record names, in the library and in albums, must
	// be on disk. Records of unfinished moves are not checked.
	var missing []string
	for _, rec := range recs {
		if rec.Stage != "placed" && rec.Stage != "cloned" {
			continue
		}
		paths := []string{rec.Path}
		if rec.Stage == "cloned" {
			// Album copies are finished only once the record says cloned.
			paths = append(paths, rec.Albums...)
		}
		for _, p := range paths {
			if p == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(opt.Results, filepath.FromSlash(p))); err != nil {
				missing = append(missing, p)
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return ExitReconcile, rep, fmt.Errorf("%d files from the journal are missing, for example: %s", len(missing), strings.Join(missing[:min(len(missing), 8)], "; "))
	}
	if rep.Failed > 0 {
		return ExitReconcile, rep, errFailed(rep.Failed, opt.Results)
	}
	if rep.AlbumErrors > 0 {
		return ExitReconcile, rep, errAlbums(rep.AlbumErrors)
	}
	clients, err := startClients(opt.Exiftool, 4)
	if err != nil {
		return ExitPreflight, rep, err
	}
	defer closeClients(clients)
	// Files with tag errors are known to lack their date; they are exit 4, not
	// a wrong year.
	known := map[string]bool{}
	for _, p := range rep.TagErrorPaths {
		known[p] = true
	}
	for _, f := range rep.TagErrorFiles {
		known[filepath.ToSlash(f.Path)] = true
	}
	bad, err := YearMismatches(clients, opt.Results, known)
	if err != nil {
		return ExitReconcile, rep, err
	}
	if len(bad) > 0 {
		n := len(bad)
		if n > 8 {
			n = 8
		}
		return ExitReconcile, rep, fmt.Errorf("%d files are in the wrong year folder, for example: %s", len(bad), strings.Join(bad[:n], "; "))
	}
	_ = ctx
	// Files with tag errors are in the library; the year check above still ran.
	if rep.TagErrors > 0 {
		return ExitTagErrors, rep, nil
	}
	return ExitOK, rep, nil
}

// Status prints one line from the journal.
func Status(results string) string {
	b, err := os.ReadFile(filepath.Join(results, ".takeout", "state.jsonl"))
	if err != nil {
		return "no run yet"
	}
	n := strings.Count(string(b), "\n")
	line := fmt.Sprintf("journal lines %d", n)
	if rb, err := os.ReadFile(filepath.Join(results, ".takeout", "report.json")); err == nil {
		var rep Report
		if json.Unmarshal(rb, &rep) == nil && rep.TagErrors > 0 {
			line += fmt.Sprintf("\n%d tag errors, see %s tag_error_files", rep.TagErrors,
				filepath.Join(results, ".takeout", "report.json"))
		}
	}
	return line
}
