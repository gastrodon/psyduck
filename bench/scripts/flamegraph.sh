#!/usr/bin/env bash
# Renders a flame-graph PNG (and, with `top`, a call-graph PNG) from a
# profile already captured by profile.sh, via headless Chromium against
# `go tool pprof -http`. Needs Node + the Playwright package + a Chromium
# binary; this repo's dev container has both preinstalled -- see the NODE_PATH
# / PLAYWRIGHT_* exports below if running elsewhere.
#
# Usage: scripts/flamegraph.sh <label>
set -euo pipefail

label="${1:?label required, e.g. jq (matches results/<label>.cpu.prof from profile.sh)}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$root/results"

bin="$out/$label.test"
cpu="$out/$label.cpu.prof"
[ -f "$cpu" ] || { echo "missing $cpu -- run scripts/profile.sh $label <regex> first" >&2; exit 1; }

export NODE_PATH="${NODE_PATH:-/opt/node22/lib/node_modules}"
export PLAYWRIGHT_BROWSERS_PATH="${PLAYWRIGHT_BROWSERS_PATH:-/opt/pw-browsers}"

node "$root/scripts/flamegraph.js" "$bin" "$cpu" "$out/$label.flamegraph.png" --route flamegraph
node "$root/scripts/flamegraph.js" "$bin" "$cpu" "$out/$label.callgraph.png" --route top
