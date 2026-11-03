#!/usr/bin/env python3
import argparse
import json
import re
import subprocess


def search(generator, n, r):
    completed = subprocess.run(
        [generator, str(n), str(n - r)],
        check=True,
        capture_output=True,
        text=True,
    )
    summary = re.search(r"states=(\d+) eccentricity=(\d+)", completed.stdout)
    flip = re.search(r"flip_distance=(\d+)", completed.stdout)
    if summary is None or flip is None:
        raise RuntimeError("incomplete generator report")
    return {
        "n": n,
        "r": r,
        "copies": n - r,
        "states": int(summary.group(1)),
        "diameter": int(summary.group(2)),
        "flip_distance": int(flip.group(1)),
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--generator", default=".research-server/bin/repeated-bfs-fast")
    parser.add_argument("--output")
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--max-n", type=int, default=16)
    parser.add_argument("--r-values", default="1,2,3,4,5")
    args = parser.parse_args()
    r_values = [int(value) for value in args.r_values.split(",")]
    rows = [search(args.generator, n, r) for r in r_values for n in range(max(8, r + 3), args.max_n + 1)]
    if args.check:
        for row in rows:
            n, r = row["n"], row["r"]
            expected_states = 1
            for value in range(n - r + 1, n + 1):
                expected_states *= value
            assert row["states"] == expected_states, row
            if r == 1:
                expected = n // 6 + 1 + (1 if n % 6 == 4 else 0)
                assert row["diameter"] == expected, row
                assert row["flip_distance"] == 1, row
            if r == 2:
                assert row["diameter"] == (n + 2) // 3, row
                assert row["flip_distance"] == 3, row
            if r == 3:
                assert row["diameter"] == n // 2 + (1 if n % 6 in (1, 5) else 0), row
            if r == 4:
                assert row["diameter"] == (2 * n + 2) // 3 - 1 + (1 if n % 6 == 0 else 0), row
            if r == 5:
                expected = 6 if n == 9 else n - n // 5 - 1 + (1 if n in (20, 25, 26, 30, 31, 32) else 0)
                assert row["diameter"] == expected, row
    body = json.dumps(rows, ensure_ascii=False, indent=2) + "\n"
    if args.output:
        with open(args.output, "w") as output:
            output.write(body)
    else:
        print(body, end="")


if __name__ == "__main__":
    main()
