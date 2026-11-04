#!/usr/bin/env python3
import argparse

from near_binary_witness import apply_op


def circular_distance(a, b, n):
    difference = abs(a - b)
    return min(difference, n - difference)


def target(n):
    return [6] * (n - 7) + [5, 4, 3, 2, 1, 0, 6]


def inversions(state):
    order = [value for value in state if value < 6]
    rank = {value: 5 - value for value in range(6)}
    return sum(rank[order[i]] > rank[order[j]] for i in range(6) for j in range(i + 1, 6))


def linear_two(state, n):
    goal = target(n)
    return sum(abs(state.index(value) - goal.index(value)) for value in range(6)) + 2 * inversions(state)


def circular(state, n):
    goal = target(n)
    return sum(circular_distance(state.index(value), goal.index(value), n) for value in range(6))


def background_j(state, n):
    goal = target(n)
    score = circular(state, n)
    actual = [i for i, value in enumerate(state) if value == 6]
    expected = [i for i, value in enumerate(goal) if value == 6]
    best = None
    for k in range(len(expected)):
        shifted = expected[k:] + expected[:k]
        value = sum(circular_distance(x, y, n) for x, y in zip(actual, shifted))
        best = value if best is None else min(best, value)
    return score + best + inversions(state)


POTENTIALS = {
    "linear-two": linear_two,
    "circular": circular,
    "background-j": background_j,
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--exhaustive-n", type=int, choices=(10,))
    args = parser.parse_args()
    if args.exhaustive_n:
        import itertools
        n = args.exhaustive_n
        maxima = {name: 0 for name in POTENTIALS}
        for positions in itertools.combinations(range(n), 6):
            for order in itertools.permutations(range(6)):
                state = [6] * n
                for position, value in zip(positions, order):
                    state[position] = value
                old = {name: function(state, n) for name, function in POTENTIALS.items()}
                for i in range(n):
                    for direction in ("left", "right"):
                        new_state = apply_op(state, {"direction": direction, "index": i})
                        for name, function in POTENTIALS.items():
                            maxima[name] = max(maxima[name], old[name] - function(new_state, n))
        print(maxima)
        if maxima != {"linear-two": 30, "circular": 6, "background-j": 15}:
            raise ValueError("unexpected potential maxima")
        return
    examples = [
        ("linear-two", [0, 6, 6, 1, 2, 3, 4, 6, 6, 5], 9, 30),
        ("circular", [1, 2, 3, 0, 5, 4, 6, 6, 6, 6], 2, 6),
        ("background-j", [0, 6, 6, 1, 2, 6, 3, 4, 6, 5], 9, 15),
    ]
    for name, state, index, expected_drop in examples:
        function = POTENTIALS[name]
        new_state = apply_op(state, {"direction": "left", "index": index})
        drop = function(state, 10) - function(new_state, 10)
        if drop != expected_drop:
            raise ValueError(f"{name}: drop {drop} != {expected_drop}")
        print({"potential": name, "drop": drop})
    for n in (10, 11, 20):
        state = list(range(6)) + [6] * (n - 6)
        print({"n": n, "initial": {name: function(state, n) for name, function in POTENTIALS.items()}})


if __name__ == "__main__":
    main()
