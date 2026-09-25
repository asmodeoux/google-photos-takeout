# Contributing

Thanks for helping people get their photos out of a Takeout in one piece.

## Setup

Install Go and ExifTool (`brew install go exiftool`; on Windows `winget install --id GoLang.Go -e` and `winget install --id OliverBetz.ExifTool -e`). ffmpeg is optional for most tests and required for the corpus.

```sh
go test ./...
scripts/smoke.sh          # scripts\smoke.ps1 on Windows
```

`TAKEOUT_REQUIRE_TOOLS=1` makes tests fail instead of skip when ExifTool or ffmpeg is missing, as CI does.

## Changes

Branch from `main`. Open a pull request and fill in the template.

- Add a table test when you change sidecar matching or date resolution. The test should fail before the fix.
- Add a row to the synthetic Takeout in `internal/testgen/corpus.go` for a new export layout, then run `go test ./internal/pipeline -run Corpus -update` and check the golden diff. A row that reproduces an issue from another project names it, for example `gpth-353-sidecar-truncated-to-51-bytes` for GooglePhotosTakeoutHelper #353.
- Code must work on Windows: build paths with `path/filepath`, move files with `media.Rename` (it never replaces an existing file), and run `GOOS=windows go vet ./...`.
- A bad file is recorded and the run continues. Do not panic, and do not stop the whole library for one file.
- Do not commit anything under `archives/`, `results/`, or `unzipped/`.
- Do not paste personal photos, album names, or GPS coordinates into issues, pull requests, or fixtures.

`gofmt` the Go you touch. `go test ./...` must pass.

## Reporting a bug

Use the bug report form. It asks for `takeout version`, the output of `takeout doctor`, your OS and disk format, and the counts from `results/.takeout/report.json`. Delete any `url` fields before pasting. Do not attach photos or zips.
