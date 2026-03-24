package smol

import (
	"encoding/binary"
	"fmt"
	"io"
)

func decompressWithTables(br *bitReader, litLenDecoderMap, distDecoderMap map[int]map[uint32]uint16, litFast []uint32, litK int, distFast []uint32, distK int, window []byte) ([]byte, error) {

	for {
		// Fast decode for lit/len using prefix table when possible
		var litLenCode uint16
		if litK > 0 {
			v, err := br.PeekBits(litK)
			if err != nil {
				return nil, err
			}
			entry := uint32(0)
			if v >= 0 && v < len(litFast) {
				entry = litFast[v]
			}
			sym, l := unpackFastEntry(entry)
			if l != 0 {
				// consume the code bits and use the fast result
				br.ConsumeBits(int(l))
				litLenCode = sym
			} else {
				// fallback to per-bit decode
				val := uint32(0)
				l2 := 0
				for {
					bit, err := br.ReadBit()
					if err != nil {
						return nil, err
					}
					val |= uint32(bit) << uint(l2)
					l2++
					if decoder, ok := litLenDecoderMap[l2]; ok {
						if code, ok2 := decoder[val]; ok2 {
							litLenCode = code
							break
						}
					}
					if l2 > 20 {
						return nil, fmt.Errorf("data corruption: lit/len code too long")
					}
				}
			}
		} else {
			// No fast table available; fallback to per-bit decode
			val := uint32(0)
			l2 := 0
			for {
				bit, err := br.ReadBit()
				if err != nil {
					return nil, err
				}
				val |= uint32(bit) << uint(l2)
				l2++
				if decoder, ok := litLenDecoderMap[l2]; ok {
					if code, ok2 := decoder[val]; ok2 {
						litLenCode = code
						break
					}
				}
				if l2 > 20 {
					return nil, fmt.Errorf("data corruption: lit/len code too long")
				}
			}
		}

		if litLenCode < 256 {
			// Literal byte
			window = append(window, byte(litLenCode))
		} else if litLenCode == 256 {
			// End-of-block
			break
		} else if litLenCode >= 257 && litLenCode <= 285 {
			// Length/distance pair
			lenCodeIndex := litLenCode - 257
			if int(lenCodeIndex) >= len(lenCodes) {
				return nil, fmt.Errorf("data corruption: invalid length code")
			}
			lenInfo := lenCodes[lenCodeIndex]
			lenExtraBits := lenInfo.extraBits
			lenExtraVal := 0
			var err error
			if lenExtraBits > 0 {
				lenExtraVal, err = br.ReadNBits(int(lenExtraBits))
				if err != nil {
					return nil, err
				}
			}
			length := lenInfo.base + uint16(lenExtraVal)

			// Decode distance code using fast table when possible
			var distCode uint16
			if distK > 0 {
				v, err := br.PeekBits(distK)
				if err != nil {
					return nil, err
				}
				entry := uint32(0)
				if v >= 0 && v < len(distFast) {
					entry = distFast[v]
				}
				sym, l := unpackFastEntry(entry)
				if l != 0 {
					br.ConsumeBits(int(l))
					distCode = sym
				} else {
					// fallback
					val := uint32(0)
					l2 := 0
					for {
						bit, err := br.ReadBit()
						if err != nil {
							return nil, err
						}
						val |= uint32(bit) << uint(l2)
						l2++
						if decoder, ok := distDecoderMap[l2]; ok {
							if code, ok2 := decoder[val]; ok2 {
								distCode = code
								break
							}
						}
						if l2 > 20 {
							return nil, fmt.Errorf("data corruption: dist code too long")
						}
					}
				}
			} else {
				val := uint32(0)
				l2 := 0
				for {
					bit, err := br.ReadBit()
					if err != nil {
						return nil, err
					}
					val |= uint32(bit) << uint(l2)
					l2++
					if decoder, ok := distDecoderMap[l2]; ok {
						if code, ok2 := decoder[val]; ok2 {
							distCode = code
							break
						}
					}
					if l2 > 20 {
						return nil, fmt.Errorf("data corruption: dist code too long")
					}
				}
			}

			distCodeIndex := distCode
			if int(distCodeIndex) >= len(distCodes) {
				// Instead of error, treat as invalid and break out of loop
				return nil, fmt.Errorf("data corruption: invalid distance code")
			}
			distInfo := distCodes[distCodeIndex]
			distExtraBits := distInfo.extraBits
			distExtraVal := 0
			if distExtraBits > 0 {
				distExtraVal, err = br.ReadNBits(int(distExtraBits))
				if err != nil {
					return nil, err
				}
			}
			distance := distInfo.base + uint16(distExtraVal)

			// DEFLATE distances must be in [1, len(window)] and allow overlapping copies
			if distance < 1 || int(distance) > len(window) {
				return nil, fmt.Errorf("data corruption: invalid distance %d (window size %d)", distance, len(window))
			}
			start := len(window) - int(distance)
			remaining := int(length)

			// Hot-path: distance == 1 (repeat single byte) — common case and cheap to handle.
			if distance == 1 {
				b := window[len(window)-1]
				add := make([]byte, remaining)
				for i := 0; i < remaining; i++ {
					add[i] = b
				}
				window = append(window, add...)
				continue
			}
			// Fast-path: distance == 2 (two-byte repeating pattern)
			if distance == 2 {
				b0 := window[start]
				b1 := window[start+1]
				add := make([]byte, remaining)
				for i := 0; i < remaining; i++ {
					if i%2 == 0 {
						add[i] = b0
					} else {
						add[i] = b1
					}
				}
				window = append(window, add...)
				continue
			}

			for remaining > 0 {
				chunk := remaining
				if chunk > int(distance) {
					chunk = int(distance)
				}
				// append chunk (handles overlapping copies correctly)
				window = append(window, window[start:start+chunk]...)
				start += chunk
				remaining -= chunk
			}
		} else {
			return nil, fmt.Errorf("data corruption: invalid lit/len code %d", litLenCode)
		}
	}
	return window, nil
}

