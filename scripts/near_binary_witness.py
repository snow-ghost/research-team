#!/usr/bin/env python3
import argparse
import json
import os
import re
import subprocess
from pathlib import Path

SCHEMA = "near-binary-witnesses@1"


def initial_state(n, copies):
    return list(range(n - copies)) + [n - copies] * copies


def apply_op(state, op):
    n = len(state)
    block = [state[(op["index"] + j) % n] for j in range(4)]
    shift = 3 if op["direction"] == "right" else 1
    result = state[:]
    for j in range(4):
        result[(op["index"] + j) % n] = block[(j + shift) % 4]
    return result


def parse_ops(text):
    if re.fullmatch(r"(?:[LR]\d+ )*", text) is None:
        raise ValueError("invalid operation list")
    ops = []
    for direction, index in re.findall(r"([LR])(\d+)", text):
        ops.append({"direction": "left" if direction == "L" else "right", "index": int(index)})
    return ops


def parse_report(report, n, copies):
    header = re.search(r"states=(\d+) eccentricity=(\d+)", report)
    flip = re.search(r"flip_distance=(\d+) flip_word=([LR0-9 ]*)", report)
    if header is None or flip is None:
        raise ValueError("incomplete generator report")
    witnesses = []
    for match in re.finditer(r"^state=([0-9]+) word=([LR0-9 ]*)$", report, re.MULTILINE):
        state = [int(value) for value in match.group(1)]
        ops = parse_ops(match.group(2))
        if len(state) != n or len(ops) != int(header.group(2)):
            raise ValueError("invalid witness dimensions")
        witnesses.append({"state": state, "ops": ops, "distance": len(ops)})
    if not witnesses:
        raise ValueError("generator returned no diameter witness")
    flip_ops = parse_ops(flip.group(2))
    if len(flip_ops) != int(flip.group(1)):
        raise ValueError("invalid flip word")
    return {
        "schema": SCHEMA,
        "n": n,
        "copies": copies,
        "unique": n - copies,
        "states": int(header.group(1)),
        "diameter": int(header.group(2)),
        "exhaustive": True,
        "initial": initial_state(n, copies),
        "witnesses": witnesses,
        "flip": {
            "state": list(reversed(initial_state(n, copies))),
            "ops": flip_ops,
            "distance": len(flip_ops),
        },
    }


def validate(data):
    n = data["n"]
    expected_states = 1
    for value in range(data["copies"] + 1, n + 1):
        expected_states *= value
    if data["states"] != expected_states:
        raise ValueError(f"state count mismatch: {data['states']} != {expected_states}")
    for witness in data["witnesses"] + [data["flip"]]:
        state = data["initial"]
        if any(op["index"] >= n for op in witness["ops"]):
            raise ValueError("operation index out of range")
        for op in witness["ops"]:
            state = apply_op(state, op)
        if state != witness["state"]:
            raise ValueError("witness word does not produce its state")
        if witness["distance"] != len(witness["ops"]):
            raise ValueError("witness distance mismatch")


def six_unique_far_ops(n):
    if n < 10:
        return []
    ops = [
        {"direction": "left", "index": 0},
        {"direction": "left", "index": 1},
    ]
    ops += [{"direction": "right", "index": i} for i in range(3, n - 7)]
    ops += [
        {"direction": direction, "index": index}
        for direction, index in (
            ("right", n - 3),
            ("right", n - 2),
            ("right", n - 1),
            ("left", n - 4),
            ("right", n - 6),
            ("right", n - 7),
        )
    ]
    return ops


def attach_hypotheses(data):
    macro = six_unique_far_ops(data["n"])
    indices = [i for i, witness in enumerate(data["witnesses"]) if witness["ops"] == macro]
    data["hypotheses"] = []
    if indices:
        data["hypotheses"].append({
            "id": "six-unique-far",
            "witness_indices": indices,
            "word_length": len(macro),
            "lean_lemma": "Wrapped4.sixUniqueFarOps_correct_extra",
            "status": "general_word_verified_not_a_general_diameter_proof",
        })


def lean_atom(value):
    return str(value)


def lean_list(values):
    return "[" + ", ".join(lean_atom(value) for value in values) + "]"


def lean_ops(ops):
    return "[" + ", ".join(f"Op.{op['direction']} {op['index']}" for op in ops) + "]"


def lean_source(data, witness_index):
    witness = data["witnesses"][witness_index]
    name = f"nearBinary{data['n']}Unique{data['unique']}Witness{witness_index}"
    return f"""import Wrapped4.Core

namespace Wrapped4

def {name}Ops : List Op :=
  {lean_ops(witness['ops'])}

theorem {name}_correct :
    applyWrappedOps {name}Ops {lean_list(data['initial'])} = {lean_list(witness['state'])} := by
  decide

theorem {name}_length : {name}Ops.length = {witness['distance']} := by
  rfl

#print axioms Wrapped4.{name}_correct
#print axioms Wrapped4.{name}_length

end Wrapped4
"""


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--generator", required=True)
    parser.add_argument("--n", type=int, required=True)
    parser.add_argument("--unique", type=int, required=True)
    parser.add_argument("--expected-diameter", type=int)
    parser.add_argument("--output", required=True)
    parser.add_argument("--witness-index", type=int, default=0)
    parser.add_argument("--use-hypothesis", action="store_true")
    parser.add_argument("--lean-output")
    parser.add_argument("--lean-bin")
    parser.add_argument("--lean-path")
    args = parser.parse_args()
    if args.unique < 1 or args.unique > 15 or args.n <= args.unique:
        parser.error("require 1 <= unique < n and unique <= 15")
    if not 0 <= args.witness_index < 12:
        parser.error("witness index must be in 0..11")
    report = subprocess.run(
        [args.generator, str(args.n), str(args.n - args.unique)],
        check=True,
        capture_output=True,
        text=True,
    ).stdout
    data = parse_report(report, args.n, args.n - args.unique)
    validate(data)
    attach_hypotheses(data)
    if args.expected_diameter is not None and data["diameter"] != args.expected_diameter:
        raise ValueError(f"diameter mismatch: {data['diameter']} != {args.expected_diameter}")
    if args.use_hypothesis:
        if not data["hypotheses"]:
            parser.error("no supported macro hypothesis matches a witness")
        args.witness_index = data["hypotheses"][0]["witness_indices"][0]
    if args.witness_index >= len(data["witnesses"]):
        parser.error("requested witness index is absent")
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
    if args.lean_output:
        lean_output = Path(args.lean_output)
        lean_output.parent.mkdir(parents=True, exist_ok=True)
        lean_output.write_text(lean_source(data, args.witness_index))
        if args.lean_bin:
            env = os.environ.copy()
            if args.lean_path:
                env["LEAN_PATH"] = args.lean_path
            checked = subprocess.run(
                [args.lean_bin, str(lean_output)],
                check=True,
                capture_output=True,
                text=True,
                env=env,
            )
            if re.search(r"sorryAx|native_decide|ofReduceBool|ofReduceNat", checked.stdout):
                raise ValueError("disallowed Lean axiom in generated certificate")
    print(json.dumps({
        "n": data["n"],
        "unique": data["unique"],
        "states": data["states"],
        "diameter": data["diameter"],
        "witnesses": len(data["witnesses"]),
        "hypotheses": [item["id"] for item in data["hypotheses"]],
        "verified_words": True,
        "lean_checked": bool(args.lean_output and args.lean_bin),
    }))


if __name__ == "__main__":
    main()
