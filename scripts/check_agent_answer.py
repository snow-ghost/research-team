#!/usr/bin/env python3
import argparse
import json
import re
from pathlib import Path

ALLOWED_PROOF_TACTICS = {"intro", "exact", "omega"}
ALLOWED_REFUTATION_TACTICS = {"unfold", "decide"}
FORBIDDEN = ("Mathlib", "Proof.", "Qed.", "sorry", "native_decide", "admit", "axiom", "```")


def validate_answer(answer, candidate, declaration, mode, library_theorem):
    if set(answer) != {"outcome", "source", "evidence"}:
        raise ValueError("expected exactly outcome, source and evidence")
    expected_outcome = "refutation" if mode == "refutation" else "proof"
    if answer["outcome"] != expected_outcome:
        raise ValueError(f"outcome must be {expected_outcome}")
    source = answer["source"]
    if not source.startswith("import Goal\n"):
        raise ValueError("source must start with import Goal")
    if "\nimport " in source:
        raise ValueError("only import Goal is allowed")
    for token in FORBIDDEN:
        if token in source:
            raise ValueError(f"forbidden token: {token}")
    signature = f"theorem {candidate} : {declaration} := by"
    if signature not in source:
        raise ValueError("missing exact Lean theorem signature")
    if library_theorem not in source:
        raise ValueError("library theorem is not used")
    if mode == "proof" and "intro " not in source:
        raise ValueError("variables must be introduced before exact")
    allowed = ALLOWED_REFUTATION_TACTICS if mode == "refutation" else ALLOWED_PROOF_TACTICS
    for token in re.findall(r"\b(?:intro|exact|omega|simp|norm_num|unfold|decide|apply|refine|rw|simpa|aesop|linarith)\b", source):
        if token not in allowed:
            raise ValueError(f"tactic is not allowed: {token}")
    if not isinstance(answer["evidence"], str) or len(answer["evidence"].split()) > 40:
        raise ValueError("evidence must be at most 40 words")
    return True


def load_answer(path):
    outer = json.loads(Path(path).read_text())
    if "candidate" in outer:
        return json.loads(outer["candidate"])
    return outer


def self_test():
    valid = {
        "outcome": "proof",
        "source": "import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
        "evidence": "library application",
    }
    validate_answer(valid, "Candidate202", "Research202", "proof", "Wrapped4.sixUniqueFarOps_correct")
    for source in (
        "import Mathlib\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
        "import Goal\nimport Library\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
        "import Goal\ntheorem Candidate202 : Research202 := by\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x",
        "import Goal\ntheorem Candidate202 : Research202 := by\n  intro n hn a b c d e f x\n  exact Wrapped4.sixUniqueFarOps_correct hn a b c d e f x\nQed.",
    ):
        try:
            validate_answer({**valid, "source": source}, "Candidate202", "Research202", "proof", "Wrapped4.sixUniqueFarOps_correct")
        except ValueError:
            continue
        raise ValueError("invalid answer accepted")
    print("agent answer preflight: valid answer accepted, Mathlib, missing intro and Coq rejected")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--answer")
    parser.add_argument("--candidate", default="Candidate202")
    parser.add_argument("--declaration", default="Research202")
    parser.add_argument("--mode", choices=("proof", "refutation"), default="proof")
    parser.add_argument("--library-theorem", default="Wrapped4.sixUniqueFarOps_correct")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
        return
    if not args.answer:
        parser.error("--answer is required unless --self-test is used")
    answer = load_answer(args.answer)
    validate_answer(answer, args.candidate, args.declaration, args.mode, args.library_theorem)
    print("agent answer preflight: accepted")


if __name__ == "__main__":
    main()
