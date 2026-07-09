#!/usr/bin/env bash
# Statistically compares two raw `run.sh` outputs with benchstat -- the
# Go-native equivalent of `cargo criterion --baseline`. Needs -count>=1 in
# both files; more repetitions (run.sh's default is 10) means a tighter
# confidence interval and a more trustworthy delta %.
#
# Usage: bench/diff.sh <old-label> <new-label>
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
old="$root/results/${1:?old label required}.txt"
new="$root/results/${2:?new label required}.txt"

for f in "$old" "$new"; do
  [ -f "$f" ] || { echo "missing $f -- run bench/run.sh first" >&2; exit 1; }
done

benchstat_bin="$(command -v benchstat || echo "$(go env GOPATH)/bin/benchstat")"
if [ ! -x "$benchstat_bin" ]; then
  echo "benchstat not found -- install with: go install golang.org/x/perf/cmd/benchstat@latest" >&2
  exit 1
fi

"$benchstat_bin" "$old" "$new"
