package smol

import (
	"container/heap"
	"sort"
)

// --- Huffman Tree Implementation ---
type huffmanNode struct {
	Symbol      uint16
	Freq        int
	Left, Right *huffmanNode
}
type priorityQueue []*huffmanNode

func (pq priorityQueue) Len() int           { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool { return pq[i].Freq < pq[j].Freq }
func (pq priorityQueue) Swap(i, j int)      { pq[i], pq[j] = pq[j], pq[i] }
func (pq *priorityQueue) Push(x any)        { *pq = append(*pq, x.(*huffmanNode)) }
func (pq *priorityQueue) Pop() any {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

func buildHuffmanTree(freqMap map[uint16]int) *huffmanNode {
	if len(freqMap) == 0 {
		return nil
	}
	pq := &priorityQueue{}
	heap.Init(pq)
	for symbol, freq := range freqMap {
		if freq > 0 {
			heap.Push(pq, &huffmanNode{Symbol: symbol, Freq: freq})
		}
	}
	if pq.Len() == 0 {
		return nil
	}
	if pq.Len() == 1 {
		node := heap.Pop(pq).(*huffmanNode)
		return &huffmanNode{Freq: node.Freq, Left: node}
	}
	for pq.Len() > 1 {
		left := heap.Pop(pq).(*huffmanNode)
		right := heap.Pop(pq).(*huffmanNode)
		internalNode := &huffmanNode{Freq: left.Freq + right.Freq, Left: left, Right: right}
		heap.Push(pq, internalNode)
	}
	return heap.Pop(pq).(*huffmanNode)
}

// packageMerge attempts to produce length-limited code lengths for a sorted (ascending) list of symbol
// frequencies. The earlier attempted package-merge implementation proved buggy in practice; here we use a
// robust two-phase approach that (1) builds an unconstrained Huffman tree to obtain initial bit lengths,
// then (2) enforces the maximum code length by folding longer codes upward (the standard technique used by
// PNG/zlib). The result preserves canonical ordering when canonical codes are later generated.
func packageMerge(freqs []uint64, maxLen int) []int {
	m := len(freqs)
	if m == 0 || maxLen == 0 {
		return nil
	}
	if m == 1 {
		return []int{1}
	}

	// Build Huffman tree using existing huffmanNode/priorityQueue types (symbol := index)
	pq := &priorityQueue{}
	heap.Init(pq)
	for i, f := range freqs {
		if f == 0 {
			// give a tiny weight so zero-frequency symbols sit at the bottom
			heap.Push(pq, &huffmanNode{Symbol: uint16(i), Freq: 1})
		} else {
			heap.Push(pq, &huffmanNode{Symbol: uint16(i), Freq: int(f)})
		}
	}
	if pq.Len() == 0 {
		return nil
	}
	if pq.Len() == 1 {
		res := make([]int, m)
		res[0] = 1
		return res
	}
	for pq.Len() > 1 {
		left := heap.Pop(pq).(*huffmanNode)
		right := heap.Pop(pq).(*huffmanNode)
		parent := &huffmanNode{Freq: left.Freq + right.Freq, Left: left, Right: right}
		heap.Push(pq, parent)
	}
	root := heap.Pop(pq).(*huffmanNode)

	lengths := make([]int, m)
	var maxDepth int
	var dfs func(n *huffmanNode, d int)
	dfs = func(n *huffmanNode, d int) {
		if n == nil {
			return
		}
		if n.Left == nil && n.Right == nil {
			// leaf
			idx := int(n.Symbol)
			if idx >= 0 && idx < m {
				lengths[idx] = d
				if d > maxDepth {
					maxDepth = d
				}
			}
			return
		}
		dfs(n.Left, d+1)
		dfs(n.Right, d+1)
	}
	dfs(root, 0)

	if maxDepth <= maxLen {
		for i := range lengths {
			if lengths[i] == 0 {
				lengths[i] = 1
			}
		}
		return lengths
	}

	// Count codes per length
	blCount := make([]int, maxDepth+1)
	for _, l := range lengths {
		if l == 0 {
			l = 1
		}
		if l >= len(blCount) {
			l = len(blCount) - 1
		}
		blCount[l]++
	}

	// Fold longer codes up until no length > maxLen
	for l := maxDepth; l > maxLen; l-- {
		for blCount[l] > 0 {
			blCount[l] -= 2
			blCount[l-1]++
		}
	}

	// Assign lengths to symbols: shortest lengths to highest-frequency symbols
	pairs := make([]struct {
		idx int
		f   uint64
	}, 0, m)
	for i, f := range freqs {
		pairs = append(pairs, struct {
			idx int
			f   uint64
		}{i, f})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].f != pairs[j].f {
			return pairs[i].f > pairs[j].f
		}
		return pairs[i].idx < pairs[j].idx
	})

	res := make([]int, m)
	pos := 0
	for l := 1; l <= maxLen; l++ {
		cnt := 0
		if l < len(blCount) {
			cnt = blCount[l]
		}
		for i := 0; i < cnt; i++ {
			if pos >= len(pairs) {
				break
			}
			res[pairs[pos].idx] = l
			pos++
		}
	}
	for pos < len(pairs) {
		res[pairs[pos].idx] = maxLen
		pos++
	}

	return res
}

