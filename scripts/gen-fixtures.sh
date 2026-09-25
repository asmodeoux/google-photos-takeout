#!/bin/sh
# Regenerates the tiny synthetic media in internal/testgen/testdata.
# Needs macOS (sips for HEIC), ffmpeg with libx264, libvpx and libwebp, and exiftool.
# Every file is generated from ffmpeg test patterns; none comes from a camera.
set -eu
cd "$(dirname "$0")/.."
out=internal/testgen/testdata
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
ff="ffmpeg -loglevel error -y"
for i in 1 2 3; do
  hue=$((i * 90))
  $ff -f lavfi -i "testsrc=size=64x48:rate=1:duration=1" -vf "hue=h=$hue" -frames:v 1 "$tmp/b$i.png"
  sips -s format heic "$tmp/b$i.png" --out "$out/still$i.heic" >/dev/null
  $ff -i "$tmp/b$i.png" -c:v libwebp -map_metadata -1 "$out/still$i.webp"
  src="-f lavfi -i testsrc=size=64x48:rate=10:duration=0.5 -vf hue=h=$hue -map_metadata -1 -fflags +bitexact"
  $ff $src -c:v libx264 -pix_fmt yuv420p -f mp4 "$out/clip$i.mp4"
  $ff $src -c:v libx264 -pix_fmt yuv420p -f mov "$out/clip$i.mov"
  $ff $src -c:v libx264 -pix_fmt yuv420p -f 3gp "$out/clip$i.3gp"
  $ff $src -c:v libvpx -b:v 50k -f matroska "$out/clip$i.mkv"
  $ff $src -c:v libvpx -b:v 50k -f webm "$out/clip$i.webm"
done
exiftool -q -all= -overwrite_original "$out"/*.heic "$out"/*.webp "$out"/*.mp4 "$out"/*.mov "$out"/*.3gp
ls -l "$out"
