# google-photos-takeout

Turn a Google Photos Takeout into a photo library with the right dates, places, Live Photos and albums, and a report that proves every file in the zips was accounted for.

Google's export splits each photo from its date and location: the date lives in a JSON file next to the photo, often under a truncated name, sometimes in another zip. Import the zips as they are and years of photos land on the day you downloaded them. This tool reads the zips directly, finds each photo's JSON, writes the date, time zone and GPS into the file itself, rebuilds Live Photos, and sorts everything into year folders.

Works on **macOS, Windows and Linux**. Importing straight into Apple Photos needs a Mac; the library it builds opens correctly anywhere that reads photo metadata.

- [How it works in eight steps](#tutorial)
- [Install](#2-install): [macOS](#macos) · [Windows](#windows) · [Linux](#linux)
- [Commands](#commands) · [Flags](#flags) · [Exit codes](#exit-codes) · [report.json](#reportjson-fields)
- [Windows notes](#what-is-different-on-windows) · [Troubleshooting](#troubleshooting) · [FAQ](#faq)

## What you get

```
results/2019/          every unique photo and video, tags written inside the file
results/unknown/       files with no trustworthy date
results/albums/        the same files grouped by Takeout album
results/placeholders/  Google's stand-in images for files it could not export
results/.takeout/      report.json, report.txt and the resume journal
```

- **Dates.** Stills get `DateTimeOriginal` with `OffsetTimeOriginal`. Videos get QuickTime dates in UTC plus `Keys:CreationDate` with a time zone, which is what Apple Photos reads.
- **Time zones.** From the photo's GPS position, the camera's own offset, a photo taken nearby in time, or `--default-tz`, in that order.
- **Places.** GPS from the JSON, written for stills and videos. Google's `0,0` "no location" is not written.
- **Live Photos.** The still and its video share a `ContentIdentifier`, keep matching names, and the video becomes `.MOV`. Pixel `.MP` motion videos pair with their `.MP.jpg`.
- **Nothing lost.** Every entry in every zip ends up in the library, in `unknown`, or in the report with the reason. `takeout verify` checks it again later.

## Tutorial

### 1. Export from Google

1. Open [takeout.google.com](https://takeout.google.com), click **Deselect all**, then tick **Google Photos** only.
2. **Next step.** Choose *Export once*, file type **.zip**, and the largest size that fits your disk (larger parts mean fewer files to download).
3. Download **every** part of the export into one folder. Do not unzip them. Parts are named `takeout-20240101T000000Z-1-001.zip`, `-002.zip` and so on.

The folder is called `archives` below. Anything works: `archives/` inside this project, or a folder on an external disk passed with `--archives`.

### 2. Install

You need **ExifTool** (writes the tags) and, to build from source, **Go**. **ffmpeg** is optional: without it, WebM and MKV videos are copied to `results/not-importable/` instead of being converted.

#### macOS

```sh
brew install go exiftool ffmpeg
git clone https://github.com/asmodeoux/google-photos-takeout.git
cd google-photos-takeout
./takeout.sh doctor
```

#### Windows

In PowerShell:

```powershell
winget install --id GoLang.Go -e
winget install --id OliverBetz.ExifTool -e
winget install --id Gyan.FFmpeg -e
```

**Open a new PowerShell window** so it sees the new programs. Then download this project (green **Code** button, **Download ZIP**, then extract it, or `git clone`), and in that folder:

```powershell
.\takeout.cmd doctor
```

`takeout.cmd` builds `takeout.exe` the first time and works even where PowerShell blocks scripts. Every command below works the same way with `.\takeout.cmd` in place of `./takeout.sh`.

<details>
<summary>Prebuilt takeout.exe without installing Go</summary>

Every green CI run on `main` builds `takeout.exe`. On GitHub, open **Actions**, pick the latest green run on `main`, and download **takeout-windows-amd64** (or `-arm64`) under **Artifacts**. Only runs on `main` publish `takeout-*` artifacts; `test-build-*` ones come from pull requests and are not for use. You need to be signed in to GitHub, and artifacts expire after 30 days. There is no signed release yet, so:

- Extract the zip, then run `Unblock-File .\takeout.exe` in that folder.
- If SmartScreen says "Windows protected your PC", choose **More info**, then **Run anyway**. <a id="smartscreen"></a>
- You still need ExifTool, installed as above.

Then use `.\takeout.exe` wherever this guide says `.\takeout.cmd`.
</details>

<details>
<summary>Git Bash, MSYS2 or WSL2</summary>

PowerShell is the tested shell. From Git Bash or MSYS2, run `./takeout.cmd` rather than `./takeout.sh`.

WSL2 runs the Linux build. It works, but reading zips from `/mnt/c` or `/mnt/d` is several times slower than native Windows, and Windows creation dates are not set. Prefer the native `takeout.exe`.
</details>

#### Linux

```sh
sudo apt install golang libimage-exiftool-perl ffmpeg   # or your distribution's packages
git clone https://github.com/asmodeoux/google-photos-takeout.git
cd google-photos-takeout
./takeout.sh doctor
```

### 3. Check this computer

`doctor` finds ExifTool, checks the results folder can be written, and writes and reads back a date in a file with a non-English name:

```
info  system      darwin/arm64
ok    exiftool    13.59  /opt/homebrew/bin/exiftool
ok    results     /Users/you/google-photos-takeout/results
info  disk        apfs, 296 GB free
ok    utf-8 paths wrote and read back probe-Фото-é.jpg
ok    ffmpeg      7.1.1  /opt/homebrew/bin/ffmpeg

All required checks passed.
```

A failed check prints what it found, the fix for your system, and the section of this README that explains it.

### 4. Read the plan

`check` reads the zips and writes nothing:

```sh
./takeout.sh check                                   # zips in archives/
./takeout.sh check --archives "/Volumes/Disk/Takeout"
```
```powershell
.\takeout.cmd check --archives "D:\Takeout"
```

This is the output for the synthetic test export in this repository:

```
parts 2  missing []  exports [20240101T000000Z]
filesystem apfs  free 296 GB  need about 0 GB

Review
  media in zips     72
  sidecars          61
  unique files      70
  dated             66
  unknown date      4
  with GPS          11
  without GPS       59
  live photo pairs  5
  ...
Next: ./takeout.sh run --default-tz Area/City
```

A missing zip part stops here with the part numbers, before anything is copied.

### 5. Try a small run

Pick the time zone where most photos were taken, as an [IANA name](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) such as `America/New_York` or `Europe/Berlin`. It is used only for files with no GPS and no camera offset.

```sh
./takeout.sh run --sample 50 --results results-try --default-tz Europe/Berlin
```

Open a few files from `results-try/<year>` and check the dates and places. Delete `results-try` when you are done.

### 6. Build the library

```sh
./takeout.sh run --default-tz Europe/Berlin
```
```powershell
.\takeout.cmd run --archives "D:\Takeout" --results "E:\Photos library" --default-tz Europe/Berlin
```

A large export takes hours; the computer is kept awake while it runs. Stop it with Ctrl+C and run the same command again to resume. Finished files are not copied twice.

### 7. Import

Import `results/<year>` folders and `results/unknown`. **Do not import `results/albums` as well**: it holds second copies of the same photos, grouped by album, and every album photo would appear twice.

- **Apple Photos on a Mac.** File > Import, select the year folders. Keep each Live Photo's still and `.MOV` in the same import. Photos does not turn folders into albums; `results/albums` shows what each Takeout album held.
- **iCloud Photos from Windows or Linux.** Copy the results folder to a Mac and import there, or upload the year folders at [icloud.com/photos](https://www.icloud.com/photos). Live Photos are only reassembled reliably by Apple Photos on a Mac.
- **Windows Photos, Immich, Lightroom, digiKam** and other apps read the dates and GPS from the files; import the year folders as usual.

`import-photos` (macOS, experimental) is the start of an automatic import. Today it checks the library you pass and prints the AppleScript it will run; it does not import files or create albums yet. It refuses the iCloud-synced system library unless you add `--confirm-icloud`, because everything imported there uploads to iCloud.

### 8. Verify

```sh
./takeout.sh verify
./takeout.sh status
```

`verify` checks that every file the report lists is still on disk and in the right year folder. `results/.takeout/report.txt` is the readable summary; `report.json` has the [fields below](#reportjson-fields).

## Commands

| What it does | Windows | macOS and Linux |
|---|---|---|
| Check this computer | `.\takeout.cmd doctor` | `./takeout.sh doctor` |
| Read the zips, print the plan | `.\takeout.cmd check` | `./takeout.sh check` |
| Build the library (resumes) | `.\takeout.cmd run` | `./takeout.sh run` |
| One line about the last run | `.\takeout.cmd status` | `./takeout.sh status` |
| Re-check results against the report | `.\takeout.cmd verify` | `./takeout.sh verify` |
| Extract the zips into `unzipped/` | `.\takeout.cmd unzip` | `./takeout.sh unzip` |
| Prepare a Photos import (experimental) | not available | `./takeout import-photos` (macOS) |

With a prebuilt binary use `.\takeout.exe` or `./takeout`. `takeout <command> -h` lists the flags each command takes.

## Flags

| Flag | Commands | Meaning |
|---|---|---|
| `--archives DIR` | check, run, unzip | Folder with the zips. Default `archives`. |
| `--results DIR` | check, run, status, verify, doctor | Output folder. Default `results`. |
| `--default-tz ZONE` | check, run | IANA zone for files with no GPS and no camera offset. |
| `--sample N` | run | Process only N files. For a try-out. |
| `--albums clone\|copy\|none` | check, run | Album folders as APFS clones (no extra space, macOS), full copies, or not at all. Other disks get copies. |
| `--names auto\|apple\|portable` | check, run | Which characters file and folder names may keep. `auto` is `portable` on NTFS, exFAT, FAT and on Windows, `apple` elsewhere. |
| `--exclude-screenshots` | check, run | Leave out files named `Screenshot…` or `Screen Shot…`. |
| `--include-trash` | check, run | Include the Trash folder. |
| `--keep-unzipped` | run | Also extract the zips into `unzipped/`. |
| `--no-keep-awake` | run, unzip | Let the computer sleep during the run. |
| `--exiftool PATH`, `--ffmpeg PATH` | several | Use these programs instead of the ones on PATH. |
| `--progress auto\|tty\|plain`, `--quiet` | check, run, unzip | Progress output. |
| `--library PATH`, `--confirm-icloud` | import-photos | Photos library to import into; allow the iCloud one. |

## Exit codes

| Code | Meaning | What to do |
|---|---|---|
| 0 | Done, and every zip entry is accounted for. | Import. |
| 2 | Stopped before writing: missing part, ExifTool, disk space. | Follow the `Fix:` line and run again. |
| 3 | Some files from the zips are not in the library, or a check of the library failed. | See `failed_files` in `report.json` (a damaged zip part must be downloaded again) or the message, fix it, and run again. |
| 4 | Finished, but some files did not take tags. | The library is usable. See `tag_error_files` in `report.json`. |
| 130 | Interrupted. | Run the same command; it resumes. |

## report.json fields

| Field | Meaning |
|---|---|
| `media`, `sidecars` | Photo and video entries, and JSON sidecars, in the zips. |
| `unique` | Distinct files after removing identical copies (the same photo in a year folder and an album). |
| `library`, `unknown` | Files placed in a year folder, and in `unknown/`. |
| `placeholders` | Google's stand-in images for files it could not export. |
| `live_pairs`, `identifier_copied` | Live Photo pairs, and pairs that needed a new `ContentIdentifier`. |
| `tag_errors`, `tag_error_files` | Files that did not take their tags, with ExifTool's message (up to 40). |
| `years`, `formats` | Files per year folder and per detected type. |
| `date_sources`, `timezone_steps` | Where each date and time zone came from. |
| `with_gps` | Files with a real location. |
| `untagged` | Files placed with their file dates only, because ExifTool cannot write that format: Canon CR3, AVI, MPEG, WMV, MTS, BMP. |
| `failed`, `failed_files` | Files from the zips that are not in the library (a damaged zip entry, a file that could not be placed), with the reason. Exit code 3. |
| `system_files_ignored`, `symlinks_skipped` | `._` files, `.DS_Store`, `Thumbs.db`, `__MACOSX` entries and symbolic links in the zips. |
| `names_rule`, `names_reason`, `album_renames` | The naming rule used, why, and album folders whose names had to change. |
| `extension_fixes` | Files whose extension did not match their content, such as a PNG named `.jpg`. |
| `filesystem`, `exiftool_version`, `export_ids` | Facts about this run. |
| `birth_time_errors`, `birth_time_error_files` | Files whose creation date could not be set. Tags are unaffected. |
| `retries` | Renames and tag writes that waited for another program to release a file. |
| `seconds`, `files_per_second` | Time per phase, and tagging speed. |
| `errors` | Everything else worth reading. |

## What is different on Windows

| | Windows | macOS |
|---|---|---|
| Album folders | Full copies. Use `--albums none` to save space. | APFS clones, no extra space. |
| File names | `< > : " \| ? * \ /` become `-`, and names such as `CON` get a suffix. Listed in `album_renames`. | Only `/`, `\` and `:` are replaced. |
| Creation date in Explorer / Finder | Set to the photo's date. | Set to the photo's date. |
| Long and non-English paths | Supported with ExifTool 13.07 or newer. | Supported. |
| Import into Apple Photos | Copy the results to a Mac. | File > Import in Photos. |

## Troubleshooting

<a id="zips"></a><a id="archives"></a>
**No Takeout zip files found.** The message shows the folder that was searched. Put the `takeout-*.zip` files there, or pass the folder: `--archives "D:\Takeout"`. Do not unzip them.

<a id="missing-parts"></a>
**The export is incomplete.** Download the missing parts again from takeout.google.com (Manage exports). The message names each part.

<a id="exiftool"></a><a id="exiftool-windows"></a>
**ExifTool not found.** Install it (see [Install](#2-install)) and open a new terminal window. On Windows, `winget` installs it on PATH. If you downloaded the zip from exiftool.org instead, rename `exiftool(-k).exe` to `exiftool.exe` and keep the `exiftool_files` folder next to it; the `(-k)` build waits for a key press and cannot be used. ExifTool 13.07 or newer is required on Windows.

<a id="execution-policy"></a>
**"running scripts is disabled on this system".** Use `.\takeout.cmd`, not `.\takeout.ps1`. The `.cmd` file runs the script without changing your execution policy.

<a id="disk-space"></a>
**Not enough free space.** The library needs about the size of the unique photos, plus the album copies when the disk is not APFS. Free space, use `--albums none`, or write to a bigger disk with `--results`.

<a id="fat32"></a>
**Files of 4 GiB or more on a FAT32 disk.** FAT32 cannot store them. Format the disk as exFAT or NTFS, or use another disk.

<a id="names"></a>
**The --names choice does not fit this disk.** `apple` names keep characters that NTFS and exFAT refuse. Use `--names auto`.

<a id="in-use"></a>
**Another takeout is using the results folder.** Wait for the other run to finish, or close it. The lock ends with the process, so a crashed run never leaves the folder locked.

<a id="paths"></a>
**Folder name with a line break.** Rename the folder or choose another one.

<a id="antivirus"></a>
**Slow on Windows, many retries.** Windows Defender and the search indexer open every new file. Excluding the results folder from scanning (Windows Security > Virus & threat protection > Exclusions) makes runs faster. `retries` in `report.json` shows how often takeout had to wait.

<a id="long-paths"></a>
**Long paths on Windows.** takeout and ExifTool 13.07+ handle paths over 260 characters. Some other programs do not; `doctor` shows whether Windows long paths are switched on.

<a id="verify"></a>
**No report in the results folder.** Run `run` first, or pass the folder it wrote with `--results`.

<a id="exit-3"></a>
**Exit 3.** Files from the zips did not reach the library: `failed_files` in `report.json` lists each one with the reason, usually a damaged zip part ("checksum error"). Download that part again from takeout.google.com, replace it in the archives folder, and run the same command; finished files are kept. From `verify`, exit 3 also means a library file is missing or in the wrong year folder.

**Exit 4.** Some files did not take tags. They are still in the library with their original metadata. `tag_error_files` in `report.json` lists them with ExifTool's message.

**A video shows the wrong day.** Check that `--default-tz` is the zone where it was filmed. If it still happens, open a bug with the report counts, not the video.

## FAQ

**Does it upload anything?** No. It reads your zips and writes files on your disk.

**Can I run it again?** Yes. It resumes from the journal in `results/.takeout/` and never overwrites a finished file.

**Why are some photos in `unknown`?** Neither the JSON, the file, nor its name had a date that could be trusted. They are still there; set their dates in your photo app.

**What about edited copies (`-edited.jpg`)?** They are kept as separate files, dated from the original's JSON.

**Motion Photos from a Pixel?** A `.MP` video next to its `.MP.jpg` becomes a Live Photo pair. A JPEG with a video embedded inside stays a JPEG, counted in `motion_photos`.

**RAW files?** DNG, CR2, NEF, ARW and other TIFF-based RAW files get dates and GPS like any photo. Canon CR3 files are placed with their camera date; a CR3 next to a JPEG of the same name keeps that name, so Photos pairs them.

**Photos from before 1970?** A date you set in Google Photos on an old scan is kept, back to 1800.

## Why this tool

[GooglePhotosTakeoutHelper](https://github.com/TheLastGimbus/GooglePhotosTakeoutHelper), [gpth-rs](https://github.com/jl1nie/gpth-rs), [immich-go](https://github.com/simulot/immich-go) and [takeoutfix](https://github.com/vchilikov/takeoutfix) each solve part of a Takeout. This tool is for people who want the files to open correctly in Apple Photos and other apps, including video time zones and Live Photos, on any of the three systems, with a report that accounts for every file. The test suite builds a synthetic Takeout with the layouts reported in GooglePhotosTakeoutHelper's issue tracker (truncated sidecar names, `.metadata.json`, case-mismatched names, localized folders, `._` files, Pixel `.MP` videos, RAW) and runs it end to end on Windows, macOS and Linux on every change.

Sidecar name rules follow GooglePhotosTakeoutHelper and the [51-character truncation notes](https://gist.github.com/AkuEgor/41f758cdf305d6c97608cd5f06a140fc). Filename date patterns and localized folder names follow gpth-rs. The code is new; those projects are not vendored. An agent can run the whole process from [AGENTS.md](AGENTS.md).

Do not attach photos, videos or GPS coordinates to a bug report. See [SECURITY.md](SECURITY.md) and [CONTRIBUTING.md](CONTRIBUTING.md).
