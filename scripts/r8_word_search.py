#!/usr/bin/env python3
from near_binary_witness import apply_op


def base_ops(n):
    return [
        ("left", 0), ("left", 0), ("left", 4), ("right", 5),
        ("left", 4), ("right", 6), ("right", 7), ("left", 5),
        ("right", n - 3), ("left", 0), ("right", n - 3),
        ("right", n - 1), ("left", n - 4), ("right", n - 1),
    ]


def run(ops, n):
    state = list(range(8)) + [8] * (n - 8)
    for direction, index in ops:
        state = apply_op(state, {"direction": direction, "index": index})
    return state == list(reversed(list(range(8)) + [8] * (n - 8)))


def moves(n):
    return [(direction, index) for direction in ("left", "right") for index in range(n)]


def two_insertions(base, n):
    result = []
    available = moves(n)
    for first in range(len(base) + 1):
        for second in range(first, len(base) + 1):
            for a in available:
                for b in available:
                    ops = base[:]
                    ops.insert(first, a)
                    ops.insert(second, b)
                    if run(ops, n):
                        result.append(ops)
    return result


def replacement_with_insertion(base, n):
    found = []
    available = moves(n)
    for delete in range(len(base)):
        shortened = base[:delete] + base[delete + 1:]
        for first in range(len(shortened) + 1):
            for second in range(first, len(shortened) + 1):
                for a in available:
                    for b in available:
                        ops = shortened[:]
                        ops.insert(first, a)
                        ops.insert(second, b)
                        if run(ops, n):
                            found.append(ops)
    return found


def two_replacements_with_insertion(base, n):
    found = []
    available = moves(n)
    for ops16 in two_insertions(base, n):
        for move in available:
            ops = ops16 + [move]
            for first in range(len(ops)):
                for second in range(first + 1, len(ops)):
                    candidate = [op for i, op in enumerate(ops) if i != first and i != second]
                    if run(candidate, n):
                        found.append(candidate)
    return found


def main():
    n = 16
    base = base_ops(n)
    longer = two_insertions(base, n)
    if len(longer) != 84:
        raise ValueError(f"unexpected number of longer words: {len(longer)}")
    first = replacement_with_insertion(base, n)
    second = two_replacements_with_insertion(base, n)
    if first or second:
        raise ValueError("new minimal candidate requires independent review")
    print({"length_16_candidates": len(longer), "replacement_with_insertion": 0, "two_replacements_with_insertion": 0})


if __name__ == "__main__":
    main()
