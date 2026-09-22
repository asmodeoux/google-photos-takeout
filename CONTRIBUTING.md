# Contributing

Thanks for helping people get their photos out of a Takeout in one piece.

## Setup

Install Go and ExifTool (`brew install exiftool`). ffmpeg is optional.

```sh
go test ./...
```

## Changes

Branch from `main`. Open a pull request and fill in the template.

- Add a table test when you change sidecar matching or date resolution. The test should fail before the fix.
- A bad file is recorded and the run continues. Do not panic, and do not stop the whole library for one file.
- Do not commit anything under `archives/`, `results/`, or `unzipped/`.
- Do not paste personal photos, album names, or GPS coordinates into issues, pull requests, or fixtures.

`gofmt` the Go you touch. `go test ./...` must pass.

## Reporting a bug

Use the bug report form. It asks for `takeout --version`, your OS, the ExifTool version, and the counts from `results/.takeout/report.json`. Delete any `url` fields before pasting. Do not attach photos or zips.
