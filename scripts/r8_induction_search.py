#!/usr/bin/env python3
import json

from near_binary_witness import apply_op

BASE15 = [
    ("left", 0), ("left", 0), ("left", 4), ("right", 5),
    ("left", 4), ("right", 6), ("right", 7), ("left", 5),
    ("right", 12), ("left", 0), ("right", 12),
    ("right", 14), ("left", 11), ("right", 14),
]

BASE16 = [
    ("left", 15), ("left", 14), ("right", 0), ("left", 14),
    ("right", 11), ("left", 14), ("left", 13), ("left", 11),
    ("right", 5), ("right", 6), ("left", 8), ("left", 4),
    ("right", 6), ("left", 8), ("left", 7), ("right", 5),
]


def moves(n):
    return [(direction, index) for direction in ("left", "right") for index in range(n)]


def solves(ops, n):
    initial = list(range(8)) + [8] * (n - 8)
    state = initial
    for direction, index in ops:
        state = apply_op(state, {"direction": direction, "index": index})
    return state == list(reversed(initial))


def word(ops):
    return " ".join(("L" if direction == "left" else "R") + str(index) for direction, index in ops)


def search(base, n, include_double=False):
    available = moves(n)
    found = {name: [] for name in ("direct", "delete", "replace", "insert", "shifted_insert")}
    if solves(base, n):
        found["direct"].append(base)
    for position in range(len(base) + 1):
        for move in available:
            candidate = base[:position] + [move] + base[position:]
            if solves(candidate, n):
                found["insert"].append(candidate)
    for deleted in range(len(base)):
        shortened = base[:deleted] + base[deleted + 1:]
        if solves(shortened, n):
            found["delete"].append(shortened)
        for move in available:
            candidate = shortened[:deleted] + [move] + shortened[deleted:]
            if solves(candidate, n):
                found["replace"].append(candidate)
    for cut in range(n):
        shifted = [(direction, index + (1 if index >= cut else 0)) for direction, index in base]
        for position in range(len(shifted) + 1):
            for move in available:
                candidate = shifted[:position] + [move] + shifted[position:]
                if solves(candidate, n):
                    found["shifted_insert"].append(candidate)
    if include_double:
        found["double_insert"] = []
        for first in range(len(base) + 1):
            for second in range(first, len(base) + 1):
                for a in available:
                    for b in available:
                        candidate = base[:]
                        candidate.insert(first, a)
                        candidate.insert(second, b)
                        if solves(candidate, n):
                            found["double_insert"].append(candidate)
    return found


def main():
    control = search(BASE15, 16)
    control_counts = {name: len(items) for name, items in control.items()}
    if any(control_counts[name] for name in ("direct", "delete", "replace", "insert", "shifted_insert")):
        raise ValueError("a length-15 flip for n=16 contradicts the exhaustive BFS certificate")
    candidates = search(BASE16, 17, include_double=True)
    print(json.dumps({
        "control_15_to_16_length_15": control_counts,
        "candidate_16_to_17": {
            name: {"count": len(items), "words": [word(ops) for ops in items[:3]]}
            for name, items in candidates.items()
        },
        "general_theorem": False,
    }, ensure_ascii=False, indent=2, sort_keys=True))
    if any(candidates.values()):
        raise ValueError("new r=8 candidate requires independent verification and Lean certification")


if __name__ == "__main__":
    main()
