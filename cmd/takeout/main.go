package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/asmodeoux/google-photos-takeout/internal/awake"
	"github.com/asmodeoux/google-photos-takeout/internal/photos"
	"github.com/asmodeoux/google-photos-takeout/internal/pipeline"
	"github.com/asmodeoux/google-photos-takeout/internal/proc"
	"github.com/asmodeoux/google-photos-takeout/internal/version"
)

func main() {
	proc.KillTreeOnExit()
	if len(os.Args) < 2 {
		usage(os.Stdout)
		if startedFromExplorer() {
			fmt.Println("This is a command-line tool. Open PowerShell in this folder and run: .\\takeout.exe doctor")
			fmt.Print("Press Enter to close this window.")
			fmt.Scanln()
		}
		os.Exit(pipeline.ExitPreflight)
	}
	switch os.Args[1] {
	case "help", "-h", "--help":
		usage(os.Stdout)
		return
	case "version", "--version", "-V":
		fmt.Println(version.Version)
		return
	}
	if _, ok := commands[os.Args[1]]; !ok {
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(pipeline.ExitPreflight)
	}
	os.Exit(run(os.Args[1], os.Args[2:], os.Stdout, os.Stderr))
}

// commands maps each command to the flags it accepts.
var commands = map[string][]string{
	"check": {"archives", "results", "default-tz", "albums", "include-trash", "exclude-screenshots",
		"names", "exiftool", "quiet", "progress"},
	"run": {"archives", "results", "default-tz", "albums", "include-trash", "exclude-screenshots",
		"names", "exiftool", "ffmpeg", "sample", "dry-run", "keep-unzipped", "no-keep-awake", "quiet", "progress"},
	"unzip":         {"archives", "no-keep-awake", "quiet", "progress"},
	"status":        {"results"},
	"verify":        {"results", "exiftool"},
	"doctor":        {"results", "exiftool", "ffmpeg"},
	"import-photos": {"results", "library", "confirm-icloud"},
}

type cli struct {
	archives, results, tz, albums, namesRule, exif, ff, progress, library string
	sample                                                                int
	dry, keep, trash, shots, confirm, quiet, noAwake                      bool
}

// flags builds the flag set for one command from the shared table, so
// "takeout <cmd> -h" lists only the flags that command reads.
func flags(cmd string, out io.Writer) (*flag.FlagSet, *cli) {
	c := &cli{}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(out)
	for _, name := range commands[cmd] {
		switch name {
		case "archives":
			fs.StringVar(&c.archives, name, "archives", "folder with the Takeout zip files")
		case "results":
			fs.StringVar(&c.results, name, "results", "output folder")
		case "default-tz":
			fs.StringVar(&c.tz, name, "", "IANA timezone, such as Europe/Berlin, for photos with no GPS and no camera offset")
		case "albums":
			fs.StringVar(&c.albums, name, "clone", "clone, copy, or none")
		case "include-trash":
			fs.BoolVar(&c.trash, name, false, "include Trash and Bin folders")
		case "exclude-screenshots":
			fs.BoolVar(&c.shots, name, false, "leave out files named Screenshot... or Screen Shot...")
		case "names":
			fs.StringVar(&c.namesRule, name, "auto", "auto, apple, or portable: which characters output names may keep")
		case "exiftool":
			fs.StringVar(&c.exif, name, "", "path to exiftool")
		case "ffmpeg":
			fs.StringVar(&c.ff, name, "", "path to ffmpeg")
		case "sample":
			fs.IntVar(&c.sample, name, 0, "process only N files into the results folder")
		case "dry-run":
			fs.BoolVar(&c.dry, name, false, "read the zips and print the plan")
		case "keep-unzipped":
			fs.BoolVar(&c.keep, name, false, "also extract the zips into unzipped/")
		case "no-keep-awake":
			fs.BoolVar(&c.noAwake, name, false, "let the computer sleep during the run")
		case "quiet":
			fs.BoolVar(&c.quiet, name, false, "print only the final summary")
		case "progress":
			fs.StringVar(&c.progress, name, "auto", "auto, tty, or plain")
		case "library":
			fs.StringVar(&c.library, name, "", "Photos library to import into")
		case "confirm-icloud":
			fs.BoolVar(&c.confirm, name, false, "allow import into the Photos library that syncs to iCloud")
		default:
			panic("unknown flag " + name)
		}
	}
	return fs, c
}

