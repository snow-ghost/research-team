#!/usr/bin/env python3
import argparse
import json
import re
import subprocess
from collections import deque

from near_binary_witness import apply_op


def step(state, n, index, inverse):
    return tuple(apply_op(list(state), {"direction": "right" if inverse else "left", "index": index}))


def exhaustive_bfs(n):
    initial = tuple(range(6)) + (6,) * (n - 6)
    target = tuple(reversed(initial))
    distances = {initial: 0}
    queue = deque([initial])
    farthest = initial
    while queue:
        state = queue.popleft()
        depth = distances[state]
        if depth > distances[farthest]:
            farthest = state
        for index in range(n):
            for inverse in (False, True):
                following = step(state, n, index, inverse)
                if following not in distances:
                    distances[following] = depth + 1
                    queue.append(following)
    return {
        "n": n,
        "states": len(distances),
        "diameter": distances[farthest],
        "flip_distance": distances[target],
        "independent_python_bfs": True,
    }


def generator_summary(command):
    report = subprocess.run(command, check=True, capture_output=True, text=True).stdout
    header = re.search(r"states=(\d+) eccentricity=(\d+)", report)
    flip = re.search(r"flip_distance=(\d+)", report)
    if header is None or flip is None:
        raise ValueError("incomplete C++ generator report")
    return {"states": int(header.group(1)), "diameter": int(header.group(2)), "flip_distance": int(flip.group(1))}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--n", type=int, default=10)
    parser.add_argument("--generator")
    args = parser.parse_args()
    expected_by_n = {
        10: {"states": 151200, "diameter": 8, "flip_distance": 7},
        11: {"states": 332640, "diameter": 9, "flip_distance": 9},
        12: {"states": 665280, "diameter": 10, "flip_distance": 10},
        13: {"states": 1235520, "diameter": 11, "flip_distance": 11},
        14: {"states": 2162160, "diameter": 12, "flip_distance": 12},
        15: {"states": 3603600, "diameter": 13, "flip_distance": 12},
        16: {"states": 5765760, "diameter": 14, "flip_distance": 13},
        17: {"states": 8910720, "diameter": 15, "flip_distance": 14},
        18: {"states": 13366080, "diameter": 16, "flip_distance": 14},
    }
    if args.n not in expected_by_n:
        parser.error("the independent Python certificate currently requires n=10..18")
    result = exhaustive_bfs(args.n)
    expected = expected_by_n[args.n]
    if any(result[key] != value for key, value in expected.items()):
        raise ValueError(f"unexpected independent BFS result: {result}")
    if args.generator:
        other = generator_summary([args.generator, str(args.n), str(args.n - 6)])
        if other != expected:
            raise ValueError(f"C++ and Python BFS disagree: {other} != {expected}")
        result["cpp_report_verified"] = True
    print(json.dumps(result, sort_keys=True))


if __name__ == "__main__":
    main()
