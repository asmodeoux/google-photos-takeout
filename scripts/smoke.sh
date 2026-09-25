#!/bin/sh
# Runs the built takeout binary on the synthetic corpus and checks the result.
# Usage: scripts/smoke.sh [path/to/takeout]
# SMOKE_RESULTS=dir keeps a copy of the results folder there.
set -eu
cd "$(dirname "$0")/.."
bin=${1:-}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
if [ -z "$bin" ]; then
  go build -o "$work/takeout" ./cmd/takeout
  bin="$work/takeout"
fi
case "$bin" in /*) ;; *) bin="$PWD/$bin" ;; esac
case "${SMOKE_RESULTS:-}" in ""|/*) ;; *) SMOKE_RESULTS="$PWD/$SMOKE_RESULTS" ;; esac
go run ./cmd/testgen "$work/archives" >/dev/null

# Relative folders, as with the defaults: archives/ and results/ here.
cd "$work"
"$bin" version
"$bin" doctor
"$bin" check
start=$(date +%s)
"$bin" run --default-tz Europe/Berlin --progress plain --quiet
echo "run took $(( $(date +%s) - start ))s"
"$bin" verify
"$bin" status

report="$work/results/.takeout/report.json"
sed -n '/"seconds"/,/}/p' "$report"
grep '"files_per_second"' "$report" || true
grep -q '"tag_errors": 0' "$report" || { echo "tag errors in $report" >&2; cat "$report" >&2; exit 1; }
grep -q '"live_pairs": 5' "$report" || { echo "live pairs missing" >&2; exit 1; }
if [ -n "${SMOKE_RESULTS:-}" ]; then
  rm -rf "$SMOKE_RESULTS"
  cp -R "$work/results" "$SMOKE_RESULTS"
fi
echo "smoke ok"
