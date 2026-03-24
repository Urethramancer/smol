package smol

import "io"

// --- Bit-level I/O (LSB-first) ---
type bitWriter struct {
	buffer byte
	count  uint8
	writer io.Writer
}

// WriteBits writes nbits of val LSB-first.
func (bw *bitWriter) WriteBits(val uint, nbits int) {
	for i := 0; i < nbits; i++ {
		bit := byte((val >> i) & 1)
		bw.buffer |= bit << bw.count
		bw.count++
		if bw.count == 8 {
			bw.writer.Write([]byte{bw.buffer})
			bw.buffer = 0
			bw.count = 0
		}
	}
}

// WriteString writes a string of '0'/'1' bits LSB-first (first rune is the least-significant bit).
func (bw *bitWriter) WriteString(bits string) {
	for _, bit := range bits {
		var b byte
		if bit == '1' {
			b = 1
		} else {
			b = 0
		}
		bw.buffer |= b << bw.count
		bw.count++
		if bw.count == 8 {
			bw.writer.Write([]byte{bw.buffer})
			bw.buffer = 0
			bw.count = 0
		}
	}
}

func (bw *bitWriter) Flush() {
	if bw.count > 0 {
		// remaining bits are already in the low bits of buffer; write them
		bw.writer.Write([]byte{bw.buffer})
		bw.buffer = 0
		bw.count = 0
	}
}
