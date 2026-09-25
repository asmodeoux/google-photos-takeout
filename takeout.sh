#!/bin/sh
# One command on macOS and Linux: check tools, build, and run takeout.
set -eu
cd "$(dirname "$0")"

case "$(uname -s)" in
  Darwin) install_go="brew install go"; install_exiftool="brew install exiftool"; install_ffmpeg="brew install ffmpeg" ;;
  *) install_go="see https://go.dev/dl/"; install_exiftool="sudo apt install libimage-exiftool-perl (or your distribution's exiftool package)"; install_ffmpeg="sudo apt install ffmpeg" ;;
esac

if ! command -v go >/dev/null 2>&1; then
  echo "Go is required to build takeout. Install it with: $install_go" >&2
  exit 2
fi
# ExifTool can also be passed with --exiftool; takeout itself reports a
# missing one with the fix, so this is only a hint.
if ! command -v exiftool >/dev/null 2>&1; then
  echo "note: exiftool is not on PATH. Install it with: $install_exiftool" >&2
fi
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "note: ffmpeg is optional. Without it, WebM and MKV videos go to results/not-importable/. Install with: $install_ffmpeg" >&2
fi

go build -o takeout ./cmd/takeout
TAKEOUT_LAUNCHER=./takeout.sh
export TAKEOUT_LAUNCHER

# Before a run, check this computer the same way "takeout doctor" does, with
# the same --results, --exiftool and --ffmpeg.
doctor() {
  results="" exif="" ff="" prev=""
  for a in "$@"; do
    case "$prev" in
      --results|-results) results=$a ;;
      --exiftool|-exiftool) exif=$a ;;
      --ffmpeg|-ffmpeg) ff=$a ;;
    esac
    case "$a" in
      --results=*|-results=*) results=${a#*=} ;;
      --exiftool=*|-exiftool=*) exif=${a#*=} ;;
      --ffmpeg=*|-ffmpeg=*) ff=${a#*=} ;;
    esac
    prev=$a
  done
  set -- doctor
  [ -n "$results" ] && set -- "$@" --results "$results"
  [ -n "$exif" ] && set -- "$@" --exiftool "$exif"
  [ -n "$ff" ] && set -- "$@" --ffmpeg "$ff"
  ./takeout "$@"
}
if [ "${1:-}" = run ]; then
  doctor "$@" || exit $?
fi
exec ./takeout "$@"
