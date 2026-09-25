package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/asmodeoux/google-photos-takeout/internal/photos"
	"github.com/asmodeoux/google-photos-takeout/internal/pipeline"
	"github.com/asmodeoux/google-photos-takeout/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(pipeline.ExitPreflight)
	}
	switch os.Args[1] {
	case "help", "-h", "--help":
		usage()
		return
	case "version", "--version", "-V":
		fmt.Println(version.Version)
		return
	case "check", "run", "unzip", "status", "verify", "import-photos":
		os.Exit(run(os.Args[1], os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(pipeline.ExitPreflight)
	}
}

func run(cmd string, args []string) int {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	archives := fs.String("archives", "archives", "directory of Takeout zip files")
	results := fs.String("results", "results", "output directory")
	tz := fs.String("default-tz", "", "IANA timezone used when a photo has no GPS and no camera offset")
	sample := fs.Int("sample", 0, "process only N files into the results directory")
	dry := fs.Bool("dry-run", false, "read the zips and print the plan")
	keep := fs.Bool("keep-unzipped", false, "also extract the zips into unzipped/")
	albums := fs.String("albums", "clone", "clone, copy, or none")
	trash := fs.Bool("include-trash", false, "include Trash and Bin folders")
	shots := fs.Bool("exclude-screenshots", false, "leave out files named Screenshot... or Screen Shot...")
	confirm := fs.Bool("confirm-icloud", false, "allow import into the Photos library that syncs to iCloud")
	quiet := fs.Bool("quiet", false, "print only the final summary")
	progress := fs.String("progress", "auto", "auto, tty, or plain")
	exif := fs.String("exiftool", "", "path to exiftool")
	ff := fs.String("ffmpeg", "", "path to ffmpeg")
	library := fs.String("library", "", "Photos library to import into")
	namesRule := fs.String("names", "auto", "auto, apple, or portable: which characters output names may keep")
	if err := fs.Parse(args); err != nil {
		return pipeline.ExitPreflight
	}
	if cmd == "status" {
		fmt.Println(pipeline.Status(*results))
		return 0
	}
	if cmd == "check" {
		*dry = true
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opt := pipeline.Options{
		Archives: *archives, Results: *results, DefaultTZ: *tz, Sample: *sample,
		DryRun: *dry, KeepUnzipped: *keep, UnzipOnly: cmd == "unzip",
		Albums: *albums, IncludeTrash: *trash, ExcludeScreenshots: *shots, Quiet: *quiet, Progress: *progress,
		Exiftool: *exif, FFmpeg: *ff, Names: *namesRule, Stdout: os.Stdout,
	}
	if cmd == "import-photos" {
		return importPhotos(*library, *results, *confirm)
	}
	if cmd == "verify" {
		opt.DryRun = false
		code, _, err := pipeline.Verify(ctx, opt)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
		return code
	}
	code, _, err := pipeline.Run(ctx, opt)
	if err != nil && code != pipeline.ExitInterrupt {
		fmt.Fprintln(os.Stderr, err)
	}
	if code == pipeline.ExitInterrupt {
		fmt.Fprintln(os.Stderr, "interrupted. Run the same command to resume.")
		return pipeline.ExitInterrupt
	}
	return code
}

func importPhotos(library, results string, confirm bool) int {
	if library == "" {
		fmt.Fprintln(os.Stderr, "pass --library. Importing into the system Photos library uploads to iCloud. Add --confirm-icloud if that is the library you mean.")
		return pipeline.ExitPreflight
	}
	if photos.IsSystemLibrary(library) && !confirm {
		fmt.Fprintln(os.Stderr, photos.ICloudWarning(library))
		return pipeline.ExitPreflight
	}
	if photos.IsSystemLibrary(library) {
		fmt.Fprintln(os.Stderr, "Confirmed. Importing into the iCloud Photos library.")
	}
	fmt.Println(photos.Script(library, nil, nil))
	fmt.Println("results:", filepath.Clean(results))
	return 0
}

func usage() {
	fmt.Printf(`takeout %s

  takeout check                 read the zips and print the plan
  takeout run                   build results/<year> and results/albums
  takeout status                one line from the resume journal
  takeout verify                re-check the report ledger
  takeout unzip                 extract into unzipped/ (optional)
  takeout import-photos         import into a non-iCloud Photos library

Put Takeout zips in archives/ and run ./takeout.sh
`, version.Version)
}
