#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD_DIR="${BUILD_DIR:-$ROOT/.research-server/lean-build}"
mkdir -p "$BUILD_DIR"
"${CXX:-c++}" -O3 -std=c++17 -Wall -Wextra "$ROOT/scripts/near_binary_bfs256.cpp" -o "$BUILD_DIR/near-binary-bfs256"
python3 "$ROOT/scripts/near_binary_bfs.py" --generator "$BUILD_DIR/near-binary-bfs256" --r-values 2,3,4,5 --max-n 40 --output "$BUILD_DIR/near-binary-bfs256.json" --check
printf '%s\n' 'extended near-binary checks: r=2..5, n=8..40, computed formulas verified'
