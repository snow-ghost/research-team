# Server-pinned graph for n=7, k=4

Use this exact graph for `run_bfs`, `search_longest_states`, and `verify_word` in this workspace. The server rejects any other generators.

```json
{
  "degree": 7,
  "generators": [
    {"name": "left0", "permutation": [1, 2, 3, 0, 4, 5, 6]},
    {"name": "left1", "permutation": [0, 2, 3, 4, 1, 5, 6]},
    {"name": "left2", "permutation": [0, 1, 3, 4, 5, 2, 6]},
    {"name": "left3", "permutation": [0, 1, 2, 4, 5, 6, 3]},
    {"name": "left4", "permutation": [0, 1, 2, 3, 5, 6, 4]},
    {"name": "left5", "permutation": [0, 1, 2, 3, 4, 6, 5]},
    {"name": "left6", "permutation": [0, 1, 2, 3, 4, 5, 6]}
  ],
  "start": [0, 1, 2, 3, 4, 5, 6],
  "bidirectional": true,
  "max_states": 6000,
  "max_depth": 8
}
```

The full flip target is `[6,5,4,3,2,1,0]`. A shortest word is `["left0","left1","left6","left0","left5"]`, with distance 5. The start eccentricity is 6. Verify the word independently instead of inventing a general diameter formula.
