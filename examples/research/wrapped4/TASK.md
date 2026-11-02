# Wrapped consecutive 4-cycles

Verify the reflection-length proof for inverse-closed wrapped consecutive 4-cycles. Formalize the compact minimum/parity formula, circular rounding, finite certificates and six-point interval recursion. The optional conjugation identity in the source proof is false and is not part of the formal statement. Determine graph diameter separately from reflection eccentricity, including the case where one final symbol is repeated.

## Vertex transitivity for repeated symbols

Fix a multiset of symbols and let its states be all arrangements of that multiset. The symmetric group on the symbols acts on every arrangement by relabeling each entry. This action is transitive: to map one arrangement to another, match the positions of equal symbols and choose a permutation that sends each symbol to its required symbol. Relabeling commutes with every wrapped move because a move changes positions, not symbols. Consequently, relabeling is a graph automorphism, every state has the same eccentricity, and the eccentricity of the canonical start is the diameter of the whole fixed-multiset component.

The commutation part is formalized without additional axioms in `leftWrapped4_map`, `rightWrapped4_map` and `applyWrappedOps_map`: an arbitrary function on symbols commutes with each move and with every finite word. The transitivity of the relabeling action is the elementary matching argument above. Thus the exhaustive fixed-start computations below also determine graph diameter; no separate vertex-transitivity assumption is needed.

## Verified finite counterexample

For `n = 7`, the full flip is not a diameter witness. Bounded BFS explored all 5040 permutations and returned start eccentricity 6; the Cayley graph is vertex-transitive, so this is the graph diameter. The full flip has distance 5 with word `[left 0, left 1, left 6, left 0, left 5]`.

`fullFlip7Ops_correct` verifies that exact word with the wrapped operations and kernel `decide`; it has no axioms. `fullFlip7Ops_length` verifies its length. These results do not prove a general diameter formula, including repeated-symbol cases. Older BFS tables are untrusted source material, not certificates.

## Exact binary case

For one unique symbol and `n - 1` repetitions (`d = n - 1`), the diameter is `1, 2, 2` for `n = 5, 6, 7`. For `n ≥ 8`, write `n = 6q + r` with `0 ≤ r < 6`; the diameter is `q + 1 + 1` when `r = 4`, and `q + 1` otherwise.

The state is determined by the unique symbol's position. A move changes it by `+3` or `−1`; an inverse move changes it by `−3` or `+1`. With `rep(x) = floor(x/3) + x mod 3`, the distance to displacement `d` is `min(rep(d), rep(n-d))`. `repeated_cost_upper_all` proves the upper bound for every displacement, and `repeated_cost_witness` constructs an attaining displacement for each residue class. `repeated_cost_base` verifies the three base cases. This proves only the binary case, not a formula for arbitrary repeated symbols.

## Boundary for two copies

The simple conjecture `D(n,2) = n-2` is false. Independent BFS on 1,814,400 states at `n = 10, d = 2` gives graph diameter 9 while the full flip has distance 8. This is a counterexample to both a universal linear formula and to treating every full flip as a diameter witness. No general diameter formula for `d = 2` is claimed.

The two constructions are independently pinned in Go and Lean. Starting from `[0,1,2,3,4,5,6,7,8,8]`, the full flip has shortest word `[L1,R2,R8,L5,L3,R1,R9,R2]`; `twoCopies10FlipOps` contains the same zero-based `Op` list. Its length is 8 and exhaustive BFS establishes minimality. A farther state is `[8,7,8,6,5,4,3,2,1,0]`, reached by the nine operations in `twoCopies10FarOps`. The Lean theorems use kernel `decide` and prove reachability and lengths; the exhaustive BFS, not those finite Lean theorems alone, supplies the eccentricity and shortest-distance conclusions.

## Exact eleven-position computations

The offline command exhausts the complete fixed-multiset component and writes farthest-state and flip certificates. It uses a compact open-addressed index and does not change the 20,000-state server limit.

```sh
go run ./cmd/research-graph-search -degree 11 -copies 2 -out /private/new-repeated11.json
```

At `n = 11`, independent C++ and Go implementations agree on the following exact values:

| Copies `d` | Explored states | Graph diameter | Full-flip distance |
| --- | ---: | ---: | ---: |
| 2 | 19,958,400 | 10 | 10 |
| 3 | 6,652,800 | 10 | 9 |
| 4 | 1,663,200 | 9 | 9 |

For `d = 2`, the full flip and the distinct state `[4,3,2,1,0,9,9,8,7,6,5]` are both diameter witnesses. Their ten-operation words are pinned as `twoCopies11FlipOps` and `twoCopies11FarOps` and checked independently in Go and Lean. The Lean theorems prove only the constructions; exhaustive search supplies minimality and coverage. These finite results do not establish a general diameter formula for repeated symbols.
