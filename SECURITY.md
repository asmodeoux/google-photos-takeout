# Security

This program reads zip files you already have and writes copies on your disk. It does not upload photos and it does not talk to Google or iCloud.

## Reporting a vulnerability

Use GitHub private vulnerability reporting on this repository. Do not open a public issue for a security problem, and do not attach a Takeout archive, a photo, or a GPS track.

Describe what an attacker could do. The useful kind of bug here is a zip entry that writes outside the output directory, or a filename that is passed to ExifTool or ffmpeg as an option.
