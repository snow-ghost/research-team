#!/usr/bin/env python3
import argparse
import json
import os
import re
import subprocess
from pathlib import Path

PROOFS = {
    201: ("Research201", "intro a b c d e f x\n  exact sixUniqueFarOps_correct (by omega) a b c d e f x"),
    202: ("Research202", "intro n hn a b c d e f x\n  exact sixUniqueFarOps_correct hn a b c d e f x"),
    203: ("Research203", "intro n hn\n  exact sixUniqueFarOps_length hn"),
    204: ("Research204", "rfl"),
    205: ("Research205", "intro C a b c d e f x\n  exact sixUniqueFarOps_correct_list C a b c d e f x"),
    206: ("Research206", "intro G m a b c d e f x\n  exact sixUniqueFarOps_correct_extra G m a b c d e f x"),
    207: ("Research207", "intro C D k\n  exact take_append_add C D k"),
    208: ("Research208", "intro P B a b c d\n  exact rightWrapped4_append P B a b c d"),
    209: ("¬ Research209", "unfold Research209\n  decide"),
    210: ("¬ Research210", "unfold Research210\n  decide"),
    211: ("¬ Research211", "intro h\n  have h1 := h 1\n  simp [sixUniqueFarOps, moveRightOps] at h1"),
    212: ("¬ Research212", "intro h\n  have h0 := h 0 0 0 0 0 0 0 0\n  simp [sixUniqueFarOps, moveRightOps, applyWrappedOps, applyWrappedOp,\n    leftWrapped4, rightWrapped4] at h0"),
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--cases", required=True)
    parser.add_argument("--lean-bin", required=True)
    parser.add_argument("--build-dir", required=True)
    args = parser.parse_args()
    cases = json.loads(Path(args.cases).read_text())["cases"]
    if len(cases) != 12 or {case["id"] for case in cases} != {f"task-{i}" for i in range(201, 213)}:
        raise ValueError("unexpected macro evaluation case set")
    if len({case["class"] for case in cases}) != 6:
        raise ValueError("six macro evaluation classes required")
    build = Path(args.build_dir)
    build.mkdir(parents=True, exist_ok=True)
    goals = build / "MacroEvaluationGoals.lean"
    audit = build / "MacroEvaluationAudit.lean"
    body = "\n".join("\n".join(case["goal"]["source"].splitlines()[1:]) for case in cases)
    goals.write_text("import Wrapped4.NearBinary\n" + body)
    declarations = []
    for case in cases:
        number = int(case["id"].split("-")[1])
        goal_type, proof = PROOFS[number]
        declarations.append(f"theorem Native{number} : {goal_type} := by\n  {proof}\n\n#print axioms Native{number}\n")
    audit.write_text("import MacroEvaluationGoals\n\nopen Wrapped4\n\n" + "\n".join(declarations))
    env = os.environ.copy()
    env["LEAN_PATH"] = str(build)
    subprocess.run([args.lean_bin, "-o", str(build / "MacroEvaluationGoals.olean"), str(goals)], check=True, env=env)
    checked = subprocess.run([args.lean_bin, str(audit)], check=True, capture_output=True, text=True, env=env)
    if re.search(r"sorryAx|native_decide|ofReduceBool|ofReduceNat", checked.stdout):
        raise ValueError("disallowed Lean axiom in macro evaluation baseline")
    print("macro evaluation: 12 goals compiled, 8 proofs and 4 refutations verified without model calls")


if __name__ == "__main__":
    main()
