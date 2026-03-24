package smol_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	smol "github.com/Urethramancer/smol"
)

func TestAmortizedCacheBasic(t *testing.T) {
	// Basic smoke test to exercise the exported cache APIs.
	smol.SetFastTableCacheCapacity(8)
	h, m, size := smol.FastTableCacheStats()
	if h != 0 || m != 0 {
		t.Fatalf("expected zero stats on fresh cache, got hits=%d misses=%d", h, m)
	}
	if size < 0 || size > 8 {
		t.Fatalf("unexpected cache size %d", size)
	}
	// Resize downwards and ensure size does not exceed new capacity
	smol.SetFastTableCacheCapacity(2)
	_, _, newSize := smol.FastTableCacheStats()
	if newSize > 2 {
		t.Fatalf("cache size %d exceeds reduced capacity 2", newSize)
	}
}

// This test is a microbenchmark that measures buildFastTable call cost for different K.
// It writes results to stdout in CSV form: K,iter,duration_ns
func TestBuildFastTableMicro(t *testing.T) {
	Ks := []int{9, 10, 11, 12, 13}
	iters := 2000
	// Ensure statics are initialized (init funcs run before tests)
	for _, K := range Ks {
		durations := make([]int64, 0, iters)
		for i := 0; i < iters; i++ {
			now := time.Now()
			_, _ = smol.BuildFastTable(smol.StaticLitLenDecoderMap, K)
			durations = append(durations, time.Since(now).Nanoseconds())
		}
		// compute mean
		var sum int64
		for _, v := range durations {
			sum += v
		}
		mean := float64(sum) / float64(len(durations))
		fmt.Printf("buildfast,K=%d,iters=%d,mean_ns=%.2f\n", K, iters, mean)
	}
}

const (
	testCompressFile   = "compress.go"
	testDecompressFile = "compress.go.smol"
)

func TestCompress(t *testing.T) {
	smol.SetFastTableCacheCapacity(64)
	f, err := os.Open(testCompressFile)
	if err != nil {
		t.Fatalf("open %s: %v", testCompressFile, err)
	}
	defer f.Close()

	out, err := os.OpenFile(testDecompressFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("open %s: %v", testDecompressFile, err)
	}
	defer out.Close()

	start := time.Now()
	po := smol.ParsingOptions{
		UseOptimal: true,
		ChainDepth: 512,
	}
	if err := smol.Compress(f, out, po); err != nil {
		t.Fatalf("compress failed: %v", err)
	}
	dur := time.Since(start)
	t.Logf("compress, duration_s=%.6f", dur.Seconds())
}

func TestDecompressy(t *testing.T) {
	smol.SetFastTableCacheCapacity(64)
	if _, err := os.Stat(testDecompressFile); err != nil {
		if os.IsNotExist(err) {
			t.Skip(testDecompressFile + " not present; skipping integration bench")
		}
		t.Fatalf("stat %s: %v", testDecompressFile, err)
	}
	f, err := os.Open(testDecompressFile)
	if err != nil {
		t.Fatalf("open %s: %v", testDecompressFile, err)
	}
	defer f.Close()
	start := time.Now()
	if err := smol.Decompress(f, io.Discard); err != nil {
		t.Fatalf("decompress failed: %v", err)
	}
	dur := time.Since(start)
	t.Logf("decompress, duration_s=%.6f", dur.Seconds())
}

func TestFastTableCacheCapacity(t *testing.T) {
	// Set a small capacity and verify stats/reporting behave sensibly.
	smol.SetFastTableCacheCapacity(2)
	hits, misses, size := smol.FastTableCacheStats()
	if hits != 0 || misses != 0 {
		t.Fatalf("expected 0 hits/misses after resetting cache, got hits=%d misses=%d", hits, misses)
	}
	if size < 0 || size > 2 {
		t.Fatalf("unexpected cache size %d (expected 0..2)", size)
	}

	// Increase capacity and ensure reported size does not exceed the new capacity.
	smol.SetFastTableCacheCapacity(4)
	_, _, size2 := smol.FastTableCacheStats()
	if size2 > 4 {
		t.Fatalf("cache size %d exceeds capacity 4", size2)
	}
}

func TestTinyRoundtrip(t *testing.T) {
	cases := [][]byte{}
	for n := 1; n <= 32; n++ {
		b := make([]byte, n)
		for i := 0; i < n; i++ {
			b[i] = byte((i + n) & 0xff)
		}
		cases = append(cases, b)
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("len=%d", len(c)), func(t *testing.T) {
			var comp bytes.Buffer
			po := smol.ParsingOptions{
				UseOptimal: true,
				ChainDepth: 512,
			}
			if err := smol.Compress(bytes.NewReader(c), &comp, po); err != nil {
				t.Fatalf("Compress failed for len=%d: %v", len(c), err)
			}

			// attempt to decompress
			var out bytes.Buffer
			if err := smol.Decompress(bytes.NewReader(comp.Bytes()), &out); err != nil {
				t.Fatalf("Decompress failed for len=%d: %v\ncompressed hex: %x", len(c), err, comp.Bytes())
			}
			if !bytes.Equal(c, out.Bytes()) {
				t.Fatalf("roundtrip mismatch len=%d\norig: %x\nrecv: %x", len(c), c, out.Bytes())
			}
		})
	}
}
