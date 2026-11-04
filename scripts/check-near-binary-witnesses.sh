#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LEAN_BIN="${LEAN_BIN:-$ROOT/.research-server/lean4/bin/lean}"
BUILD_DIR="${BUILD_DIR:-$ROOT/.research-server/lean-build}"
GENERATOR="$BUILD_DIR/near-binary-bfs128"
export LEAN_PATH="$BUILD_DIR"

run_witness() {
  local n="$1"
  local unique="$2"
  local diameter="$3"
  python3 "$ROOT/scripts/near_binary_witness.py" \
    --generator "$GENERATOR" \
    --n "$n" \
    --unique "$unique" \
    --expected-diameter "$diameter" \
    --output "$BUILD_DIR/near-binary-r${unique}-n${n}.json" \
    --lean-output "$BUILD_DIR/NearBinaryR${unique}N${n}.lean" \
    --lean-bin "$LEAN_BIN" \
    --lean-path "$BUILD_DIR"
}

run_witness 10 6 8
run_witness 11 6 9
run_witness 12 6 10
run_witness 13 6 11
run_witness 14 6 12
run_witness 15 6 13
run_witness 16 6 14
run_witness 17 6 15
run_witness 18 6 16
run_witness 10 7 8
run_witness 12 7 11
run_witness 15 7 14
run_witness 11 8 10
printf '%s\n' 'near-binary witnesses: exhaustive BFS, independent words and Lean certificates verified'
