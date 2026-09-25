# TODOs

Deferred from the Windows support plan, and known limitations.

- **ContentIdentifier on stills without Apple MakerNotes.** ExifTool cannot create Apple MakerNotes, so `-Apple:ContentIdentifier` is silently not written to a still that has none (JPEG and HEIC from non-Apple cameras, and synthetic files). The video half gets its identifier; Apple Photos may not pair the two. Options: write the identifier where Photos also reads it, or copy a MakerNotes block. Needs a test on a real Mac import.
- **Finish `import-photos`.** It checks the library choice and prints an AppleScript, but does not import files or create albums yet.
- **Pixel Motion Photo to Live Photo** (GooglePhotosTakeoutHelper PR #384). Split a JPEG with an embedded video into a still and a `.MOV` with a shared identifier.
- **Signed, tagged releases.** CI artifacts need a GitHub sign-in and expire. Publish signed binaries on tagged releases.
- **Hardlink albums on NTFS.** Album folders are full copies off APFS. Hard links would save the space, with the caveat that editing one copy edits all.
- **ReFS block cloning** on Windows Server and Dev Drive, like APFS clones.
- **Dates before 1990 in file names and camera clocks** are rejected as likely wrong. Sidecar dates are accepted back to 1800.
