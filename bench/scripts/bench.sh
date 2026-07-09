#!/usr/bin/env bash
# Runs the full bench suite `count` times with allocation stats on, the
# Go-native precursor to a criterion-style statistical report: repeated
# samples so benchstat (golang.org/x/perf/cmd/benchstat) can report a mean
# and confidence interval per benchmark instead of a single noisy number.
#
# Usage:
#   scripts/bench.sh [label] [count] [benchtime] [regex]
#
#   label      name for the output file under bench/results/ (default: "run")
#   count      repetitions per benchmark, fed to -count (default: 10)
#   benchtime  time (or Nx iterations) per repetition (default: 200ms)
#   regex      -bench filter (default: . -- everything)
#
# Output: bench/results/<label>.txt (raw `go test -bench` output) and
# bench/results/<label>.benchstat.txt (benchstat's formatted summary).
#
# To compare before/after an optimization:
#   scripts/bench.sh baseline
#   # make your change
#   scripts/bench.sh candidate
#   scripts/compare.sh baseline candidate
set -euo pipefail

label="${1:-run}"
count="${2:-10}"
benchtime="${3:-200ms}"
regex="${4:-.}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="$root/results"
mkdir -p "$out_dir"

raw="$out_dir/$label.txt"
summary="$out_dir/$label.benchstat.txt"

echo "==> go test -bench=$regex -benchtime=$benchtime -count=$count -benchmem ./bench/..." >&2
(cd "$root/.." && go test -run='^$' -bench="$regex" -benchtime="$benchtime" -count="$count" -benchmem ./bench/...) | tee "$raw"

if command -v benchstat >/dev/null 2>&1; then
  benchstat "$raw" | tee "$summary"
elif [ -x "$(go env GOPATH)/bin/benchstat" ]; then
  "$(go env GOPATH)/bin/benchstat" "$raw" | tee "$summary"
else
  echo "benchstat not found -- install with: go install golang.org/x/perf/cmd/benchstat@latest" >&2
fi

echo "==> raw:     $raw" >&2
echo "==> summary: $summary" >&2