func run(cmd string, args []string, stdout, stderr io.Writer) int {
	fs, c := flags(cmd, stderr)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return pipeline.ExitPreflight
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected argument %q. Flags start with --, for example --archives \"%s\"\n", fs.Arg(0), fs.Arg(0))
		return pipeline.ExitPreflight
	}
	launcher := os.Getenv("TAKEOUT_LAUNCHER")
	if launcher == "" {
		launcher = pipeline.DefaultLauncher(runtime.GOOS)
	}
	switch cmd {
	case "status":
		fmt.Fprintln(stdout, pipeline.Status(c.results))
		return 0
	case "doctor":
		return pipeline.Doctor(pipeline.Options{Results: c.results, Exiftool: c.exif, FFmpeg: c.ff, Stdout: stdout})
	case "import-photos":
		return importPhotos(c.library, c.results, c.confirm, stdout, stderr)
	}

	ctx, force, stop := interrupts(stderr, make(chan os.Signal, 3), os.Exit)
	defer stop()
	opt := pipeline.Options{
		Archives: c.archives, Results: c.results, DefaultTZ: c.tz, Sample: c.sample,
		DryRun: c.dry || cmd == "check", KeepUnzipped: c.keep, UnzipOnly: cmd == "unzip",
		Albums: c.albums, IncludeTrash: c.trash, ExcludeScreenshots: c.shots, Quiet: c.quiet, Progress: c.progress,
		Exiftool: c.exif, FFmpeg: c.ff, Names: c.namesRule, Force: force, Stdout: stdout, Launcher: launcher,
	}
	if cmd == "verify" {
		code, _, err := pipeline.Verify(ctx, opt)
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		return code
	}
	if (cmd == "run" || cmd == "unzip") && !opt.DryRun && !c.noAwake {
		release, err := awake.Hold()
		if err != nil {
			fmt.Fprintf(stderr, "note: could not keep the computer awake (%v). Turn off sleep if the run takes hours.\n", err)
		}
		defer release()
	}
	code, _, err := pipeline.Run(ctx, opt)
	if code == pipeline.ExitInterrupt {
		fmt.Fprintln(stderr, "interrupted. Run the same command to resume.")
		return code
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
	}
	if cmd == "check" && code == pipeline.ExitOK {
		fmt.Fprintln(stdout, nextLine(launcher, c))
	}
	return code
}

// nextLine is the run command that follows a successful check, with the
// folders the user passed.
func nextLine(launcher string, c *cli) string {
	var b strings.Builder
	b.WriteString("Next: " + launcher + " run")
	if c.archives != "archives" {
		fmt.Fprintf(&b, ` --archives "%s"`, c.archives)
	}
	if c.results != "results" {
		fmt.Fprintf(&b, ` --results "%s"`, c.results)
	}
	if c.tz != "" {
		b.WriteString(" --default-tz " + c.tz)
	} else {
		b.WriteString(" --default-tz Area/City")
		b.WriteString("\n      Replace Area/City with where most photos were taken, such as America/New_York or Europe/Berlin.")
	}
	return b.String()
}

// forceExitAfter is how long a second Ctrl+C waits for takeout to stop on its
// own before the process exits.
var forceExitAfter = 10 * time.Second

// interrupts cancels ctx on the first Ctrl+C, so files in flight finish, and
// closes force on the second, which stops ExifTool and ffmpeg at once. If the
// run has not ended soon after that, or on a third Ctrl+C, exit is called.
// The journal is written file by file, so the next run resumes either way.
func interrupts(stderr io.Writer, sig chan os.Signal, exit func(int)) (context.Context, <-chan struct{}, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	force := make(chan struct{})
	signal.Notify(sig, stopSignals...)
	go func() {
		n := 0
		for range sig {
			n++
			switch n {
			case 1:
				fmt.Fprintln(stderr, "stopping after the files in flight. Press Ctrl+C again to stop now.")
				cancel()
			case 2:
				fmt.Fprintln(stderr, "stopping now.")
				close(force)
				time.AfterFunc(forceExitAfter, func() {
					fmt.Fprintln(stderr, "interrupted. Run the same command to resume.")
					exit(130)
				})
			default:
				exit(130)
			}
		}
	}()
	return ctx, force, func() { signal.Stop(sig); cancel() }
}

func importPhotos(library, results string, confirm bool, stdout, stderr io.Writer) int {
	abs, err := filepath.Abs(results)
	if err != nil {
		abs = filepath.Clean(results)
	}
	if runtime.GOOS != "darwin" {
		fmt.Fprintf(stderr, "import-photos needs a Mac with Apple Photos. Copy %s to a Mac, then import its year folders and unknown in Photos (File > Import), or run takeout import-photos there.\n", abs)
		return pipeline.ExitPreflight
	}
	if library == "" {
		fmt.Fprintln(stderr, "pass --library. Importing into the system Photos library uploads to iCloud. Add --confirm-icloud if that is the library you mean.")
		return pipeline.ExitPreflight
	}
	if photos.IsSystemLibrary(library) && !confirm {
		fmt.Fprintln(stderr, photos.ICloudWarning(library))
		return pipeline.ExitPreflight
	}
	if photos.IsSystemLibrary(library) {
		fmt.Fprintln(stderr, "Confirmed. Importing into the iCloud Photos library.")
	}
	fmt.Fprintln(stdout, photos.Script(library, nil, nil))
	fmt.Fprintln(stdout, "results:", abs)
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `takeout %s

  takeout doctor                check that this computer can run takeout
  takeout check                 read the zips and print the plan
  takeout run                   build results/<year> and results/albums
  takeout status                one line about the last run
  takeout verify                re-check the results against the report
  takeout unzip                 extract into unzipped/ (optional)
  takeout import-photos         import into a Photos library (macOS)

Run "takeout <command> -h" for its flags.
`, version.Version)
}