func buildLengthLimitedTree(originalFreqMap map[uint16]int, maxLen int) *huffmanNode {
	// collect non-zero symbols and sort ascending by frequency
	syms := make([]uint16, 0)
	freqs := make([]uint64, 0)
	for s, f := range originalFreqMap {
		if f > 0 {
			syms = append(syms, s)
			freqs = append(freqs, uint64(f))
		}
	}
	if len(syms) == 0 {
		return nil
	}
	// sort by freq ascending (stable with symbol tie-break)
	type kv struct {
		s uint16
		f uint64
	}
	kvs := make([]kv, len(syms))
	for i := range syms {
		kvs[i] = kv{syms[i], freqs[i]}
	}
	sort.Slice(kvs, func(i, j int) bool {
		if kvs[i].f != kvs[j].f {
			return kvs[i].f < kvs[j].f
		}
		return kvs[i].s < kvs[j].s
	})
	sortedSyms := make([]uint16, len(kvs))
	sortedFreqs := make([]uint64, len(kvs))
	for i := range kvs {
		sortedSyms[i] = kvs[i].s
		sortedFreqs[i] = kvs[i].f
	}

	// compute code lengths via package-merge
	lengths := packageMerge(sortedFreqs, maxLen)
	// build symbolBitLength slice
	sbls := make([]symbolBitLength, 0, len(lengths))
	for i, l := range lengths {
		if l > 0 {
			sbls = append(sbls, symbolBitLength{Symbol: sortedSyms[i], Len: l})
		}
	}
	// generate canonical codes and then build a tree by inserting leaves at code bit paths
	codeMap, _ := generateCanonicalCodes(sbls)
	root := &huffmanNode{}
	for _, sbl := range sbls {
		cv := codeMap[sbl.Symbol]
		if cv.Len == 0 {
			continue
		}
		// recover MSB-first code
		msbCode := reverseBits(cv.Bits, int(cv.Len))
		node := root
		for i := int(cv.Len) - 1; i >= 0; i-- {
			bit := (msbCode >> uint(i)) & 1
			if bit == 0 {
				if node.Left == nil {
					node.Left = &huffmanNode{}
				}
				node = node.Left
			} else {
				if node.Right == nil {
					node.Right = &huffmanNode{}
				}
				node = node.Right
			}
		}
		// place leaf
		node.Symbol = sbl.Symbol
	}
	return root
}

// --- Canonical Huffman Code Generation ---
type symbolBitLength struct {
	Symbol uint16
	Len    int
}

// codeVal stores the numeric code (LSB-first bit ordering) and its length.
type codeVal struct {
	Bits uint32
	Len  uint8
}

func getBitLengths(node *huffmanNode, length int, lengths []symbolBitLength) []symbolBitLength {
	if node == nil {
		return lengths
	}
	if node.Left == nil && node.Right == nil {
		return append(lengths, symbolBitLength{Symbol: node.Symbol, Len: length})
	}
	lengths = getBitLengths(node.Left, length+1, lengths)
	lengths = getBitLengths(node.Right, length+1, lengths)
	return lengths
}

