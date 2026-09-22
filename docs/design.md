# Design notes

## Go, not Rust

The slow work is reading and writing the library. ExifTool, which writes the tags, is a separate program either way. A Rust port would not change the wait in a way you can feel. Go is one `go build` for someone who clones the repo and drops zips in `archives/`.

## Why ExifTool

Go libraries that write metadata cover still images. They do not write QuickTime `Keys:CreationDate` or `Keys:GPSCoordinates`. Those are the tags Apple Photos uses for a video's date and place. A missing timezone on that tag is what puts a video on the wrong day. ExifTool stays open for the whole run so it is not started once per file.

## Timezone

JSON timestamps are UTC instants. The year folder and the clock stored in the file use this order:

1. The timezone of the GPS point, including daylight saving on that date.
2. An offset already stored in the file.
3. The camera's wall clock minus the JSON instant, when the difference is a whole 15-minute step.
4. The zone of a photo taken within 36 hours that already resolved.
5. `--default-tz`, or else the most common zone in this library.
6. UTC.

A filename date such as `IMG_20190509_154733` is a wall clock with no zone. It is written as those digits, with no offset.

## Matching

Sidecars are looked up in the same Takeout folder, even when that folder is split across zip parts. The same filename in two folders can be two different photos, so a global basename index is not used. Identical bytes are one library file. Album folders get another directory entry for that file.

## Live Photos

An Apple Live Photo is a still and a short video that share `ContentIdentifier`. Takeout often names the video `.MP4` even when the container is QuickTime. The video is renamed to `.MOV`, kept beside the still, and never loses its identifier. A Google Motion Photo (a JPEG with a video attached, or a `.MP` file) is left as it is and listed in the report.
