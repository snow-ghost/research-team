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
    args = parser.parse_args()
    rows = [search(args.generator, n, r) for r in range(1, 6) for n in range(max(8, r + 3), 17)]
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
    body = json.dumps(rows, ensure_ascii=False, indent=2) + "\n"
    if args.output:
        with open(args.output, "w") as output:
            output.write(body)
    else:
        print(body, end="")


if __name__ == "__main__":
    main()