// Decompress from a reader to a writer.
func Decompress(in io.Reader, out io.Writer) error {
	magic := make([]byte, len(magicNumber))
	if _, err := io.ReadFull(in, magic); err != nil {
		if err == io.EOF {
			return nil
		}
		return fmt.Errorf("reading magic number: %w", err)
	}
	if string(magic) != magicNumber {
		return fmt.Errorf("invalid magic number")
	}

	br := &bitReader{reader: in}
	var window []byte

	for {
		oldLen := len(window)
		finalBit, err := br.ReadBit()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading BFINAL bit: %w", err)
		}

		btypeVal, err := br.ReadNBits(2)
		if err != nil {
			return fmt.Errorf("reading BTYPE: %w", err)
		}
		btype := blockType(btypeVal)

		switch btype {
		case blockStored:
			// Discard remaining bits in current byte to align to the next byte boundary
			br.count = 0
			br.buffer = 0

			var length, nlength uint16
			if err := binary.Read(br.reader, binary.LittleEndian, &length); err != nil {
				return fmt.Errorf("reading stored block length: %w", err)
			}
			if err := binary.Read(br.reader, binary.LittleEndian, &nlength); err != nil {
				return fmt.Errorf("reading stored block nlength: %w", err)
			}

			if length+nlength != 0xFFFF {
				return fmt.Errorf("stored block length mismatch")
			}

			storedData := make([]byte, length)
			if _, err := io.ReadFull(br.reader, storedData); err != nil {
				return fmt.Errorf("reading stored block data: %w", err)
			}
			window = append(window, storedData...)

		case blockStatic:
			window, err = decompressWithTables(br, StaticLitLenDecoderMap, staticDistDecoderMap, staticLitLenFast, staticLitK, staticDistFast, staticDistK, window)
			if err != nil {
				return fmt.Errorf("decompressing static block: %w", err)
			}
		case blockDynamic:
			litLenMap, distMap, litFast, litK, distFast, distK, errTable := parseDynamicTables(br)
			if errTable != nil {
				return fmt.Errorf("parsing dynamic tables: %w", errTable)
			}
			window, err = decompressWithTables(br, litLenMap, distMap, litFast, litK, distFast, distK, window)
			if err != nil {
				return fmt.Errorf("decompressing dynamic block: %w", err)
			}

		default:
			return fmt.Errorf("unknown block type: %d", btype)
		}

		if len(window) > oldLen {
			if _, err := out.Write(window[oldLen:]); err != nil {
				return fmt.Errorf("writing decompressed data: %w", err)
			}
		}

		if len(window) > windowSize {
			newWindow := make([]byte, windowSize)
			copy(newWindow, window[len(window)-windowSize:])
			window = newWindow
		}

		if finalBit == 1 {
			break
		}
	}
	return nil
}
