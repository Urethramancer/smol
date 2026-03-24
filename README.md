# SMOL

SMOL is a tiny DEFLATE-like compressor made for fun.

## Features
- Small, dependency-free Go codebase.
- Fast prefix Huffman decoder with optional dynamic fast-table cache.
- Builds on TinyGo.
- CLI with three compression levels: `fast`, `normal`, `high`.

## Installation
Build from source (Go TinyGo 1.25+):
```bash
git clone https://github.com/Urethramancer/smol.git
cd smol
go build
```

Or with TinyGo:
```bash
git clone https://github.com/Urethramancer/smol.git
cd smol
go install golang.org/dl/go1.25.8@latest
GOTOOLCHAIN=go1.25.8 go build
```

(Replace the toolchain version with later ones when TinyGo support is available.)

Or use the provided Makefile which supports TinyGo builds:
```bash
go install golang.org/dl/go1.25.8@latest
make build
```

## Usage

Basic usage:
```bash
# Compress
./smol -c infile
```

This produces `infile.smol`. Use the -o flag to specify a different output filename.

```
# Decompress
./smol -d infile.smol -o outfile
```

## Flags and behaviour
- `-c`  : compress
- `-d`  : decompress
- `-o`  : explicit output filename
- Compression levels: `fast`, `normal`, `high` (default: `normal`)

## Exit codes
- `0` : success (including `-v`/version and `-h` help)
- `1` : user/input/usage errors (missing filename, parse errors)
- `2` : runtime/program errors (I/O failure, write errors)

## Development & Benchmarks
- Bench harnesses and microbench tests live in the repository (see `bench_*` test files). Use `go test -bench` or the Makefile bench targets to reproduce timings.
- The code includes a K sweep microbench for tuning the fast-table prefix width (K). The recommended default is `K=12` based on TinyGo and amortized tiny-file tests; see `/tmp` bench artifacts for raw runs when available.

## License
MIT
