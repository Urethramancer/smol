package smol

import (
	"bufio"
	"fmt"
	"io"
)

// ParsingOptions for compression levels.
type ParsingOptions struct {
	UseOptimal bool
	ChainDepth int
}

// --- Core Logic ---
func compressBlock(bw *bitWriter, data []byte, offset int, isFinal bool, po ParsingOptions) error {
	finalBit := uint(0)
	if isFinal {
		finalBit = 1
	}

	blockData := data[offset:]

	if len(blockData) == 0 {
		if isFinal {
			bw.WriteBits(1, 1)
			bw.WriteBits(uint(blockStatic), 2)
			// write EOB with static table
			if cv, ok := staticLitLenCodeMap[256]; ok {
				bw.WriteBits(uint(cv.Bits), int(cv.Len))
			}
		}
		return nil
	}

	// Initial greedy parse to collect candidate frequencies and build initial trees.
	greedyOutput := lz77GreedyPass(data, offset)
	greedyOutput = append(greedyOutput, uint16(256)) // EOB

	litLenFreq := make(map[uint16]int)
	distFreq := make(map[uint16]int)
	for _, item := range greedyOutput {
		switch v := item.(type) {
		case uint16:
			litLenFreq[v]++
		case lz77Match:
			lenCode, _, _ := getLengthCode(uint16(v.Length))
			distCode, _, _ := getDistCode(uint16(v.Distance))
			litLenFreq[lenCode]++
			if v.Distance > 0 {
				distFreq[distCode]++
			}
		}
	}

	calculateSize := func(lzOut []any, llLengths, dLengths []symbolBitLength) int {
		llMap := make(map[uint16]int, len(llLengths))
		for _, sbl := range llLengths {
			llMap[sbl.Symbol] = sbl.Len
		}
		dMap := make(map[uint16]int, len(dLengths))
		for _, sbl := range dLengths {
			dMap[sbl.Symbol] = sbl.Len
		}
		totalBits := 0
		for _, item := range lzOut {
			switch v := item.(type) {
			case uint16:
				totalBits += llMap[v]
			case lz77Match:
				lenCode, _, lenExtraBits := getLengthCode(uint16(v.Length))
				distCode, _, distExtraBits := getDistCode(uint16(v.Distance))
				totalBits += llMap[lenCode] + int(lenExtraBits)
				if v.Distance > 0 {
					totalBits += dMap[distCode] + int(distExtraBits)
				}
			}
		}
		return totalBits
	}

	// --- Try Dynamic Huffman ---
	litLenTree := buildLengthLimitedTree(litLenFreq, 15)
	dynamicLLBitLengths := getBitLengths(litLenTree, 0, []symbolBitLength{})
	distTree := buildLengthLimitedTree(distFreq, 15)
	dynamicDBitLengths := getBitLengths(distTree, 0, []symbolBitLength{})

	// If optimal parsing is enabled, we will re-run parsing using these bit lengths as costs.
	// For size estimation here, use the greedy output.
	dynamicSize := calculateSize(greedyOutput, dynamicLLBitLengths, dynamicDBitLengths)

	llLens := make([]uint8, 287)
	for _, sbl := range dynamicLLBitLengths {
		if sbl.Symbol < 287 {
			llLens[sbl.Symbol] = uint8(sbl.Len)
		}
	}
	dLens := make([]uint8, 32)
	for _, sbl := range dynamicDBitLengths {
		if sbl.Symbol < 32 {
			dLens[sbl.Symbol] = uint8(sbl.Len)
		}
	}

	// If optimal parsing was requested, run the optimal parser using the bit-length costs obtained above.
	if po.UseOptimal {
		// Build quick lookup maps for code bit-lengths (symbol -> length in bits)
		llLenMap := make(map[uint16]int)
		for _, sbl := range dynamicLLBitLengths {
			llLenMap[sbl.Symbol] = sbl.Len
		}
		dLenMap := make(map[uint16]int)
		for _, sbl := range dynamicDBitLengths {
			dLenMap[sbl.Symbol] = sbl.Len
		}
		optOutput := lz77OptimalParse(data, offset, llLenMap, dLenMap, po.ChainDepth)
		// Use the optimal parse for final emission.
		greedyOutput = optOutput
	}

	// Work with the final chosen parse (greedy or optimal)
	lz77Output := greedyOutput
	hlitVal := 257
	for i := 286; i >= 257; i-- {
		if llLens[i] > 0 {
			hlitVal = i + 1
			break
		}
	}
	hdistVal := 1
	for i := 31; i >= 1; i-- {
		if dLens[i] > 0 {
			hdistVal = i + 1
			break
		}
	}
	llRleSymbols, _ := rleCodeLengths(llLens[:hlitVal])
	dRleSymbols, _ := rleCodeLengths(dLens[:hdistVal])
	codeLenFreq := make(map[uint16]int)
	for _, s := range llRleSymbols {
		codeLenFreq[s]++
	}
	for _, s := range dRleSymbols {
		codeLenFreq[s]++
	}
	codeLenTree := buildLengthLimitedTree(codeLenFreq, 7)
	codeLenBitLengths := getBitLengths(codeLenTree, 0, []symbolBitLength{})
	clMap := make(map[uint16]int)
	for _, sbl := range codeLenBitLengths {
		clMap[sbl.Symbol] = sbl.Len
	}
	dynamicSize += 5 + 5 + 4
	// HCLEN is encoded as (number of code length codes) - 4 and must be at least 4 codes.
	// Ensure we never set hclenVal below 4 even if the highest non-zero index is < 4.
	hclenVal := 4
	for i := 18; i >= 0; i-- {
		if clMap[codeLenOrder[i]] > 0 {
			if i+1 > hclenVal {
				hclenVal = i + 1
			}
			break
		}
	}
	dynamicSize += hclenVal * 3
	for _, s := range llRleSymbols {
		dynamicSize += clMap[s]
		if s >= 16 {
			dynamicSize += []int{2, 3, 7}[s-16]
		}
	}
	for _, s := range dRleSymbols {
		dynamicSize += clMap[s]
		if s >= 16 {
			dynamicSize += []int{2, 3, 7}[s-16]
		}
	}

	staticSize := calculateSize(lz77Output, staticLitLenBitLengths, staticDistBitLengths)
	if staticSize < dynamicSize {
		bw.WriteBits(finalBit, 1)
		bw.WriteBits(uint(blockStatic), 2)
		writeCompressedData(lz77Output, staticLitLenCodeMap, staticDistCodeMap, bw)
	} else {
		bw.WriteBits(finalBit, 1)
		bw.WriteBits(uint(blockDynamic), 2)
		codeLenCodeMap, _ := generateCanonicalCodes(codeLenBitLengths)
		bw.WriteBits(uint(hlitVal-257), 5)
		bw.WriteBits(uint(hdistVal-1), 5)
		bw.WriteBits(uint(hclenVal-4), 4)
		for i := 0; i < hclenVal; i++ {
			bw.WriteBits(uint(clMap[codeLenOrder[i]]), 3)
		}
		llExtraIdx, dExtraIdx := 0, 0
		_, llExtra := rleCodeLengths(llLens[:hlitVal])
		_, dExtra := rleCodeLengths(dLens[:hdistVal])
		for _, s := range llRleSymbols {
			cv := codeLenCodeMap[s]
			bw.WriteBits(uint(cv.Bits), int(cv.Len))
			if s >= 16 {
				nbits := []int{2, 3, 7}[s-16]
				bw.WriteBits(uint(llExtra[llExtraIdx]), nbits)
				llExtraIdx++
			}
		}
		for _, s := range dRleSymbols {
			cv := codeLenCodeMap[s]
			bw.WriteBits(uint(cv.Bits), int(cv.Len))
			if s >= 16 {
				nbits := []int{2, 3, 7}[s-16]
				bw.WriteBits(uint(dExtra[dExtraIdx]), nbits)
				dExtraIdx++
			}
		}
		litLenCodeMap, _ := generateCanonicalCodes(dynamicLLBitLengths)
		distCodeMap, _ := generateCanonicalCodes(dynamicDBitLengths)
		writeCompressedData(lz77Output, litLenCodeMap, distCodeMap, bw)
	}

	return nil
}

