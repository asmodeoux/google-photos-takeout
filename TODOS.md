# TODOs

Deferred from the Windows support plan, and known limitations.

- **ContentIdentifier on stills without Apple MakerNotes.** ExifTool cannot create Apple MakerNotes, so `-Apple:ContentIdentifier` is silently not written to a still that has none (JPEG and HEIC from non-Apple cameras, and synthetic files). The video half gets its identifier; Apple Photos may not pair the two. Options: write the identifier where Photos also reads it, or copy a MakerNotes block. Needs a test on a real Mac import.
- **Finish `import-photos`.** It checks the library choice and prints an AppleScript, but does not import files or create albums yet.
- **Pixel Motion Photo to Live Photo** (GooglePhotosTakeoutHelper PR #384). Split a JPEG with an embedded video into a still and a `.MOV` with a shared identifier.
- **Code-signed release binaries.** Tagged releases publish binaries with SHA256SUMS, but they are not signed: Windows SmartScreen and macOS Gatekeeper warn on first run. Sign and notarize them.
- **Hardlink albums on NTFS.** Album folders are full copies off APFS. Hard links would save the space, with the caveat that editing one copy edits all.
- **ReFS block cloning** on Windows Server and Dev Drive, like APFS clones.
- **Historical time zones before about 1900.** GPS time-zone lookup can give local mean time offsets with seconds; EXIF offsets hold only minutes, so the written instant can be off by seconds for such old dated scans.
- **Restart a crashed ExifTool.** A client that dies mid-run is not restarted; its share of the remaining files fails as tag errors (exit 4) until the next run.
- **Faster duplicate confirmation.** Files that share size and CRC32 are hashed one at a time before extraction; a large export with many album copies spends a while there with no progress line. Hash in parallel and reuse the hash when extracting.
- **Pin ffmpeg in CI.** Windows CI installs the current ffmpeg from Chocolatey without a checksum. It is only used by tests, not shipped.
- **Dates before 1990 in file names and camera clocks** are rejected as likely wrong. Sidecar dates are accepted back to 1800.

## Test coverage to add

Scenarios the synthetic Takeout and CI do not cover yet.

- **Google placeholder images.** No corpus row has one; the report always says `placeholders 0`.
- **`--exclude-screenshots` and `--include-trash` end to end.** Only the helper functions are tested.
- **Paths over 260 characters on Windows.** No corpus row sends a file with a long full path through ExifTool on Windows.
- **Zip entries with non-UTF-8 (CP437) names,** as older zip tools write them.
- **Localized "edited" suffixes** (`-bearbeitet`, `-modifié`, `-編集済み`): matched in code, but the corpus has only `-edited`.
- **Two different exports mixed in one archives folder, end to end.** Only the zip index unit test covers it.
- **Other files in the Google Photos folder** (`.txt`, `.pdf`): how they are placed and reported.
- **Launcher argument passing:** `takeout.cmd`, `takeout.ps1` and `takeout.sh` with paths that contain spaces, quotes, `&` and `%`; CI runs only `version` through them.
- **`--sample` end to end,** and videos dated before 1970.
- **exFAT and FAT32 results disks in CI:** create and format a virtual disk in the Windows job and run the corpus onto it.
- **Windows on ARM:** run the tests and the `windows-arm64` binary on a Windows ARM runner.
- **Double-clicking `takeout.exe`:** the pause message needs a desktop session; check it by hand.

