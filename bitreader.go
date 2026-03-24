package smol

import (
	"bufio"
	"io"
)

type bitReader struct {
	buffer uint64
	count  uint8
	reader io.Reader
}

// ensureBits fills the internal buffer until at least n bits are available.
// Treat io.EOF as a soft condition: if no more bytes are available we allow
// parsing to proceed with the bits already buffered (missing high bits are
// effectively zero). This avoids spurious EOF when peeking past the last
// partial byte in a stream.
func (br *bitReader) ensureBits(n int) error {
	for br.count < uint8(n) {
		if rb, ok := br.reader.(io.ByteReader); ok {
			b, err := rb.ReadByte()
			if err != nil {
				if err == io.EOF {
					// No more bytes; stop attempting to read further and let caller
					// use whatever bits are already buffered.
					return nil
				}
				return err
			}
			br.buffer |= uint64(b) << br.count
			br.count += 8
		} else {
			b := make([]byte, 1)
			n, err := br.reader.Read(b)
			if err != nil {
				if err == io.EOF {
					if n > 0 {
						// Read may return EOF with n==0; if we did receive a byte (n>0),
						// incorporate it; otherwise treat EOF as soft and return.
						br.buffer |= uint64(b[0]) << br.count
						br.count += 8
						// continue the loop; if still short, next iteration will hit EOF again and return nil
						continue
					}
					// No bytes read and EOF: return nil to let caller use buffered bits
					return nil
				}
				return err
			}
			// incorporate the byte(s) read
			if n > 0 {
				br.buffer |= uint64(b[0]) << br.count
				br.count += 8
			}
		}
	}
	return nil
}

// PeekBits returns the next n bits LSB-first without consuming them.
func (br *bitReader) PeekBits(n int) (int, error) {
	if n <= 0 {
		return 0, nil
	}
	if err := br.ensureBits(n); err != nil {
		return 0, err
	}
	mask := uint64((1 << uint(n)) - 1)
	return int(br.buffer & mask), nil
}

// ConsumeBits drops n bits from the buffer, assuming they are already present.
func (br *bitReader) ConsumeBits(n int) {
	if n <= 0 {
		return
	}
	br.buffer >>= uint(n)
	br.count -= uint8(n)
}

// ReadNBits reads n bits LSB-first and consumes them.
func (br *bitReader) ReadNBits(n int) (int, error) {
	v, err := br.PeekBits(n)
	if err != nil {
		return 0, err
	}
	br.ConsumeBits(n)
	return v, nil
}

// ReadBit reads a single bit LSB-first and consumes it.
func (br *bitReader) ReadBit() (int, error) {
	if err := br.ensureBits(1); err != nil {
		return 0, err
	}
	bit := int(br.buffer & 1)
	br.ConsumeBits(1)
	return bit, nil
}

// Helper to create a bitReader backed by a buffered reader.
func newBitReader(r io.Reader) *bitReader {
	return &bitReader{reader: bufio.NewReader(r)}
}
