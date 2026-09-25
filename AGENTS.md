# AGENTS.md

## Operating on a user's Takeout

The user has private photos in `archives/`. Treat that directory as read-only.

Commands below are for macOS and Linux. On Windows, use PowerShell and replace `./takeout.sh` with `.\takeout.cmd` and `./takeout` with `.\takeout.exe`; paths look like `"D:\Takeout"`.

1. Ask where the zips are and which IANA timezone to use as `--default-tz` before a full run. Do not guess a timezone from the machine.
2. Run `./takeout.sh doctor`, then `./takeout.sh check --archives <dir>`. `check` only reads. `doctor` writes one probe file under `results/.takeout/doctor-*` and deletes it; it never touches `archives/`. `doctor` exit 2 prints a `Fix:` line; follow it.
3. Start `./takeout.sh run --archives <dir> --default-tz <zone>` in the background. Poll `./takeout status` every 60 seconds. Do not stream the full log into the chat.
4. Success is exit code 0 and `./takeout verify` exiting 0. Quote `media`, `library`, `unknown`, `live_pairs`, `placeholders`, and `tag_errors` from `results/.takeout/report.json`, plus `names_rule` and any `album_renames`. On Windows also quote `retries`.
5. Exit 2: every message has a `Fix:` line and a `See: README.md#...` section. Fix the problem (missing zip, missing ExifTool, disk space) and run again.
6. Exit 130: run the same command again. It resumes.
7. Exit 4: the library is usable. Report the tag error count and the paths in `tag_error_files`. Do not delete files to "clean up".
8. Never unzip by hand, write a one-off script, delete anything in `archives/`, edit files in `results/` by hand, or `git add` `archives/`, `results/`, or `unzipped/`.
9. `import-photos` is macOS only and experimental: it checks the library and prints an AppleScript, and does not import yet. It needs `--library`. If that path is the system Photos library, stop and tell the user it will upload to iCloud. Run it only after they pass `--confirm-icloud`. While testing on a machine that already has a personal library, import one photo and one video into a new library, not the system library.

The first `go build` downloads modules. Writing to an external disk needs permission outside the workspace. On Windows, `winget install` changes PATH only for new windows; `takeout.cmd` says so when Go or ExifTool is installed but not found. Photos Automation permission is a macOS dialog the user has to approve.

## Contributing

Pipeline order: index the zips, match sidecars in the same folder across every zip, group identical bytes, resolve the date and timezone, write tags with ExifTool, place files, clone albums, reconcile.

Matching rules live in `internal/match` and follow GooglePhotosTakeoutHelper plus the 51-character sidecar truncation. Do not match a sidecar by basename across folders. Date order and the timezone chain live in `internal/dates`. Apple Photos reads `DateTimeOriginal` plus `OffsetTimeOriginal` on stills, and `Keys:CreationDate` with an offset plus `Keys:GPSCoordinates` on videos.

`go test ./...` must pass on macOS, Linux and Windows. Tests use synthetic files only. Personal album names, places, and photos do not belong in tests, docs, or fixtures. The end-to-end corpus is `internal/testgen/corpus.go`; after changing behavior, run `go test ./internal/pipeline -run Corpus -update` and review the golden diff.

Run `gofmt` on Go files you change.
