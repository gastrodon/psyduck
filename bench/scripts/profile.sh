#!/usr/bin/env bash
# Builds the bench test binary once and captures CPU + memory profiles for a
# given -bench regex, then renders the text reports and the pprof call graph
# (the "box -> box" graph `go tool pprof -svg`/`-http` draws via graphviz).
#
# Usage: scripts/profile.sh <label> <bench-regex> [benchtime]
#
# Output, all under bench/results/<label>.*:
#   <label>.test            compiled test binary (kept for later pprof calls)
#   <label>.cpu.prof        CPU profile
#   <label>.mem.prof        heap allocation profile
#   <label>.cpu-top.txt     flat (self) time, text
#   <label>.cpu-top-cum.txt cumulative time, text
#   <label>.cpu-graph.svg   the call graph -- boxes are functions, edges are
#                           calls, sized/weighted by sample time
#   <label>.mem-top.txt     top allocators by bytes (alloc_space)
#
# Requires graphviz's `dot` on PATH for the SVG (apt install graphviz).
set -euo pipefail

label="${1:?label required, e.g. jq}"
regex="${2:?bench regex required, e.g. BenchmarkByJQ_vs_Compiled}"
benchtime="${3:-2s}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/results"
mkdir -p "$out"

bin="$out/$label.test"
cpu="$out/$label.cpu.prof"
mem="$out/$label.mem.prof"

echo "==> profiling -bench=$regex (benchtime=$benchtime)" >&2
(cd "$root/.." && go test -run='^$' -bench="$regex" -benchtime="$benchtime" -benchmem \
  -cpuprofile="$cpu" -memprofile="$mem" -o "$bin" ./bench/...)

echo "==> pprof text reports" >&2
go tool pprof -top -nodecount=30 "$bin" "$cpu" > "$out/$label.cpu-top.txt"
go tool pprof -top -cum -nodecount=30 "$bin" "$cpu" > "$out/$label.cpu-top-cum.txt"
go tool pprof -top -sample_index=alloc_space -nodecount=30 "$bin" "$mem" > "$out/$label.mem-top.txt" || true

echo "==> pprof call graph (svg)" >&2
if command -v dot >/dev/null 2>&1; then
  go tool pprof -svg "$bin" "$cpu" > "$out/$label.cpu-graph.svg"
else
  echo "graphviz 'dot' not found -- skipping call graph SVG (apt install graphviz)" >&2
fi

echo "==> wrote:" >&2
ls -la "$out/$label".* >&2
