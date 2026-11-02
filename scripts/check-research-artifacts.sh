#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LEAN_BIN="${LEAN_BIN:-$ROOT/.research-server/lean4/bin/lean}"
BUILD_DIR="${BUILD_DIR:-$ROOT/.research-server/lean-build}"
mkdir -p "$BUILD_DIR/Wrapped4"
export LEAN_PATH="$BUILD_DIR"

"$LEAN_BIN" --version
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Arithmetic.olean" "$ROOT/examples/research/wrapped4/Arithmetic.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Certificates.olean" "$ROOT/examples/research/wrapped4/Certificates.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4/Repeated.olean" "$ROOT/examples/research/wrapped4/Repeated.lean"
"$LEAN_BIN" -o "$BUILD_DIR/Wrapped4.olean" "$ROOT/examples/research/wrapped4/Wrapped4.lean"

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
#print axioms Wrapped4.leftWrapped4_map
#print axioms Wrapped4.rightWrapped4_map
#print axioms Wrapped4.applyWrappedOps_map
EOF

OUTPUT="$("$LEAN_BIN" "$AUDIT")"
printf '%s\n' "$OUTPUT"
if printf '%s\n' "$OUTPUT" | grep -E 'sorryAx|native_decide|ofReduceBool|ofReduceNat' >/dev/null; then
  echo 'Disallowed computational axiom or native reduction found.' >&2
  exit 1
fi
