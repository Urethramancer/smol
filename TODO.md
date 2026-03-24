# TODO — Compression Improvements (experimental)

Goal: improve compression ratio aggressively (memory usage and CPU secondary). This branch/experiment collects tasks, priorities, and implementation notes.

## High-priority (start these first)

1. Enable / extend the optimal parser
   - Make the optimal parser easily toggleable and exposed via `ParsingOptions`.
   - Ensure the optimal parse cost model includes:
     - Literal/length bit lengths
     - Distance bit lengths
     - Extra bits for length/distance codes
     - Huffman header cost (approximate)
   - Implementation steps:
     - Ensure greedy pass can be re-run or replaced by the optimal pass using the same bit-length cost maps.
     - Expose `ChainDepth`/`MaxProbes` in `ParsingOptions` and pass through to `lz77OptimalParse`.
   - Expected payoff: moderate→high on many inputs.

2. Improve match-finder quality
   - Increase hash table size (e.g., 1<<15 → 1<<17) and make it tunable.
   - Increase chain search depth (e.g., 512 → 4096) and make it tunable.
   - Consider two-position lookahead (lazy-match extension) or multi-position lazy matching.
   - Implementation steps:
     - Replace compile-time constants with package-level vars or parameters read from `ParsingOptions`.
     - Make `lz77GreedyPass` accept a `chainDepth` parameter.
   - Expected payoff: moderate→high. Memory cost: higher.

3. Better block-splitting & decision heuristics
   - Add heuristics to choose block boundaries and whether to use static vs dynamic per-subblock.
   - Quick pre-scan to decide strategy per block (or adaptive small-block fallback).
   - Implementation steps:
     - Add a cheap scanning pass that measures local compressibility and suggests block splits.
     - Expose a knob in `ParsingOptions` to enable "aggressive splitting".
   - Expected payoff: small→moderate but consistent.

## Medium-priority (follow-ups)

- Prototype ANS / range coder (separate format mode) — big gains but large implementation cost.
- Context modelling for literals (order-1 tables or split histograms) — large gains for text.
- Post-transforms (BWT + MTF) — situational gains.

## Low-hanging tweaks

- Tune Huffman RLE heuristics and package-merge tie-breaking.
- Static/shared dictionary for common substrings.
- Distance-code remapping for skewed distances.

## Experiment workflow

1. Pick a representative corpus (you own this; please point me to a directory or list of files).
2. Baseline: measure sizes and times with the current `main` defaults.
3. Implement one change at a time; run the same benchmark.
4. Record size delta and CPU/memory. Roll forward changes that give good ROI.

---

## Current action items (this branch)

- [x] Create this TODO.md and branch `experimental/compression`.
- [x] Start implementing high-priority items 1–3: make chain depth and hash size tunable; wire options through `ParsingOptions`; prepare for block-splitting hooks.
- [ ] Run baseline corpus benchmarks.
- [ ] Iterate on match-finder and optimal parser tuning.

---

Notes:
- This branch intentionally prefers compression gains over memory or binary size.
- If you want TinyGo-friendly variants, we can gate them behind build tags or separate flags.

If this looks good, I will commit these changes locally on `experimental/compression`. Do you want me to push the branch to origin as well?