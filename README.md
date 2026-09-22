# google-photos-takeout

Go CLI that fixes a Google Photos Takeout for Apple Photos. Restores dates, timezones, GPS, and Live Photos from the JSON sidecars, sorts by year, and proves no photo or video was lost.

macOS 13 or newer, on Apple silicon or Intel, is the supported system. Linux runs the same pipeline with full file copies instead of APFS clones, and without file birth times. Windows builds, and is untested.

## Quick start

```sh
brew install exiftool
# optional, only for WebM files Photos cannot import:
# brew install ffmpeg
```

Put the Takeout `.zip` files in `archives/`, then:

```sh
./takeout.sh
```

That checks the zips, writes `results/<year>/`, and prints a short summary. `./takeout.sh check` does the same read and prints the plan without copying anything.

## What you get

```
results/2016/          unique photos and videos, original names, tags inside the file
results/unknown/       no trustworthy date
results/albums/        the same files grouped by Takeout album (APFS clones on Mac)
results/placeholders/  Google stand-in images, kept out of the library
results/.takeout/      resume journal and report.json
```

Apple Photos does not turn folders into albums. Import `results/<year>` and `results/unknown`. Leave `results/albums` out of that import, or every album photo is added twice. To create real Photos albums from the Takeout, use `import-photos` (below).

## Commands

| Command | What it does |
|---|---|
| `takeout check` | Read the zips and print counts. Writes nothing. |
| `takeout run` | Build the library. Safe to run again; it resumes. |
| `takeout status` | One line from the resume journal. |
| `takeout verify` | Check that every finished file is still on disk. |
| `takeout unzip` | Optional extract into `unzipped/`. |
| `takeout import-photos` | Import into a Photos library that is not your iCloud library. |

Useful flags: `--default-tz America/New_York`, `--sample 50`, `--exclude-screenshots`, `--albums clone|copy|none`, `--keep-unzipped`, `--include-trash`, `--quiet`, `--progress plain`.

`--exclude-screenshots` leaves out files whose names start with `Screenshot` or `Screen Shot`, such as `Screenshot_20190606-142331.jpg`. They stay in the zip and are counted in the review as screenshots left out.

Exit codes: `0` reconciled, `2` preflight failed, `3` the ledger does not add up, `4` finished with per-file tag errors, `130` interrupted (run the same command to resume).

## Apple Photos

Dates and places come from tags inside the file, not from the folder name.

- Stills get `DateTimeOriginal` and `OffsetTimeOriginal`, plus GPS when the sidecar has a real location. Takeout uses `0,0` for "no location", and that is not written.
- Videos get `Keys:CreationDate` with a numeric timezone, QuickTime dates in UTC, and `Keys:GPSCoordinates`.
- An Apple Live Photo is a still plus a short video that share a `ContentIdentifier`. The video is named `.MOV` and kept next to the still. Import both files together.
- The timezone is the place the photo was taken when GPS is present. Otherwise the tool uses the camera's own offset, a nearby photo, `--default-tz`, or the most common zone in the library. UTC is the last resort.

`import-photos` can send the library to the Photos library that syncs with iCloud. That upload can be tens of gigabytes, so the command stops and explains the consequence unless you pass `--confirm-icloud`. A different library, created with Option-click in Photos, needs no extra flag.

```sh
./takeout import-photos --library "$HOME/Pictures/Takeout.photoslibrary" --results results
```

The first run asks for Automation permission.

## Why this tool

Other tools each solve part of a Takeout. [GooglePhotosTakeoutHelper](https://github.com/TheLastGimbus/GooglePhotosTakeoutHelper) and [gpth-rs](https://github.com/jl1nie/gpth-rs) match sidecars and organize files. [takeoutfix](https://github.com/vchilikov/takeoutfix) checks the archive before it writes. This program is for people who want the files to open correctly in Apple Photos, including Live Photos, and who want a report that every file in the zip was accounted for. An agent can run it from [AGENTS.md](AGENTS.md).

Sidecar filename rules follow GooglePhotosTakeoutHelper and the [51-character truncation notes](https://gist.github.com/AkuEgor/41f758cdf305d6c97608cd5f06a140fc). Filename date patterns and year-folder names in other languages follow gpth-rs. The code is new; those projects are not vendored.

## Troubleshooting

- **Exit 2, missing part.** A multi-part Takeout is incomplete. Download the missing zip into `archives/` and run again.
- **Exit 2, not enough disk.** The library is about the size of the unique photos, plus album copies when the disk is not APFS.
- **Exit 4.** Some files could not take tags. They are still in the library. The paths are in `results/.takeout/report.json`.
- **Exit 130.** The run stopped. Run the same command. Finished files are not copied again.
- **A video shows the wrong day.** `Keys:CreationDate` needs a timezone. If this happens, file a bug with the report counts, not the video.

Do not attach photos, videos, or GPS coordinates to a bug report, unless absolutely necessary to reproduce. See [SECURITY.md](SECURITY.md).
