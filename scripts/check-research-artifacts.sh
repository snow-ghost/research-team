#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LEAN_BIN="${LEAN_BIN:-$ROOT/.research-server/lean4/bin/lean}"
BUILD_DIR="${BUILD_DIR:-$ROOT/.research-server/lean-build}"
mkdir -p "$BUILD_DIR/Wrapped4"
export LEAN_PATH="$BUILD_DIR"

"$LEAN_BIN" --version
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Arithmetic.olean" "$ROOT/examples/research/wrapped4/Arithmetic.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Core.olean" "$ROOT/examples/research/wrapped4/Core.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Certificates.olean" "$ROOT/examples/research/wrapped4/Certificates.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Repeated.olean" "$ROOT/examples/research/wrapped4/Repeated.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/NearBinary.olean" "$ROOT/examples/research/wrapped4/NearBinary.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/NearBinaryFlip.olean" "$ROOT/examples/research/wrapped4/NearBinaryFlip.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4.olean" "$ROOT/examples/research/wrapped4/Wrapped4.lean"
python3 "$ROOT/scripts/check_macro_evaluation.py" \
  --cases "$ROOT/examples/evaluation-macro-cases.json" \
  --lean-bin "$LEAN_BIN" \
  --build-dir "$BUILD_DIR"
python3 "$ROOT/scripts/near_binary_potential.py"
python3 "$ROOT/scripts/check_agent_answer.py" --self-test
python3 "$ROOT/scripts/near_binary_beam.py"

"${CXX:-c++}" -O3 -std=c++17 -Wall -Wextra "$ROOT/scripts/repeated_bfs_fast.cpp" -o "$BUILD_DIR/repeated-bfs-fast"
"${CXX:-c++}" -O3 -std=c++17 -Wall -Wextra "$ROOT/scripts/near_binary_bfs128.cpp" -o "$BUILD_DIR/near-binary-bfs128"
"${CXX:-c++}" -O3 -std=c++17 -Wall -Wextra "$ROOT/scripts/near_binary_potential_search.cpp" -o "$BUILD_DIR/near-binary-potential-search"
"$BUILD_DIR/near-binary-potential-search"
"${CXX:-c++}" -O3 -std=c++17 -Wall -Wextra "$ROOT/scripts/near_binary_bidirectional.cpp" -o "$BUILD_DIR/near-binary-bidirectional"
BIDIRECTIONAL_OUTPUT="$("$BUILD_DIR/near-binary-bidirectional" 8)"
printf '%s\n' "$BIDIRECTIONAL_OUTPUT" | grep -F 'distance=16' >/dev/null
BFS_OUTPUT="$("$BUILD_DIR/repeated-bfs-fast" 11 2)"
printf '%s\n' "$BFS_OUTPUT"
printf '%s\n' "$BFS_OUTPUT" | grep -F 'states=19958400 eccentricity=10' >/dev/null
printf '%s\n' "$BFS_OUTPUT" | grep -F 'flip_distance=10' >/dev/null
python3 "$ROOT/scripts/near_binary_bfs.py" --generator "$BUILD_DIR/repeated-bfs-fast" --output "$BUILD_DIR/near-binary-bfs.json" --check
printf '%s\n' 'near-binary checks: r=1..5, n=8..16, states and r=1/r=2 formulas verified'
python3 "$ROOT/scripts/near_binary_bfs.py" --generator "$BUILD_DIR/near-binary-bfs128" --r-values 2,3,4,5 --max-n 32 --output "$BUILD_DIR/near-binary-bfs128.json" --check
printf '%s\n' '128-bit checks: r=2..5, n=8..32, computed diameter and flip formulas verified'
bash "$ROOT/scripts/check-near-binary-witnesses.sh"

AUDIT="$BUILD_DIR/ResearchArtifactAudit.lean"
trap 'rm -f "$AUDIT"' EXIT
cat > "$AUDIT" <<'EOF'
import Wrapped4
#print axioms Wrapped4.fullFlip7Ops_correct
#print axioms Wrapped4.fullFlip7Ops_length
#print axioms Wrapped4.baseCert_correct
#print axioms Wrapped4.minimalLength_spec
#print axioms Wrapped4.repeated_cost_upper_all
#print axioms Wrapped4.repeated_cost_witness
#print axioms Wrapped4.repeated_cost_formula
#print axioms Wrapped4.repeated_cost_base
#print axioms Wrapped4.twoCopies10FlipOps_correct
#print axioms Wrapped4.twoCopies10FarOps_correct
#print axioms Wrapped4.twoCopies11FlipOps_correct
#print axioms Wrapped4.twoCopies11FarOps_correct
#print axioms Wrapped4.twoCopies12FlipOps_correct
#print axioms Wrapped4.twoCopies12FarOps_correct
#print axioms Wrapped4.leftWrapped4_map
#print axioms Wrapped4.rightWrapped4_map
#print axioms Wrapped4.applyWrappedOps_map
#print axioms Wrapped4.twoUniqueFlipOps_correct
#print axioms Wrapped4.twoUniqueFlipOps_length
#print axioms Wrapped4.twoUniqueFlipOps_correct_six
#print axioms Wrapped4.twoUniqueFlipOps_counterexample_five
#print axioms Wrapped4.sixUniqueFarOps_correct
#print axioms Wrapped4.sixUniqueFarOps_correct_list
#print axioms Wrapped4.sixUniqueFarOps_correct_extra
#print axioms Wrapped4.sixUniqueFarOps_length
#print axioms Wrapped4.sevenUniqueFlipOps_correct
#print axioms Wrapped4.sevenUniqueFlipOps_length
#print axioms Wrapped4.sevenUniqueFlip14Ops_correct
#print axioms Wrapped4.sevenUniqueFlip14Ops_length
#print axioms Wrapped4.eightUniqueFlip15Ops_correct
#print axioms Wrapped4.eightUniqueFlip15Ops_length
EOF

OUTPUT="$("$LEAN_BIN" "$AUDIT")"
printf '%s\n' "$OUTPUT"
if printf '%s\n' "$OUTPUT" | grep -E 'sorryAx|native_decide|ofReduceBool|ofReduceNat' >/dev/null; then
  echo 'Disallowed computational axiom or native reduction found.' >&2
  exit 1
fi