func reverseBits(val uint32, n int) uint32 {
	var r uint32
	for i := 0; i < n; i++ {
		r <<= 1
		r |= (val & 1)
		val >>= 1
	}
	return r
}

// generateCanonicalCodes returns a forward map (symbol -> codeVal) and a decoder map keyed by length: decoder[len][bits] = symbol.
func generateCanonicalCodes(lengths []symbolBitLength) (map[uint16]codeVal, map[int]map[uint32]uint16) {
	sort.Slice(lengths, func(i, j int) bool {
		if lengths[i].Len != lengths[j].Len {
			return lengths[i].Len < lengths[j].Len
		}
		return lengths[i].Symbol < lengths[j].Symbol
	})
	codeMap := make(map[uint16]codeVal)
	decoderMap := make(map[int]map[uint32]uint16)
	if len(lengths) > 0 {
		if len(lengths) == 1 {
			// Single symbol: give it code 0 length 1
			codeMap[lengths[0].Symbol] = codeVal{Bits: 0, Len: 1}
			decoderMap[1] = map[uint32]uint16{0: lengths[0].Symbol}
			return codeMap, decoderMap
		}
		var code uint32 = 0
		currentLen := lengths[0].Len
		for _, sbl := range lengths {
			if sbl.Len > currentLen {
				code <<= uint(sbl.Len - currentLen)
				currentLen = sbl.Len
			}
			// 'code' is the canonical MSB-first code; reverse it to match our LSB-first writer/reader.
			rev := reverseBits(code, sbl.Len)
			codeMap[sbl.Symbol] = codeVal{Bits: rev, Len: uint8(sbl.Len)}
			if decoderMap[sbl.Len] == nil {
				decoderMap[sbl.Len] = make(map[uint32]uint16)
			}
			decoderMap[sbl.Len][rev] = sbl.Symbol
			code++
		}
	}
	return codeMap, decoderMap
}

// fastEntry is encoded as a single uint32 to make table lookups compact and
// branch-friendly. 0 means unused. Packed as: (symbol << 6) | len
// where len==0 indicates an empty slot. symbol fits in upper bits (we use 16-bit
// symbols, so uint32 is plenty).

func packFastEntry(sym uint16, l uint8) uint32 {
	if l == 0 {
		return 0
	}
	return (uint32(sym) << 6) | uint32(l)
}

func unpackFastEntry(e uint32) (uint16, uint8) {
	if e == 0 {
		return 0, 0
	}
	lenv := uint8(e & 0x3F)
	sym := uint16(e >> 6)
	return sym, lenv
}

// BuildFastTable creates a K-bit prefix table from the per-length decoder maps.
// For codes with length <= K, it fills all table entries that share the same
// K-bit prefix so decoding can be done with a single table lookup and a
// variable bit consume. Codes longer than K are left to the fallback decoder.
// The returned table is a slice of packed uint32 entries.
func BuildFastTable(decoderMap map[int]map[uint32]uint16, K int) ([]uint32, int) {
	if K <= 0 {
		return nil, 0
	}
	size := 1 << uint(K)
	table := make([]uint32, size)

	// Convert decoderMap into a flat list of entries to avoid nested map access
	// during table population. This also gives better locality.
	type entry struct {
		bits uint32
		sym  uint16
		l    int
	}
	entries := make([]entry, 0)
	for l, m := range decoderMap {
		if l <= 0 || l > 31 {
			continue
		}
		if l <= K {
			for bits, sym := range m {
				entries = append(entries, entry{bits: bits, sym: sym, l: l})
			}
		}
	}

	for _, e := range entries {
		repeat := 1 << uint(K-e.l)
		base := int(e.bits)
		for s := 0; s < repeat; s++ {
			idx := base | (s << uint(e.l))
			if idx < size {
				table[idx] = packFastEntry(e.sym, uint8(e.l))
			}
		}
	}
	return table, K
}
