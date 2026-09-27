# Contributing

Thanks for helping people get their photos out of a Takeout in one piece.

## Setup

Install Go and ExifTool (`brew install go exiftool`; on Windows `winget install --id GoLang.Go -e` and `winget install --id OliverBetz.ExifTool -e`). ffmpeg is optional for most tests and required for the corpus.

```sh
go test ./...
scripts/smoke.sh          # scripts\smoke.ps1 on Windows
```

`scripts/test.sh` runs every test, lists the ones that skipped and why, and fails on a skip not named on its command line; CI names the expected skips per OS. `TAKEOUT_REQUIRE_TOOLS=1` makes tests fail instead of skip when ExifTool or ffmpeg is missing, as CI does.

| Variable | What it does | Example |
|---|---|---|
| `TAKEOUT_REQUIRE_TOOLS=1` | Fail instead of skip when ExifTool or ffmpeg is missing. | `TAKEOUT_REQUIRE_TOOLS=1 go test ./...` |
| `TAKEOUT_CHAOS_SEED=N` | Seed for the corpus run where ExifTool crashes at seeded points: `random` tries a new schedule, a number from the test log replays one. CI uses the fixed default. | `TAKEOUT_CHAOS_SEED=42 go test ./internal/pipeline -run CorpusSurvives -v` |
| `TAKEOUT_MEASURE_RSS=1` | Measure ExifTool's memory over 20,000 writes (macOS, Linux). Minutes. | `TAKEOUT_MEASURE_RSS=1 go test ./internal/exiftool -run MemoryGrowth -v -timeout 30m` |
| `TAKEOUT_BENCH_DUPS=1` | Benchmark duplicate confirmation on 460 MB of zips; `TAKEOUT_BENCH_DUPS_DIR` puts them on another disk. | `TAKEOUT_BENCH_DUPS=1 go test ./internal/pipeline -run '^$' -bench ConfirmDuplicates -benchtime 3x` |
| `TAKEOUT_FAKE_EXIFTOOL`, `TAKEOUT_FAKE_STATE` | Set by `fakeexif.Setup`, not by hand: they turn the test binary into an ExifTool that crashes or hangs on purpose. | |

## Changes

Branch from `main`. Open a pull request and fill in the template.

- Add a table test when you change sidecar matching or date resolution. The test should fail before the fix.
- Add a row to the synthetic Takeout in `internal/testgen/corpus.go` for a new export layout, then run `go test ./internal/pipeline -run Corpus -update` and check the golden diff. A row that reproduces an issue from another project names it, for example `gpth-353-sidecar-truncated-to-51-bytes` for GooglePhotosTakeoutHelper #353.
- Code must work on Windows: build paths with `path/filepath`, move files with `media.Rename` (it never replaces an existing file), and run `GOOS=windows go vet ./...`.
- A bad file is recorded and the run continues. Do not panic, and do not stop the whole library for one file.
- Do not commit anything under `archives/`, `results/`, or `unzipped/`.
- Do not paste personal photos, album names, or GPS coordinates into issues, pull requests, or fixtures.

`gofmt` the Go you touch. `go test ./...` must pass.

## Releasing

1. On a branch, set `Version` in `internal/version/version.go` and turn `## Unreleased` in CHANGELOG.md into `## <version> (<date>)`. Open a pull request and merge it when CI is green.
2. Tag the merge commit and push the tag: `git tag v<version> && git push origin v<version>`.
3. `.github/workflows/release.yml` checks that the tag matches the code version and the CHANGELOG, builds every platform, and publishes the GitHub Release with SHA256SUMS.txt and the CHANGELOG section as notes.

## Reporting a bug

Use the bug report form. It asks for `takeout version`, the output of `takeout doctor`, your OS and disk format, and the counts from `results/.takeout/report.json`. Delete any `url` fields before pasting. Do not attach photos or zips.
