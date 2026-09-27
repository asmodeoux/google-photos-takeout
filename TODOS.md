# TODOs

Deferred from the Windows support plan, and known limitations.

- **ContentIdentifier on stills without Apple MakerNotes.** ExifTool cannot create Apple MakerNotes, so `-Apple:ContentIdentifier` is silently not written to a still that has none (JPEG and HEIC from non-Apple cameras, and synthetic files). The video half gets its identifier; Apple Photos may not pair the two. Options: write the identifier where Photos also reads it, or copy a MakerNotes block. Needs a test on a real Mac import.
- **Finish `import-photos`.** It checks the library choice and prints an AppleScript, but does not import files or create albums yet.
- **Pixel Motion Photo to Live Photo** (GooglePhotosTakeoutHelper PR #384). Split a JPEG with an embedded video into a still and a `.MOV` with a shared identifier.
- **Code-signed release binaries.** Tagged releases publish binaries with SHA256SUMS, but they are not signed: Windows SmartScreen and macOS Gatekeeper warn on first run. Sign and notarize them.
- **Hardlink albums on NTFS.** Album folders are full copies off APFS. Hard links would save the space, with the caveat that editing one copy edits all.
- **ReFS block cloning** on Windows Server and Dev Drive, like APFS clones.
- **Historical time zones before about 1900.** GPS time-zone lookup can give local mean time offsets with seconds; EXIF offsets hold only minutes, so the written instant can be off by seconds for such old dated scans.
- **Faster place and album phases.** On a real 24k-file export (1.1.0, M4 Pro, APFS) place took 238 s of 751 s, more than tagging, though it only renames files and writes two journal lines each; albums took 102 s for 970 clones. Measure where the time goes (journal writes per file are the likely cost), batch or buffer them without losing crash safety, and compare against the baseline in the local benchmarks folder.
- **Repair damaged EXIF instead of a tag error.** Some phone edits carry a broken EXIF block ("Bad format (0) for ExifIFD entry 0"), and ExifTool refuses to write them: 5 files in a real export. ExifTool can rebuild the block from the tags it can still read (`-all= -tagsfromfile @ -all:all -unsafe -icc_profile`, ExifTool FAQ 20). Offer that as an opt-in retry for these files, keep the original bytes until the rebuilt file reads back, and add a corpus row with a synthetic damaged ExifIFD.
- **Faster duplicate confirmation.** Files that share size and CRC32 are hashed one at a time before extraction. 1.1.0 shows progress; hashing in parallel did not ship because it was never measured on an external hard disk, where parallel reads can be slower. Run `BenchmarkConfirmDuplicates` with `TAKEOUT_BENCH_DUPS_DIR` on a USB hard disk first, and reuse the hash when extracting.
- **An ETA on the duplicate progress line.**
- **`takeout status --json`** for agents and scripts.
- **A doctor check that keeps one ExifTool running** through a few hundred writes, to catch antivirus that kills it only under load.
- **Recycling ExifTool processes** did not ship: memory stayed flat at 46.8 MB over 20,000 writes (ExifTool 13.25, macOS arm64). Measure again on Windows and Linux with `TAKEOUT_MEASURE_RSS=1` before adding it.
- **Windows Job Object for ExifTool.** The process tree is found from a snapshot right after exiftool.exe exits; a job object with kill-on-close would need no process ids at all and close the small id-reuse window left.
- **`import-photos` must skip non-media by kind** once it lists files, including documents that 1.0.0 placed in year folders.
- **Pin ffmpeg in CI.** Windows CI installs the current ffmpeg from Chocolatey without a checksum. It is only used by tests, not shipped.
- **Dates before 1990 in file names and camera clocks** are rejected as likely wrong. Sidecar dates are accepted back to 1800.

## Test coverage to add

Scenarios the synthetic Takeout and CI do not cover yet.

- **Paths over 260 characters on Windows.** No corpus row sends a file with a long full path through ExifTool on Windows.
- **Zip entries with non-UTF-8 (CP437) names,** as older zip tools write them.
- **Two different exports mixed in one archives folder, end to end.** Only the zip index unit test covers it.
- **Launcher argument passing:** `takeout.cmd`, `takeout.ps1` and `takeout.sh` with paths that contain spaces, quotes, `&` and `%`; CI runs only `version` through them.
- **`--sample` end to end,** and videos dated before 1970.
- **exFAT and FAT32 results disks in CI:** create and format a virtual disk in the Windows job and run the corpus onto it.
- **Windows on ARM:** run the tests and the `windows-arm64` binary on a Windows ARM runner.
- **Double-clicking `takeout.exe`:** the pause message needs a desktop session; check it by hand.

