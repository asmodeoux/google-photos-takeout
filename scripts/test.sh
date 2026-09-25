#!/bin/sh
# Runs every test verbosely, prints a summary and the tests that skipped, and
# fails if a test skipped that is not expected to skip on this OS. A green run
# then means the tests really ran, not that they quietly stepped aside.
# Usage: scripts/test.sh [expected skipped test names...]
set -u
cd "$(dirname "$0")/.."
log=$(mktemp)
trap 'rm -f "$log"' EXIT
go test -count=1 -v ./... >"$log" 2>&1
status=$?
grep -E '^(ok|FAIL|---|panic:)|^\s+--- FAIL' "$log" | grep -v -- '--- PASS' | grep -v -- '--- SKIP'
grep -E -- '--- FAIL|^panic:' "$log" >/dev/null && sed -n '/--- FAIL/,/^FAIL/p' "$log" | head -200
echo
echo "Tests run: $(grep -c -- '--- PASS' "$log") passed, $(grep -c -- '--- FAIL' "$log") failed, $(grep -c -- '--- SKIP' "$log") skipped"
unexpected=0
for name in $(grep -oE -- '--- SKIP: [A-Za-z0-9_/]+' "$log" | sed 's/--- SKIP: //'); do
  reason=$(grep -B1 -- "--- SKIP: $name " "$log" | head -1 | sed 's/^ *[^ ]*_test.go:[0-9]*: //')
  top=${name%%/*}
  ok=no
  for allowed in "$@"; do
    [ "$top" = "$allowed" ] && ok=yes
  done
  if [ "$ok" = yes ]; then
    echo "skipped (expected): $name: $reason"
  else
    echo "skipped (NOT expected): $name: $reason"
    unexpected=1
  fi
done
if [ "$status" -ne 0 ]; then
  exit "$status"
fi
if [ "$unexpected" -ne 0 ]; then
  echo "A test skipped that should run on this OS." >&2
  exit 1
fi
