#!/bin/sh
# One command: check tools, build, and run.
set -eu
cd "$(dirname "$0")"

missing=0
if ! command -v go >/dev/null 2>&1; then
  echo "Go is required. Install it from https://go.dev/dl/" >&2
  missing=1
fi
if ! command -v exiftool >/dev/null 2>&1; then
  echo "ExifTool is required. Install it with: brew install exiftool" >&2
  missing=1
fi
if [ "$missing" -ne 0 ]; then
  exit 2
fi
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "note: ffmpeg is optional. Without it, WebM files are copied to results/not-importable/ and are not imported into Photos. Install with: brew install ffmpeg" >&2
fi

go build -o takeout ./cmd/takeout
TAKEOUT_LAUNCHER=./takeout.sh
export TAKEOUT_LAUNCHER
exec ./takeout "$@"
