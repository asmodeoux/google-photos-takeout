# Changelog

## Unreleased

Windows support, and fixes for export layouts reported against other Takeout tools.

### Added

- Windows: `takeout.cmd` and `takeout.ps1` launchers, NTFS/exFAT-safe names, file creation dates, Ctrl+C that stops ExifTool and ffmpeg, and CI that runs the full synthetic Takeout on Windows, macOS and Linux.
- `takeout doctor` checks ExifTool, the results folder, and non-English paths.
- `--names auto|apple|portable`. `auto` uses the portable rule on NTFS, exFAT, FAT and on Windows.
- `--no-keep-awake`. `run` and `unzip` keep the computer awake by default.
- Prebuilt binaries for Windows, macOS and Linux as CI artifacts.
- Camera RAW (DNG, CR2, NEF, ARW and others) gets dates and GPS. Canon CR3 is placed with its camera date.
- Pixel `.MP` / `.MV` motion videos pair with their `.MP.jpg` as Live Photos.
- Sidecars whose name differs only in case, and `.metadata.json` sidecars, are matched.
- Dates from file names with a day but no time (`IMG-20190101-WA0001.jpg`), at noon.
- Exports whose Google Photos folder is named in an unlisted language are found.
- report.json: `names_rule`, `names_reason`, `album_renames`, `tag_error_files`, `retries`, `seconds`, `files_per_second`, `exiftool_version`, `export_ids`, `system_files_ignored`, `symlinks_skipped`, `cr3_untagged`, `birth_time_errors`.
- Every preflight error prints a `Fix:` line and a README section. `check` ends with the command to run next.

### Fixed

- Video dates no longer depend on the computer's time zone. QuickTime dates were shifted by the local offset, and videos dated from their file name got the computer's zone in `Keys:CreationDate`.
- A date set on an old scan (before 1970, or before 1990) in Google Photos is kept instead of the upload date.
- A Live Photo video with no date of its own takes the still's date and place.
- A Canon CR3 next to a JPEG of the same name is no longer turned into a Live Photo `.MOV`.
- Files are never overwritten or dropped when two names become the same after sanitizing or differ only in case.
- Identical-looking files are confirmed by content before being treated as duplicates.
- Captions with line breaks no longer split ExifTool arguments.
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
