#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../server"

threshold=90
pkgs=()
for name in rules linking stacks merge; do
  [ -d "internal/$name" ] && pkgs+=("./internal/$name")
done

if [ "${#pkgs[@]}" -eq 0 ]; then
  echo "coverage-check: no gated packages yet, nothing to check"
  exit 0
fi

profile=$(mktemp)
trap 'rm -f "$profile"' EXIT
out=$(go test -race -coverprofile="$profile" "${pkgs[@]}")
echo "$out"

echo "$out" | awk -v min="$threshold" '
  /^ok / {
    seen++
    for (i = 1; i <= NF; i++) if ($i == "coverage:") { pct = $(i + 1) + 0; found = 1 }
    if (!found) { printf "coverage-check: no coverage for %s\n", $2; bad++ }
    else if (pct < min) { printf "coverage-check: %s is at %.1f%%, below %d%%\n", $2, pct, min; bad++ }
    found = 0
  }
  END {
    if (seen == 0) { print "coverage-check: no test results"; exit 1 }
    if (bad > 0) exit 1
    printf "coverage-check ok: %d package(s) at or above %d%%\n", seen, min
  }'