func writeCompressedData(lz77Output []any, litLenMap map[uint16]codeVal, distMap map[uint16]codeVal, bw *bitWriter) {
	for _, item := range lz77Output {
		switch v := item.(type) {
		case uint16:
			if code, ok := litLenMap[v]; ok {
				bw.WriteBits(uint(code.Bits), int(code.Len))
			} else {
				// missing litlen code; skip
			}
		case lz77Match:
			lenCode, lenExtra, lenExtraBits := getLengthCode(uint16(v.Length))
			distCode, distExtra, distExtraBits := getDistCode(uint16(v.Distance))
			if code, ok := litLenMap[lenCode]; ok {
				bw.WriteBits(uint(code.Bits), int(code.Len))
			} else {
				// missing litlen code for lenCode; skip
			}
			if lenExtraBits > 0 {
				bw.WriteBits(uint(lenExtra), int(lenExtraBits))
			}
			if v.Distance > 0 {
				if code, ok := distMap[distCode]; ok {
					bw.WriteBits(uint(code.Bits), int(code.Len))
				} else {
					// missing dist code for distCode; skip
				}
				if distExtraBits > 0 {
					bw.WriteBits(uint(distExtra), int(distExtraBits))
				}
			}
		}
	}
}

var codeLenOrder = []uint16{16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15}

