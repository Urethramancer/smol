package smol

// --- LZ77 Implementation with Hashing---
var (
	windowSize   = 32768
	minMatch     = 3
	maxMatch     = 258
	hashBits     = 15
	hashSize     = 1 << hashBits
	defaultChain = 512
)

func updateHash(b []byte) uint32 {
	val := uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2])
	return (val * 2654435761) >> (32 - hashBits)
}

type lz77Match struct{ Length, Distance int }

// --- ✅ FIX: Correct Single-Pass Greedy Parser ---
// This function implements a standard, high-performance greedy parser that
// correctly interleaves hashing and match finding in a single pass. This resolves
// the stream corruption bug caused by the previous two-pass implementation.
func lz77GreedyPass(data []byte, offset int, chainDepth int) []any {
	var output []any
	if offset >= len(data) {
		return output
	}

	head := make([]int, hashSize)
	prev := make([]int, len(data))
	for i := range head {
		head[i] = -1
	}

	findBestMatch := func(pos int, chainHead int) (length, distance int) {
		bestLen, bestDist := 0, 0
		chainPos := chainHead

		limit := pos - windowSize
		if limit < 0 {
			limit = 0
		}

		probes := 0

		for chainPos != -1 && chainPos >= limit && probes < chainDepth {
			// Quick 3-byte check to avoid expensive comparisons
			// Safe because we only insert positions where at least minMatch bytes remain.
			if data[chainPos] == data[pos] &&
				data[chainPos+1] == data[pos+1] &&
				data[chainPos+2] == data[pos+2] {

				l := minMatch
				// Compute upper bound once
				maxL := maxMatch
				if rem := len(data) - pos; rem < maxL {
					maxL = rem
				}
				for l < maxL && data[chainPos+l] == data[pos+l] {
					l++
				}
				if l > bestLen {
					bestLen, bestDist = l, pos-chainPos
					if bestLen == maxMatch {
						break
					}
				}
			}
			chainPos = prev[chainPos]
			probes++
		}
		return bestLen, bestDist
	}

	i := 0
	for i < len(data) {
		// --- Main Loop Logic ---
		if i+minMatch > len(data) {
			break // Go to cleanup loop
		}

		// 1. Insert current position into hash table to make it available for future searches.
		h := updateHash(data[i : i+3])
		chainHead := head[h]
		prev[i] = chainHead
		head[h] = i

		// 2. If we are in the historical window, we only update the hash table.
		// No output is generated for this part.
		if i < offset {
			i++
			continue
		}

		// 3. Find the best match for the current position.
		length, distance := findBestMatch(i, chainHead)

		// 4. Lazy Matching: Check if a better match exists at the next position.
		if i+1+minMatch <= len(data) {
			nextH := updateHash(data[i+1 : i+1+3])
			nextChainHead := head[nextH]
			nextLength, _ := findBestMatch(i+1, nextChainHead)

			if nextLength > length {
				// If the next match is better, emit the current byte as a literal
				// so we can catch the better match on the next iteration.
				output = append(output, uint16(data[i]))
				i++
				continue
			}
		}

		// 5. Output either the match or a literal.
		if length >= minMatch {
			output = append(output, lz77Match{Length: length, Distance: distance})
			// Update the hash table for all the bytes we are skipping over.
			for j := 1; j < length; j++ {
				pos := i + j
				if pos+minMatch <= len(data) {
					hj := updateHash(data[pos : pos+3])
					prev[pos] = head[hj]
					head[hj] = pos
				}
			}
			i += length
		} else {
			output = append(output, uint16(data[i]))
			i++
		}
	}

	// Add any remaining bytes at the end of the block as literals.
	for ; i < len(data); i++ {
		if i >= offset {
			output = append(output, uint16(data[i]))
		}
	}
	return output
}

// lz77OptimalParse performs a dynamic-programming optimal parsing using the
// provided bit-length cost maps for literal/length and distance symbols.
// llLenMap: map from literal/length symbol -> bit length
// dLenMap: map from distance symbol -> bit length
// maxProbes: maximum chain probes when searching for matches (controls CPU)
func lz77OptimalParse(data []byte, offset int, llLenMap map[uint16]int, dLenMap map[uint16]int, maxProbes int) []any {
	n := len(data)
	if offset >= n {
		return nil
	}

	// Prebuild head/prev for match finding across the buffer.
	head := make([]int, hashSize)
	prev := make([]int, n)
	for i := range head {
		head[i] = -1
	}
	for i := 0; i+minMatch <= n; i++ {
		h := updateHash(data[i : i+3])
		prev[i] = head[h]
		head[h] = i
	}

	// Large initial cost
	const INF = int(1 << 60)
	cost := make([]int, n+1)
	for i := range cost {
		cost[i] = INF
	}
	cost[n] = 0

	type choice struct {
		isLiteral bool
		lit       byte
		matchLen  int
		matchDist int
	}

	choices := make([]choice, n)

	// Helper to get bit-length for a symbol, fallback to 8 bits if unknown.
	getLen := func(sym uint16, m map[uint16]int) int {
		if v, ok := m[sym]; ok && v > 0 {
			return v
		}
		// fallback conservative estimate
		return 8
	}

	// DP from end backwards
	for pos := n - 1; pos >= offset; pos-- {
		// Option 1: literal
		litSym := uint16(data[pos])
		litCost := getLen(litSym, llLenMap)
		bestCost := litCost + cost[pos+1]
		bestChoice := choice{isLiteral: true, lit: data[pos]}

		// Option 2: try matches found in the hash chain
		h := -1
		if pos+minMatch <= n {
			h = int(updateHash(data[pos : pos+3]))
		}
		if h != -1 {
			chainPos := head[h]
			probes := 0
			limit := pos - windowSize
			if limit < 0 {
				limit = 0
			}
			for chainPos != -1 && chainPos >= limit && probes < maxProbes {
				if chainPos < pos {
					// quick check
					if data[chainPos] == data[pos] && data[chainPos+1] == data[pos+1] && data[chainPos+2] == data[pos+2] {
						// determine match length
						l := minMatch
						maxL := maxMatch
						if rem := n - pos; rem < maxL {
							maxL = rem
						}
						for l < maxL && data[chainPos+l] == data[pos+l] {
							l++
						}
						// compute code/bit costs for this match
						lenSymbol, _, lenExtraBits := getLengthCode(uint16(l))
						distSymbol, _, distExtraBits := getDistCode(uint16(pos - chainPos))
						matchCost := getLen(lenSymbol, llLenMap) + int(lenExtraBits)
						if pos-chainPos > 0 {
							matchCost += getLen(distSymbol, dLenMap) + int(distExtraBits)
						}
						if pos+l <= n {
							totalCost := matchCost + cost[pos+l]
							if totalCost < bestCost {
								bestCost = totalCost
								bestChoice = choice{isLiteral: false, matchLen: l, matchDist: pos - chainPos}
							}
						}
					}
				}
				chainPos = prev[chainPos]
				probes++
			}
		}

		cost[pos] = bestCost
		choices[pos] = bestChoice
	}

	// Reconstruct output from choices
	var out []any
	pos := offset
	for pos < n {
		c := choices[pos]
		if c.isLiteral {
			out = append(out, uint16(c.lit))
			pos++
		} else {
			out = append(out, lz77Match{Length: c.matchLen, Distance: c.matchDist})
			pos += c.matchLen
		}
	}
	// Append EOB
	out = append(out, uint16(256))
	return out
}
