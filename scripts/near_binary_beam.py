#!/usr/bin/env python3
import argparse
import itertools
import json

from near_binary_witness import apply_op


def potential(state, n):
    score = 0
    for value in range(6):
        position = state.index(value)
        difference = abs(position - value)
        score += min(difference, n - difference)
    inversions = sum(
        state[i] > state[j]
        for i in range(6)
        for j in range(i + 1, 6)
        if state[i] < 6 and state[j] < 6
    )
    return score + 2 * inversions


def step(state, index, inverse):
    return tuple(apply_op(list(state), {"direction": "right" if inverse else "left", "index": index}))


def sorted_state(n):
    return tuple(range(6)) + (6,) * (n - 6)


def sort_with_beam(start, n, width, max_depth):
    target = sorted_state(n)
    beam = {start}
    if start == target:
        return 0
    for depth in range(1, max_depth + 1):
        following = set()
        for state in beam:
            for index in range(n):
                for inverse in (False, True):
                    following.add(step(state, index, inverse))
        if target in following:
            return depth
        beam = set(sorted(following, key=lambda state: potential(state, n))[:width])
    return None


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--n", type=int, default=10)
    parser.add_argument("--width", type=int, default=100)
    parser.add_argument("--max-depth", type=int)
    args = parser.parse_args()
    if args.n < 6 or args.width < 1:
        parser.error("require n >= 6 and positive width")
    max_depth = args.max_depth if args.max_depth is not None else args.n - 2
    histogram = {}
    for order in itertools.permutations(range(6)):
        start = tuple(order) + (6,) * (args.n - 6)
        depth = sort_with_beam(start, args.n, args.width, max_depth)
        if depth is None:
            raise ValueError(f"state was not sorted within {max_depth}: {start}")
        histogram[depth] = histogram.get(depth, 0) + 1
    print(json.dumps({
        "n": args.n,
        "width": args.width,
        "states": sum(histogram.values()),
        "max_depth": max(histogram),
        "histogram": histogram,
        "all_sorted": True,
        "general_theorem": False,
    }, sort_keys=True))


if __name__ == "__main__":
    main()
