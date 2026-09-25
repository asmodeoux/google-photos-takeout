takeout: turn a Google Photos Takeout into a library Apple Photos imports
with the right dates, places, Live Photos and albums.

This folder comes from a takeout release, built from the source by the
project's GitHub Actions:
https://github.com/asmodeoux/google-photos-takeout/releases
SHA256SUMS.txt on that page lists each download's checksum.

You also need ExifTool (required) and ffmpeg (optional, for WebM/MKV).

Windows (PowerShell, in this folder):
  winget install --id OliverBetz.ExifTool -e
  winget install --id Gyan.FFmpeg -e
  (open a new PowerShell window afterwards)
  Unblock-File .\takeout.exe
  .\takeout.exe doctor
  .\takeout.exe check --archives "D:\Takeout"
  .\takeout.exe run --archives "D:\Takeout" --default-tz Europe/Berlin

  If SmartScreen says "Windows protected your PC", choose More info, then
  Run anyway. The file is not signed.

macOS and Linux (Terminal, in this folder):
  chmod +x takeout
  ./takeout doctor
  ./takeout check --archives /Volumes/Disk/Takeout
  ./takeout run --archives /Volumes/Disk/Takeout --default-tz Europe/Berlin

  On macOS, if the file "cannot be opened because the developer cannot be
  verified", run: xattr -d com.apple.quarantine takeout

The full guide is README.md on the GitHub page above.