type fastTables struct {
	lit   []uint32
	litK  int
	dist  []uint32
	distK int
	// store a small checksum of the original lengths for safety (optional)
	llLens []uint8
	dLens  []uint8
}

var fastTableCache *lruCache

func parseDynamicTables(br *bitReader) (map[int]map[uint32]uint16, map[int]map[uint32]uint16, []uint32, int, []uint32, int, error) {
	// Debug: uncomment for verbose parsing tracing
	// fmt.Println("parseDynamicTables: start")

	readNBits := func(n int) (int, error) {
		val := 0
		for i := 0; i < n; i++ {
			bit, err := br.ReadBit()
			if err != nil {
				return 0, err
			}
			val |= (bit << i)
		}
		return val, nil
	}

	hlit, err := readNBits(5)
	if err != nil {
		return nil, nil, nil, 0, nil, 0, err
	}
	hdist, err := readNBits(5)
	if err != nil {
		return nil, nil, nil, 0, nil, 0, err
	}
	hclen, err := readNBits(4)
	if err != nil {
		return nil, nil, nil, 0, nil, 0, err
	}

	codeLenBitLengths := []symbolBitLength{}
	for i := 0; i < hclen+4; i++ {
		l, err := readNBits(3)
		if err != nil {
			return nil, nil, nil, 0, nil, 0, err
		}
		if l > 0 {
			codeLenBitLengths = append(codeLenBitLengths, symbolBitLength{Symbol: codeLenOrder[i], Len: l})
		}
	}

	_, codeLenDecoderMap := generateCanonicalCodes(codeLenBitLengths)

	decodeRle := func(targetSize int) ([]uint8, error) {
		lengths := make([]uint8, 0, targetSize)
		for len(lengths) < targetSize {
			// decode a single code using the codeLenDecoderMap (LSB-first)
			val := uint32(0)
			l := 0
			for {
				bit, err := br.ReadBit()
				if err != nil {
					return nil, err
				}
				val |= uint32(bit) << uint(l)
				l++
				if decoder, ok := codeLenDecoderMap[l]; ok {
					if s, ok2 := decoder[val]; ok2 {
						// handle symbol s
						if s <= 15 {
							lengths = append(lengths, uint8(s))
						} else if s == 16 {
							rep, err := br.ReadNBits(2)
							if err != nil {
								return nil, err
							}
							if len(lengths) == 0 {
								return nil, fmt.Errorf("header corruption: repeat with no previous length")
							}
							prev := lengths[len(lengths)-1]
							for k := 0; k < rep+3; k++ {
								lengths = append(lengths, prev)
							}
						} else if s == 17 {
							rep, err := br.ReadNBits(3)
							if err != nil {
								return nil, err
							}
							for k := 0; k < rep+3; k++ {
								lengths = append(lengths, 0)
							}
						} else if s == 18 {
							rep, err := br.ReadNBits(7)
							if err != nil {
								return nil, err
							}
							for k := 0; k < rep+11; k++ {
								lengths = append(lengths, 0)
							}
						}
						break
					}
				}
				if l > 20 {
					return nil, fmt.Errorf("header corruption: rle code too long")
				}
			}
		}
		if len(lengths) > targetSize {
			return lengths[:targetSize], nil
		}
		return lengths, nil
	}

	llLens, err := decodeRle(hlit + 257)
	if err != nil {
		return nil, nil, nil, 0, nil, 0, err
	}
	dLens, err := decodeRle(hdist + 1)
	if err != nil {
		return nil, nil, nil, 0, nil, 0, err
	}

	llBitLengths := []symbolBitLength{}
	for s, l := range llLens {
		if l > 0 {
			llBitLengths = append(llBitLengths, symbolBitLength{Symbol: uint16(s), Len: int(l)})
		}
	}
	dBitLengths := []symbolBitLength{}
	for s, l := range dLens {
		if l > 0 {
			dBitLengths = append(dBitLengths, symbolBitLength{Symbol: uint16(s), Len: int(l)})
		}
	}
	_, litLenDecoderMap := generateCanonicalCodes(llBitLengths)
	_, distDecoderMap := generateCanonicalCodes(dBitLengths)

	// Build a cache key from the literal/length and distance bit-length arrays.
	// Using hex encoding of the raw length arrays is compact and deterministic.
	key := fmt.Sprintf("%x|%x", llLens, dLens)
	if fastTableCache != nil {
		if ft, ok := fastTableCache.Get(key); ok {
			// Verify lengths match (safety against accidental collisions)
			if len(ft.llLens) == len(llLens) && len(ft.dLens) == len(dLens) {
				match := true
				for i := range llLens {
					if ft.llLens[i] != llLens[i] {
						match = false
						break
					}
				}
				if match {
					for i := range dLens {
						if ft.dLens[i] != dLens[i] {
							match = false
							break
						}
					}
				}
				if match {
					return litLenDecoderMap, distDecoderMap, ft.lit, ft.litK, ft.dist, ft.distK, nil
				}
			}
		}
	}

	// Build fast tables for these decoder maps and cache them.
	litFast, litK := BuildFastTable(litLenDecoderMap, 12)
	distFast, distK := BuildFastTable(distDecoderMap, 12)
	ft := fastTables{lit: litFast, litK: litK, dist: distFast, distK: distK, llLens: llLens, dLens: dLens}
	if fastTableCache != nil {
		fastTableCache.Put(key, ft)
	}
	return litLenDecoderMap, distDecoderMap, litFast, litK, distFast, distK, nil
}

