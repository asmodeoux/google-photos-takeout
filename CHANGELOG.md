# Changelog

## Unreleased (1.1.0)

ExifTool that crashes or hangs no longer costs a run, documents in the Google Photos folder stay out of the library, and `takeout status` says where a run is.

### Added

- ExifTool is restarted when it crashes or hangs on a file. A crash is tried once more on a new process; a hang is not, since the file is the likely cause. A file that still fails becomes a tag error. The run stops with exit 2, and a `Fix:` line, only when ExifTool also fails on a small test photo, cannot be started again, or fails on 20 of the last 100 files. Restarts and timeouts are logged in `results/.takeout/exiftool.log`.
- `--exiftool-timeout` for `run` and `verify`: how long ExifTool may take on a small file before it is restarted. Default 2 minutes, longer for large files; at least 5 seconds.
- `takeout status` prints the phase and count of a running run, or `last run stopped during <phase>` after one that stopped. It reads only; the run writes `results/.takeout/progress.json`.
- Progress while confirming duplicates, one tick per file.
- `results/not-importable/` also holds documents saved to Google Photos (`.pdf`, `.txt`, office files, archives and similar). They are never tagged or copied into albums.
- report.json: `not_importable`, `legacy_non_media`, `read_errors`, `read_error_files`, `retries.exiftool_restarts`, `exiftool_log`.

### Fixed

- An edited copy whose name is in NFD (`-modifié` from a Takeout re-zipped on a Mac) finds its original's sidecar instead of going to `unknown/`.
- A file of a type takeout does not recognize keeps its name instead of getting the extension twice (`image.jxl.jxl`).
- `verify` goes through the same restarts, exits 2 with a `Fix:` line when ExifTool keeps crashing, and lists a file ExifTool could not read with the reason.

### Changed: report.json

`schema_version` is 2. `unknown` no longer counts files in `not-importable/`: WebM and MKV videos that could not be converted, and now documents. To get the 1.0 number, add `not_importable` to `unknown`. The identity `unique = library + unknown + not_importable + placeholders + failed + skipped Trash files` holds for every run without `--sample`.

A results folder made by 1.0.0 keeps its layout when resumed: a document 1.0.0 placed in a year folder stays there, is counted in `not_importable`, and `report.txt` and `legacy_non_media` in report.json list it so you can move it out. Only new results folders get the new layout.

## 1.0.0 (2026-09-25)

Windows support, and fixes for export layouts reported against other Takeout tools.

### Added

- Windows: `takeout.cmd` and `takeout.ps1` launchers, NTFS/exFAT-safe names, file creation dates, Ctrl+C that stops ExifTool and ffmpeg, and CI that runs the full synthetic Takeout on Windows, macOS and Linux.
- `takeout doctor` checks ExifTool, the results folder, and non-English paths.
- `--names auto|apple|portable`. `auto` uses the portable rule on NTFS, exFAT, FAT and on Windows.
- `--no-keep-awake`. `run` and `unzip` keep the computer awake by default.
- Prebuilt binaries for Windows (x64, ARM), macOS (Apple silicon, Intel) and Linux on GitHub Releases, with SHA256SUMS. They are not code-signed yet.
- Camera RAW (DNG, CR2, NEF, ARW and others) gets dates and GPS. Canon CR3 is placed with its camera date.
- Pixel `.MP` / `.MV` motion videos pair with their `.MP.jpg` as Live Photos.
- Sidecars whose name differs only in case, and `.metadata.json` sidecars, are matched.
- Dates from file names with a day but no time (`IMG-20190101-WA0001.jpg`), at noon.
- Exports whose Google Photos folder is named in an unlisted language are found.
- report.json: `names_rule`, `names_reason`, `album_renames`, `tag_error_files`, `retries`, `seconds`, `files_per_second`, `exiftool_version`, `export_ids`, `system_files_ignored`, `symlinks_skipped`, `untagged`, `failed`, `failed_files`, `birth_time_errors`.
- Every preflight error prints a `Fix:` line and a README section. `check` ends with the command to run next.

### Fixed

- Video dates no longer depend on the computer's time zone. QuickTime dates were shifted by the local offset, and videos dated from their file name got the computer's zone in `Keys:CreationDate`.
- A date set on an old scan (before 1970, or before 1990) in Google Photos is kept instead of the upload date.
- A Live Photo video with no date of its own takes the still's date and place.
- A Canon CR3 next to a JPEG of the same name is no longer turned into a Live Photo `.MOV`.
- Files are never overwritten or dropped when two names become the same after sanitizing or differ only in case.
- Identical-looking files are confirmed by content before being treated as duplicates.
- Captions with line breaks no longer split ExifTool arguments.
- A damaged zip entry or a file that cannot be placed makes run and verify exit 3 and is listed in failed_files, instead of vanishing behind exit 0.
- A crash at any point resumes without "(2)" duplicates, in the library or in albums. The results lock ends with the process, and a resumed run needs disk space only for what is left.
- AVI, MPEG, WMV, MTS and BMP files are placed with file dates instead of failing every run with tag errors.
- Two albums can no longer end up in one folder, unzip never overwrites a file, and Ctrl+C stops every phase.
- `.3gp` videos keep their extension.
- `._` AppleDouble files, `.DS_Store`, `Thumbs.db` and `__MACOSX` entries are ignored; symbolic links in zips are skipped.

### Changed: album folders when resuming an older results folder

Album folders whose names differ only in case are no longer merged, and names with characters a Windows disk refuses are rewritten. Resuming a results folder made by 0.1.0 can therefore split one album across two folders. For example, a Takeout with albums `Trip` and `trip`:

```
0.1.0:     results/albums/Trip/        t1.jpg, t2.jpg
now:       results/albums/Trip/        t1.jpg
           results/albums/trip (2)/    t2.jpg
```

`album_renames` in `report.json` lists each renamed folder (`trip -> trip (2)`). To avoid a split, run into a new results folder.

## 0.1.0

First release. Reads Google Takeout zips, restores dates, timezones, GPS, and Live Photos, and writes a year library with a reconciliation report.
