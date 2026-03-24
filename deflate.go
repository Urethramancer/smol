package smol

// --- DEFLATE Tables and Header Logic ---
const magicNumber = "SMOL"

type blockType uint8

const (
	blockStored blockType = iota
	blockStatic
	blockDynamic
)

var lenCodes []struct{ base, extraBits uint16 }
var distCodes []struct{ base, extraBits uint16 }

// StaticLitLenDecoderMap and staticDistDecoderMap are maps from code length to a map of code prefix to symbol.
var StaticLitLenDecoderMap map[int]map[uint32]uint16
var staticDistDecoderMap map[int]map[uint32]uint16
var staticLitLenCodeMap map[uint16]codeVal
var staticDistCodeMap map[uint16]codeVal
var staticLitLenBitLengths []symbolBitLength
var staticDistBitLengths []symbolBitLength

// fast prefix tables for static codes (packed uint32 entries)
var staticLitLenFast []uint32
var staticLitK int
var staticDistFast []uint32
var staticDistK int

func init() {
	lenCodes = []struct{ base, extraBits uint16 }{{3, 0}, {4, 0}, {5, 0}, {6, 0}, {7, 0}, {8, 0}, {9, 0}, {10, 0}, {11, 1}, {13, 1}, {15, 1}, {17, 1}, {19, 2}, {23, 2}, {27, 2}, {31, 2}, {35, 3}, {43, 3}, {51, 3}, {59, 3}, {67, 4}, {83, 4}, {99, 4}, {115, 4}, {131, 5}, {163, 5}, {195, 5}, {227, 5}, {258, 0}}
	distCodes = []struct{ base, extraBits uint16 }{{1, 0}, {2, 0}, {3, 0}, {4, 0}, {5, 1}, {7, 1}, {9, 2}, {13, 2}, {17, 3}, {25, 3}, {33, 4}, {49, 4}, {65, 5}, {97, 5}, {129, 6}, {193, 6}, {257, 7}, {385, 7}, {513, 8}, {769, 8}, {1025, 9}, {1537, 9}, {2049, 10}, {3073, 10}, {4097, 11}, {6145, 11}, {8193, 12}, {12289, 12}, {16385, 13}, {24577, 13}}

	staticLitLenBitLengths = make([]symbolBitLength, 288)
	for i := 0; i <= 143; i++ {
		staticLitLenBitLengths[i] = symbolBitLength{Symbol: uint16(i), Len: 8}
	}
	for i := 144; i <= 255; i++ {
		staticLitLenBitLengths[i] = symbolBitLength{Symbol: uint16(i), Len: 9}
	}
	for i := 256; i <= 279; i++ {
		staticLitLenBitLengths[i] = symbolBitLength{Symbol: uint16(i), Len: 7}
	}
	for i := 280; i <= 287; i++ {
		staticLitLenBitLengths[i] = symbolBitLength{Symbol: uint16(i), Len: 8}
	}
	staticLitLenCodeMap, StaticLitLenDecoderMap = generateCanonicalCodes(staticLitLenBitLengths)
	staticLitLenFast, staticLitK = BuildFastTable(StaticLitLenDecoderMap, 9)

	staticDistBitLengths = make([]symbolBitLength, 32)
	for i := 0; i <= 31; i++ {
		staticDistBitLengths[i] = symbolBitLength{Symbol: uint16(i), Len: 5}
	}
	staticDistCodeMap, staticDistDecoderMap = generateCanonicalCodes(staticDistBitLengths)
	staticDistFast, staticDistK = BuildFastTable(staticDistDecoderMap, 9)
}

func getLengthCode(length uint16) (code, extraVal uint16, extraBits uint8) {
	if length == 258 {
		return 285, 0, 0
	}
	for i := 0; i < len(lenCodes)-1; i++ {
		if length >= lenCodes[i].base && length < lenCodes[i+1].base {
			return uint16(257 + i), length - lenCodes[i].base, uint8(lenCodes[i].extraBits)
		}
	}
	return 285, 0, 0
}

func getDistCode(dist uint16) (code, extraVal uint16, extraBits uint8) {
	if dist == 0 {
		return 0, 0, 0
	}
	for i := 0; i < len(distCodes)-1; i++ {
		if dist >= distCodes[i].base && dist < distCodes[i+1].base {
			return uint16(i), dist - distCodes[i].base, uint8(distCodes[i].extraBits)
		}
	}
	lastIdx := len(distCodes) - 1
	return uint16(lastIdx), dist - distCodes[lastIdx].base, uint8(distCodes[lastIdx].extraBits)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func rleCodeLengths(lengths []uint8) ([]uint16, []uint16) {
	var symbols, extraBits []uint16
	if len(lengths) == 0 {
		return symbols, extraBits
	}
	for i := 0; i < len(lengths); {
		l := lengths[i]
		j := i + 1
		for j < len(lengths) && lengths[j] == l {
			j++
		}
		count := j - i

		if l == 0 {
			for count > 0 {
				run := count
				run = min(run, 138)
				if run >= 11 {
					symbols = append(symbols, 18)
					extraBits = append(extraBits, uint16(run-11))
				} else if run >= 3 {
					symbols = append(symbols, 17)
					extraBits = append(extraBits, uint16(run-3))
				} else {
					for k := 0; k < run; k++ {
						symbols = append(symbols, 0)
					}
				}
				count -= run
			}
		} else {
			symbols = append(symbols, uint16(l))
			count--
			for count > 0 {
				run := count
				run = min(run, 6)
				if run >= 3 {
					symbols = append(symbols, 16)
					extraBits = append(extraBits, uint16(run-3))
				} else {
					for k := 0; k < run; k++ {
						symbols = append(symbols, uint16(l))
					}
				}
				count -= run
			}
		}
		i = j
	}
	return symbols, extraBits
}
