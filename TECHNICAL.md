# SMOL — Technical Overview

**Last updated:** 2026-03-24
A concise, developer-oriented description of the algorithm implemented in this repository and a guide to what each source file does. SMOL implements a compact DEFLATE-like compressor/decompressor optimized for small runtime size and TinyGo compatibility, with an optional optimal parser for better compression at higher CPU cost.

---

## Table of contents
- [Overview](#overview)
- [Block format (conceptual)](#block-format-conceptual)
- [Bit-layout appendix](#bit-layout-appendix)
- [Core algorithm details](#core-algorithm-details)
- [Files and responsibilities](#files-and-responsibilities)
- [Format notes & edge cases](#format-notes--edge-cases)
- [Performance & trade-offs](#performance--trade-offs)
- [Build & test](#build--test)
- [Next steps / suggestions](#next-steps--suggestions)

---

## Overview
- Type: DEFLATE-inspired stream format (LZ77 + Huffman).
- Goals: small code size, fast decode, TinyGo friendliness, optional optimal parsing for compression quality.
- Stream header: 4-byte magic `SMOL` at the start of every stream.

High-level flow:
1. Read input in 32 KiB blocks.
2. For each block: run LZ77 parse (greedy, optional optimal), produce literal/length and distance symbols.
3. Build static or dynamic Huffman tables; choose whichever yields smaller encoded size.
4. Emit block header + compressed payload.

---

## Block format (conceptual)
Bits are emitted LSB-first unless otherwise noted.
- `BFINAL` — 1 bit (1 if this is the final block)
- `BTYPE` — 2 bits
  - `00` = stored (uncompressed)
  - `01` = static Huffman
  - `10` = dynamic Huffman

Dynamic block header (DEFLATE-like):
- `HLIT`: 5 bits (`number of literal/length codes - 257`)
- `HDIST`: 5 bits (`number of distance codes - 1`)
- `HCLEN`: 4 bits (`number of code length codes - 4`)
- `HCLEN` entries: `(HCLEN_total) × 3 bits` (bit-lengths for the code-length alphabet)
- Followed by RLE of literal/length and distance code lengths, then canonical codes, then compressed payload.

> Note: dynamic headers are RLE-encoded (symbols `0–18`) following DEFLATE conventions (`16`=repeat prev, `17/18`=repeat zeros).

---

## Bit-layout appendix
This appendix gives exact bit offsets and wire order for the dynamic header fields. All fields are encoded LSB-first (least-significant bit first on the stream). Offsets below are relative to the start of the dynamic header immediately after `BFINAL` and `BTYPE`.

### Field offsets (bit indices)
| Field  | Width (bits) | Wire value | Encoded range | Bit offsets (inclusive..exclusive) |
|--------|--------------:|:----------:|:--------------|:----------------------------------:|
| HLIT   | 5             | HLIT_total - 257 | 0..29 → 257..286 | [0..5)  |
| HDIST  | 5             | HDIST_total - 1  | 0..31 → 1..32   | [5..10) |
| HCLEN  | 4             | HCLEN_total - 4  | 0..15 → 4..19   | [10..14)|
| HCLEN entries | 3 × HCLEN_total | code-length bit-lengths | 0..7 per entry | [14..(14+3*HCLEN_total)) |

> Reading/writing is done LSB-first via the project's `bitReader`/`bitWriter` helpers.

### HCLEN entry wire order
The HCLEN entries are present in the wire in the following order (this matches DEFLATE):

```text
codeLenOrder = [16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15]
```

Only the first `HCLEN_total` entries appear on the wire; the remaining symbols are implicitly zero.

### Example (human-readable)
If the header bits (LSB-first per field) decode as:

- `HLIT` = `00011` (value 3) → `HLIT_total = 260` (describe codes `0..259`)
- `HDIST` = `00000` (value 0) → `HDIST_total = 1` (single distance code)
- `HCLEN` = `0010`  (value 2) → `HCLEN_total = 6` → read 6 × 3 = 18 bits for the HCLEN entries in the `codeLenOrder` above.

After HCLEN entries, decode the RLE-encoded literal/length lengths (`HLIT_total + 257` entries) followed by `HDIST_total + 1` distance lengths.

---

## Core algorithm details

### LZ77
- Sliding window: **32 KiB** (32768)
- Min match: **3**, max match: **258**
- Hashing: 3-byte rolling hash into 15-bit table (`1<<15`) with chaining (`head`/`prev` arrays)
- Parsers:
  - `lz77GreedyPass` — single-pass greedy with lazy matching (lookahead = 1)
  - `lz77OptimalParse` — backward DP using symbol bit-length costs (higher CPU/memory)

### Huffman
- Two-phase length-limited builder: build unconstrained Huffman tree → fold long codes upward to meet max code length (zlib/PNG technique).
- Canonical codes generated and stored as `{symbol → codeVal}` and decoder maps per code length.

### Fast tables
- K-bit prefix tables accelerate decoding (single lookup returns symbol + length when code length ≤ K).
- Tables packed into `uint32` entries for cache-friendly lookups.
- LRU cache keyed by hex encoding of literal/length and distance length arrays avoids repeated rebuilds.

### Bit I/O
- LSB-first `bitWriter` / `bitReader`.
- `bitReader.ensureBits` treats `io.EOF` softly so peeking slightly past the final partial byte won't spuriously fail decoding.

---

## Files and responsibilities
A compact reference table and example usage for each major file.

| File | Responsibility | Example entrypoint |
|------|---------------|--------------------|
| `cmd/smol/main.go` | CLI: flags `-c/-d/-o/-level`; tolerant `-o` parsing | `smol -c infile -o outfile` |
| `compress.go` | Top-level compressor (`Compress(in, out, po)`) | Called by CLI or tests |
| `decompress.go` | Top-level decompressor (`Decompress(in, out)`) | Called by CLI or tests |
| `deflate.go` | Static tables, `getLengthCode`, `getDistCode` | Internal helpers |
| `lz77.go` | LZ77 matchers & parsers | `lz77GreedyPass` / `lz77OptimalParse` |
| `huff.go` | Huffman tree + canonical code builder | `BuildFastTable` |
| `bitwriter.go` / `bitreader.go` | LSB-first bit I/O primitives | Used by compressor & decompressor |
| `dynamic_cache.go` | LRU cache for fastTables; test helpers | `SetFastTableCacheCapacity` |

---

## Format notes & edge cases
- Stream begins with ASCII `SMOL` (4 bytes).
- Stored blocks align to the next byte boundary; dynamic/static blocks use LSB-first Huffman-coded payloads.
- Overlapping matches follow DEFLATE semantics; special fast-paths exist for `distance==1` and `distance==2`.
- Decoder treats `io.EOF` softly when peeking bits.

---

## Performance & trade-offs
- Fast decode via prefix tables and caching (memory ↔ speed trade-off).
- Optional optimal parsing improves compression at the cost of CPU/memory during parse.
- TinyGo-friendly: avoids heavy runtime dependencies; aims for portability to TinyGo targets.