// Compress from a reader to a writer.
func Compress(in io.Reader, out io.Writer, po ParsingOptions) error {
	bufOut := bufio.NewWriterSize(out, 64*1024)
	if _, err := bufOut.Write([]byte(magicNumber)); err != nil {
		return fmt.Errorf("writing magic number: %w", err)
	}

	bw := &bitWriter{writer: bufOut}
	var window []byte

	const maxBlockSize = 32768
	buf := make([]byte, maxBlockSize)

	for {
		n, err := in.Read(buf)
		if err != nil && err != io.EOF {
			return fmt.Errorf("reading input data: %w", err)
		}

		isFinal := (err == io.EOF)
		if n == 0 && !isFinal {
			break
		}

		blockData := buf[:n]

		processingData := make([]byte, len(window)+len(blockData))
		copy(processingData, window)
		copy(processingData[len(window):], blockData)

		offset := len(window)

		if errCompress := compressBlock(bw, processingData, offset, isFinal, po); errCompress != nil {
			return fmt.Errorf("failed to compress block: %w", errCompress)
		}

		start := 0
		if len(processingData) > windowSize {
			start = len(processingData) - windowSize
		}
		newWindow := make([]byte, len(processingData)-start)
		copy(newWindow, processingData[start:])
		window = newWindow

		if isFinal {
			break
		}
	}

	// Flush only once at the very end of the stream to avoid inter-block padding.
	bw.Flush()
	// Ensure buffered writer is flushed to the underlying writer.
	if err := bufOut.Flush(); err != nil {
		return err
	}
	return nil
}
