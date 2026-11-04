#!/usr/bin/env python3
import argparse
import json
import os
import re
import subprocess
from pathlib import Path

from near_binary_witness import apply_op, initial_state, lean_list, lean_ops, parse_ops

SCHEMA = "near-binary-bidirectional-witness@1"


def parse_report(report):
    left = re.search(r"^left=(\d+) right=(\d+)$", report, re.MULTILINE)
    distance = re.search(r"^distance=(\d+)$", report, re.MULTILINE)
    lower_bound = re.search(r"^no_path_through=(\d+)$", report, re.MULTILINE)
    state = re.search(r"^state=([0-9]+)$", report, re.MULTILINE)
    word = re.search(r"^word=([LR0-9 ]*)$", report, re.MULTILINE)
    verified = re.search(r"^verified=true$", report, re.MULTILINE)
    if None in (left, distance, lower_bound, state, word, verified):
        raise ValueError("incomplete bidirectional generator report")
    return {
        "left": int(left.group(1)),
        "right": int(left.group(2)),
        "distance": int(distance.group(1)),
        "no_path_through": int(lower_bound.group(1)),
        "meeting_state": [int(value) for value in state.group(1)],
        "ops": parse_ops(word.group(1)),
    }


def validate(data, n, unique):
    initial = initial_state(n, n - unique)
    target = list(reversed(initial))
    ops = data["ops"]
    if data["distance"] != len(ops) or data["left"] + data["right"] != len(ops):
        raise ValueError("bidirectional word length mismatch")
    if data["no_path_through"] >= len(ops):
        raise ValueError("lower bound does not establish minimality")
    if any(op["index"] >= n for op in ops):
        raise ValueError("operation index out of range")
    meeting = initial
    for op in ops[:data["left"]]:
        meeting = apply_op(meeting, op)
    if meeting != data["meeting_state"]:
        raise ValueError("left half does not produce the meeting state")
    result = meeting
    for op in ops[data["left"]:]:
        result = apply_op(result, op)
    if result != target:
        raise ValueError("bidirectional word does not produce the flip")
    data.update({
        "schema": SCHEMA,
        "n": n,
        "unique": unique,
        "initial": initial,
        "target": target,
        "exhaustive_no_path_through": data["no_path_through"],
        "independently_verified": True,
    })
    return data


def lean_source(data):
    return f"""import Wrapped4.Core

namespace Wrapped4

def eightUniqueFlip16Ops : List Op :=
  {lean_ops(data['ops'])}

theorem eightUniqueFlip16Ops_correct :
    applyWrappedOps eightUniqueFlip16Ops {lean_list(data['initial'])} = {lean_list(data['target'])} := by
  decide

theorem eightUniqueFlip16Ops_length : eightUniqueFlip16Ops.length = {data['distance']} := by
  rfl

#print axioms Wrapped4.eightUniqueFlip16Ops_correct
#print axioms Wrapped4.eightUniqueFlip16Ops_length

end Wrapped4
"""


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--generator", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--lean-output")
    parser.add_argument("--lean-bin")
    parser.add_argument("--lean-path")
    args = parser.parse_args()
    report = subprocess.run(
        [args.generator, "8"], check=True, capture_output=True, text=True
    ).stdout
    data = validate(parse_report(report), 16, 8)
    if data["distance"] != 16 or data["no_path_through"] != 15:
        raise ValueError("expected distance 16 and exhaustive exclusion through 15")
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    if args.lean_output:
        lean_output = Path(args.lean_output)
        lean_output.parent.mkdir(parents=True, exist_ok=True)
        lean_output.write_text(lean_source(data))
        if args.lean_bin:
            env = os.environ.copy()
            if args.lean_path:
                env["LEAN_PATH"] = args.lean_path
            checked = subprocess.run(
                [args.lean_bin, str(lean_output)], check=True, capture_output=True, text=True, env=env
            )
            if re.search(r"sorryAx|native_decide|ofReduceBool|ofReduceNat", checked.stdout):
                raise ValueError("disallowed Lean axiom in generated certificate")
    print(json.dumps({
        "n": data["n"],
        "unique": data["unique"],
        "distance": data["distance"],
        "no_path_through": data["no_path_through"],
        "left": data["left"],
        "right": data["right"],
        "verified_words": True,
        "lean_checked": bool(args.lean_output and args.lean_bin),
    }))


if __name__ == "__main__":
    main()
