# Wrapped consecutive 4-cycles

Verify the reflection-length proof for inverse-closed wrapped consecutive 4-cycles. Formalize the compact minimum/parity formula, circular rounding, finite certificates and six-point interval recursion. The optional conjugation identity in the source proof is false and is not part of the formal statement. Determine graph diameter separately from reflection eccentricity, including the case where one final symbol is repeated.

## Verified finite counterexample

For `n = 7`, the full flip is not a diameter witness. Bounded BFS explored all 5040 permutations and returned start eccentricity 6; the Cayley graph is vertex-transitive, so this is the graph diameter. The full flip has distance 5 with word `[left 0, left 1, left 6, left 0, left 5]`.

`fullFlip7Ops_correct` verifies that exact word with the wrapped operations and kernel `decide`; it has no axioms. `fullFlip7Ops_length` verifies its length. These results do not prove a general diameter formula, including repeated-symbol cases. Older BFS tables are untrusted source material, not certificates.

## Exact binary case

For one unique symbol and `n - 1` repetitions (`d = n - 1`), the diameter is `1, 2, 2` for `n = 5, 6, 7`. For `n ≥ 8`, write `n = 6q + r` with `0 ≤ r < 6`; the diameter is `q + 1 + 1` when `r = 4`, and `q + 1` otherwise.

The state is determined by the unique symbol's position. A move changes it by `+3` or `−1`; an inverse move changes it by `−3` or `+1`. With `rep(x) = floor(x/3) + x mod 3`, the distance to displacement `d` is `min(rep(d), rep(n-d))`. `repeated_cost_upper_all` proves the upper bound for every displacement, and `repeated_cost_witness` constructs an attaining displacement for each residue class. `repeated_cost_base` verifies the three base cases. This proves only the binary case, not a formula for arbitrary repeated symbols.

## Boundary for two copies

The simple conjecture `D(n,2) = n-2` is false. Independent BFS on 1,814,400 states at `n = 10, d = 2` gives graph diameter 9 while the full flip has distance 8. This is a counterexample to both a universal linear formula and to treating every full flip as a diameter witness. No general diameter formula for `d = 2` is claimed.

The two constructions are independently pinned in Go and Lean. Starting from `[0,1,2,3,4,5,6,7,8,8]`, the full flip has shortest word `[L1,R2,R8,L5,L3,R1,R9,R2]`; `twoCopies10FlipOps` contains the same zero-based `Op` list. Its length is 8 and exhaustive BFS establishes minimality. A farther state is `[8,7,8,6,5,4,3,2,1,0]`, reached by the nine operations in `twoCopies10FarOps`. The Lean theorems use kernel `decide` and prove reachability and lengths; the exhaustive BFS, not those finite Lean theorems alone, supplies the diameter and shortest-distance conclusions.
