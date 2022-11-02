package lib2x3

import (
	"encoding/base32"
	"errors"
	"fmt"
	"io"
	"sort"
)

type EdgeGrouping byte

const (
	Grouping_TBD        EdgeGrouping = 0 // Not yet known
	Grouping_111        EdgeGrouping = 1 // All 3 edges go to the same vertex
	Grouping_TripleEdge EdgeGrouping = 1 // All 3 edges go to the same vertex
	Grouping_112        EdgeGrouping = 2 // First 2 edges share the same vertex
	Grouping_DoubleEdge EdgeGrouping = 2 // First 2 edges share the same vertex
	Grouping_Disjoint   EdgeGrouping = 3 // Edges connect to different vertices

)

/*
type VtxEdgesType int8
const (
	TriLoop VtxEdgesType = 0 // (e, ~e, π, ~π), 0 edges (always v=1)
	TwoLoop VtxEdgesType = 1 // "u" vertex; single edge
	OneLoop VtxEdgesType = 2 // "d" vertex; single loop
	NoLoops VtxEdgesType = 3 // "y" vertex, no loops, three edges
)

var VtxEdgesTypeDesc = []string{
	"***",
	"|**",
	"||*",
	"|||",
}
*/

// triVtx starts as a vertex in a conventional "2x3" vertex+edge and used to derive a canonical LSM-friendly encoding (i.e. TriID)
// El Shaddai's Grace abounds.  Emmanuel, God with us!  Yashua has come and victory has been sealed!
type triVtx struct {
	FamilyID GroupID      // which vtx family group this is
	CyclesID GroupID      // which cycles group this is
	count    byte         // Instances of this vtx type
	vtxID    byte         // Initial vertex ID (zero-based index)
	edges    [3]groupEdge // Edges to other vertices
}

type groupEdge struct {
	srcVtx   byte    // initial source vertex index
	srcGroup GroupID // one-based source vertex group index (known after cycle spectrum sort performed)
	siblings byte    // number of other edges also attached to srcVtx [0,1,2]
	isLoop   bool    // true if this edge is a loop
	edgeSign int8    // +1 or -1
}

// func (e *groupEdge) CycleGroupID() int {
// 	return int(e.srcGroup)
// }

// Returns:
//    0 if loop
//    1 if edge (non-loop)
func (e *groupEdge) LoopBit() int32 {
	if e.isLoop {
		return 0
	}
	return 1
}

type graphVtx struct {
	triVtx
	cycles []int64 // for traces cycle fingerprint for cycles ci
	Ci0    []int64 // matrix row of X^i for this vtx -- by initial vertex ID
	Ci1    []int64 // matrix row of X^(i+1) for this vtx,
}

func (v *triVtx) TriSign() TriSign {
	sign := byte(0)
	for _, ei := range v.edges {
		sign <<= 1
		if ei.edgeSign < 0 {
			sign |= 1
		}
	}
	return TriSign(sign)
}

/*
// Returns sortable ordinal expressing the 3 bits in i={0,1,2} order:
//    Edges[0..2].IsLoop ? 0 : 1
func (v *triVtx) EdgesType() VtxEdgesType {
	edges := int32(3)
	for _, ei := range v.edges {
		if ei.isLoop {
			edges--
		}
	}
	return VtxEdgesType(edges)
}
*/

func (v *triVtx) EdgesCycleOrd() int {
	ord := int(0)
	for _, ei := range v.edges {
		ord = (ord << GroupID_Bits) | int(ei.srcGroup)
	}
	return ord
}

func (v *triVtx) AppendEdgesLabel(io []byte) []byte {
	for _, ei := range v.edges {
		io = append(io, 'A'+byte(ei.srcGroup)-1)
	}
	return io
}

func (v *triVtx) familyEdgeOrd(ei int) uint8 {
	cyclesID := v.edges[ei].srcGroup
	ord := uint8(cyclesID) << 1
	if v.edges[ei].isLoop || cyclesID == v.CyclesID {
		ord = 1
	}
	return ord
}

func (v *triVtx) familyEdgesOrd() int {
	ord := 0
	for i := range v.edges {
		ord = (ord << 8) | int(v.familyEdgeOrd(i))
	}
	return ord
}

func (v *triVtx) printEdges(tri []byte) {
	c := byte('?')
	for ei, e := range v.edges {
		if e.isLoop || e.srcGroup == v.CyclesID {
			c = '*'
		} else {
			c = 'A' + byte(e.srcGroup) - 1
		}
		tri[ei] = c
	}
}

// pre: v.edges[].cyclesID has been determined and set
func (v *triVtx) canonizeVtx() {

	// Canonically order edges by edge type then by edge sign & groupID
	// Note that we ignore edge grouping, which means graphs with double edges will encode onto graphs having a non-double edge equivalent.
	// For v=6, this turns out to be 4% less graph encodings (52384 vs 50664) and for v=8 about 2% less encodings (477k vs 467k).
	// If we figure out we need these encodings back (so they count as valid state modes), we can just export the grouping info.
	//
	// A fundamental mystery is: what ARE these particle "sign modes" anyway??
	// Are they perhaps vibration modes of a membrane (and so all are valid)?
	// Or are Griggs-style graphs merely a stepping stone to understanding the underlying symbolic representation of God's particles that
	//     are not bound to the ways a human-conceived graph framework can construct graphs that reduces to a particular particle representation.
	sort.Slice(v.edges[:], func(i, j int) bool {
		if d := int(v.familyEdgeOrd(i)) - int(v.familyEdgeOrd(j)); d != 0 {
			return d < 0
		}
		return v.edges[i].edgeSign > v.edges[j].edgeSign
	})

	// See above --
	// // With edge order now canonic, determine the grouping.
	// // Since we sort by edgeOrd, edges going to the same vertex are always consecutive and appear first
	// // This means we only need to check a small number of cases
	// if !v.edges[0].isLoop && v.edges[0].srcVtx == v.edges[1].srcVtx {
	// 	if v.edges[1].srcVtx == v.edges[2].srcVtx {
	// 		v.grouping = Grouping_111
	// 	} else {
	// 		v.grouping = Grouping_112
	// 	}
	// } else {
	// 	v.grouping = Grouping_Disjoint
	// }
}

func (v *graphVtx) AddLoop(from int32, edgeSign int8) {
	v.AddEdge(from, edgeSign, true)
}

func (v *graphVtx) AddEdge(from int32, edgeSign int8, isLoop bool) {
	var ei int
	for ei = range v.edges {
		if v.edges[ei].edgeSign == 0 {
			v.edges[ei] = groupEdge{
				srcVtx:   byte(from),
				isLoop:   isLoop,
				edgeSign: edgeSign,
			}
			break
		}
	}

	if ei >= 3 {
		panic("tried to add more than 3 edges")
	}
}

func (v *graphVtx) Init(vtxID byte) {
	v.count = 1
	v.vtxID = vtxID
	v.edges[0].edgeSign = 0
	v.edges[1].edgeSign = 0
	v.edges[2].edgeSign = 0
}

const Graph3IDSz = 16

type Graph3ID [Graph3IDSz]byte

func (uid Graph3ID) String() string {
	return Base32Encoding.EncodeToString(uid[:])
}

// GeohashBase32Alphabet is the alphabet used for Base32Encoding
const GeohashBase32Alphabet = "0123456789bcdefghjkmnpqrstuvwxyz"

var (
	// Base32Encoding is used to encode/decode binary buffer to/from base 32
	Base32Encoding = base32.NewEncoding(GeohashBase32Alphabet).WithPadding(base32.NoPadding)
)

func chopBuf(consume []int64, N int32) (alloc []int64, remain []int64) {
	return consume[0:N], consume[N:]
}

type vtxStatus int32

const (
	newlyReset vtxStatus = iota + 1
	calculating
	canonized
	tricoded
)

type graphState struct {
	vtxCount       int32
	vtxDimSz       int32
	vtx            []*graphVtx // maps initial vertex index to cycle group vtx
	vtxGroups      []*graphVtx // ordered list of vtx groups
	numCycleGroups byte        // number of unique cycle vectors present
	numVtxGroups   byte        // number of unique vertex groups present

	curCi  int32
	traces Traces
	triID  []byte
	status vtxStatus
	store  [256]byte
}

func (X *graphState) reset(numVerts byte) {
	Nv := int32(numVerts)

	X.vtxCount = Nv
	X.numVtxGroups = numVerts
	X.curCi = 0
	X.numCycleGroups = 0
	X.triID = X.triID[:0]
	X.status = newlyReset
	if X.vtxDimSz >= Nv {
		return
	}

	// Prevent rapid resize allocs
	if Nv < 8 {
		Nv = 8
	}
	X.vtxDimSz = Nv

	X.vtx = make([]*graphVtx, Nv, 2*Nv)
	X.vtxGroups = X.vtx[Nv : 2*Nv]
	if cap(X.triID) == 0 {
		X.triID = X.store[:0]
	}

	// Place cycle bufs on each vtx
	buf := make([]int64, MaxVtxID+3*Nv*Nv)
	X.traces, buf = chopBuf(buf, MaxVtxID)

	for i := int32(0); i < Nv; i++ {
		v := &graphVtx{}
		X.vtx[i] = v
		X.vtxGroups[i] = v
		v.Ci0, buf = chopBuf(buf, Nv)
		v.Ci1, buf = chopBuf(buf, Nv)
		v.cycles, buf = chopBuf(buf, Nv)
	}

}

var (
	ErrNilGraph = errors.New("nil graph")
	ErrBadEdges = errors.New("edge count does not correspond to vertex count")
)

func (X *graphState) AssignGraph(Xsrc *Graph) error {
	if Xsrc == nil {
		X.reset(0)
		return ErrNilGraph
	}

	Nv := Xsrc.NumVerts()
	X.reset(Nv)

	// Init vtx lookup map so we can find the group for a given initial vertex idx
	Xv := X.Vtx()
	for i := byte(0); i < Nv; i++ {
		Xv[i].Init(i)
		X.vtxGroups[i] = Xv[i]
	}

	// First, add edges that connect to the same vertex (loops and arrows)
	for i, vi := range Xv {
		vtype := Xsrc.vtx[i]
		for j := vtype.NumLoops(); j > 0; j-- {
			vi.AddLoop(int32(i), +1)
		}
		for j := vtype.NumArrows(); j > 0; j-- {
			vi.AddLoop(int32(i), -1)
		}
	}

	// Second, add edges connecting two different vertices
	for _, edge := range Xsrc.Edges() {
		ai, bi := edge.VtxIdx()
		pos, neg := edge.EdgeType().NumPosNeg()
		for j := pos; j > 0; j-- {
			Xv[ai].AddEdge(bi, +1, false)
			Xv[bi].AddEdge(ai, +1, false)
		}
		for j := neg; j > 0; j-- {
			Xv[ai].AddEdge(bi, -1, false)
			Xv[bi].AddEdge(ai, -1, false)
		}
	}

	// Count edges to see if we have a valid graph
	Ne := byte(0)

	// Calculate and assign siblings for every edge
	// This ensures we can sort (group) edges first by co-connectedness
	for _, v := range Xv {
		for i, ei := range v.edges {
			v.edges[i].siblings = 0
			if ei.edgeSign != 0 {
				Ne += v.count
			}
			for j, ej := range v.edges {
				if i != j && ei.srcVtx == ej.srcVtx {
					v.edges[i].siblings++
				}
			}
		}
	}

	if Ne != 3*Nv {
		return ErrBadEdges
	}

	return nil
}

func (X *graphState) VtxGroups() []*graphVtx {
	return X.vtxGroups[:X.numVtxGroups]
}

func (X *graphState) Vtx() []*graphVtx {
	return X.vtx[:X.vtxCount]
}

func (X *graphState) sortVtxGroups() {
	Xg := X.VtxGroups()

	// With edges on vertices now canonic order, we now re-order to assert canonic order within each group.
	sort.Slice(Xg, func(i, j int) bool {
		vi := Xg[i]
		vj := Xg[j]

		if d := int(vi.FamilyID) - int(vj.FamilyID); d != 0 {
			return d < 0
		}

		for e := range vi.edges {
			if d := int(vi.familyEdgeOrd(e)) - int(vj.familyEdgeOrd(e)); d != 0 {
				return d < 0
			}
		}

		if d := int(vi.CyclesID) - int(vj.CyclesID); d != 0 {
			return d < 0
		}

		// Then sort by tri-sign
		for ei := range vi.edges {
			if d := vi.edges[ei].edgeSign - vj.edges[ei].edgeSign; d != 0 {
				return d < 0
			}
		}

		return false
	})
}

// For the currently assigned Graph, this calculates its cycles and traces up to a given level.
func (X *graphState) calcUpTo(numTraces int32) {
	Nv := X.vtxCount

	if numTraces < Nv {
		numTraces = Nv
	}

	Xv := X.Vtx()
	Xg := X.VtxGroups()

	// Init C0
	if X.status == newlyReset {
		X.status = calculating

		for i, vi := range Xv {
			for j := 0; int32(j) < Nv; j++ {
				c0 := int64(0)
				if i == j {
					c0 = 1
				}
				vi.Ci0[j] = c0
			}
		}
	}

	// This loop effectively calculates each successive graph matrix power.
	for ; X.curCi < numTraces; X.curCi++ {

		ci := X.curCi
		odd := (ci & 1) != 0
		traces_ci := int64(0)

		// Calculate Ci+1 by "flowing" the current state (Ci) through X's edges.
		for _, vi := range Xg {

			// Alternate which is the prev / next state store
			Ci0, Ci1 := vi.Ci0, vi.Ci1
			if odd {
				Ci0, Ci1 = Ci1, Ci0
			}

			for j := int32(0); j < Nv; j++ {
				dot := int64(0)
				vj := Xv[j]
				for _, e := range vj.edges {
					input := Ci0[e.srcVtx]
					if e.edgeSign < 0 {
						input = -input
					}
					dot += input
				}
				Ci1[j] = dot
			}

			vi_cycles_ci := Ci1[vi.vtxID]
			if ci < Nv {
				vi.cycles[ci] = vi_cycles_ci
			}
			traces_ci += int64(vi.count) * vi_cycles_ci
		}
		X.traces[ci] = traces_ci
	}

}

func (X *graphState) canonize() {
	if X.status >= canonized {
		return
	}

	Nv := X.vtxCount
	X.calcUpTo(Nv)

	Xv := X.Vtx()
	Xg := X.VtxGroups()

	// Sort vertices by vertex's innate characteristics & cycle signature
	{
		sort.Slice(Xg, func(i, j int) bool {
			vi := Xg[i]
			vj := Xg[j]

			// Sort by cycle count first and foremost
			// The cycle count vector (an integer sequence of size Nv) is what characterizes a vertex.
			for ci := int32(0); ci < Nv; ci++ {
				d := vi.cycles[ci] - vj.cycles[ci]
				if d != 0 {
					return d < 0
				}
			}

			return false
		})
	}

	// Now that vertices are sorted by cycle vector, assign each vertex the cycle group number now associated with its vertex index.
	{
		X.numCycleGroups = 1
		var v_prev *graphVtx
		for _, vi := range Xg {
			if v_prev != nil {
				for ci := int32(0); ci < Nv; ci++ {
					if vi.cycles[ci] != v_prev.cycles[ci] {
						X.numCycleGroups++
						break
					}
				}
			}
			vi.CyclesID = GroupID(X.numCycleGroups)
			v_prev = vi
			//X.TriGraph.Groups[i] = vi.ExportTriGroup()
		}
	}

	//X.TriGraph.Canonize()

	/*
		tri := TriGroup{
			Grouping: v.grouping,
		}

		sign := TriSign(0)
		for i, ei := range v.edges {
			tri.Edges[i] = FormGroupEdge(GroupID(ei.srcGroup), ei.isLoop)
			sign <<= 1
			if ei.edgeSign < 0 {
				sign |= 1
			}
		}
		tri.Counts[sign] = int8(v.count)
		return tri
	*/

	// With cycle group numbers assigned to each vertex, assign srcGroup to all edges and then finally order edges on each vertex canonically.
	for _, vi := range Xg {
		for ei, e := range vi.edges {
			src_vi := Xv[e.srcVtx]
			vi.edges[ei].srcGroup = src_vi.CyclesID
		}

		// With each edge srcGroup now assigned, we can order the edges canonically
		vi.canonizeVtx()
	}

	X.sortVtxGroups()

	// {
	// 	// At this point, vertices are sorted via tricode (using group numbering via canonic ranking of cycle vectors)
	// 	// Here we collapse consecutive vertices with the same tricode into a "super" group
	// 	// As we collapse tricodes, we must reassign new groupIDs
	// 	X.TriGraph.Clear(len(Xg))
	// 	for i, vi := range Xg {
	// 		X.TriGraph.Groups[i] = vi.ExportTriGroup()
	// 	}
	// 	X.TriGraph.Consolidate()
	// }

	// Last but not least, we do an RLE-style compression of the now-canonic vertex series.
	// Note that doing so invalidates edge.srvVtx values, so lets zero them out for safety.
	// Work right to left
	{
		L := byte(0)
		for R := int32(1); R < Nv; R++ {
			XgL := Xg[L]
			XgR := Xg[R]
			identical := false
			if XgL.CyclesID == XgR.CyclesID && XgL.familyEdgesOrd() == XgR.familyEdgesOrd() {
				identical = true
				for ei := range XgL.edges {
					if XgL.edges[ei].srcGroup != XgR.edges[ei].srcGroup || XgL.edges[ei].edgeSign != XgR.edges[ei].edgeSign {
						identical = false
						break
					}
				}
			}

			// If exact match, absorb R into L, otherwise advance L (old R becomes new L)
			if identical {
				XgL.count += XgR.count
			} else {
				L++
				Xg[L], Xg[R] = Xg[R], Xg[L]
			}
		}
		X.numVtxGroups = L + 1
	}

	X.status = canonized
}

func TriIDBinLen(Nv int32) int {
	return int(Nv) * 5
}

func TriIDStrLen(Nv byte) int32 {
	return int32(Nv) * 7
}

// GraphTriID is a 2x3 cycle spectrum encoding
type GraphTriID []byte

func (X *graphState) TriGraphExprStr() string {
	X.GraphTriID()

	var buf [256]byte
	return string(X.exportGraphDesc(buf[:0]))
}

func (X *graphState) GraphTriID() GraphTriID {
	if X.status < tricoded {
		X.canonize()
		X.triID = X.exportEncoding(X.triID[:0], IncludeSignModes)
		X.status = tricoded
	}

	return X.triID
}

func (X *graphState) exportEncoding(io []byte, opts TriGraphEncoderOpts) (TriID []byte) {

	Xg := X.VtxGroups()

	// Gn - Vtx Family Group cardinality
	io = append(io, byte(len(Xg)))

	for _, g := range Xg {
		for ei := range g.edges {
			io = append(io, g.familyEdgeOrd(ei))
		}
	}

	for _, gi := range Xg {
		io = append(io, byte(gi.CyclesID))
	}

	for _, gi := range Xg {
		io = append(io, byte(gi.TriSign()))
	}

	for _, gi := range Xg {
		io = append(io, gi.count)
	}

	//io = append(io, []byte(X.CGE)...)
	return io
}

func (X *graphState) exportGraphDesc(io []byte) []byte {
	//b := strings.Builder{}

	//var scrap [32]byte

	// Gn - Group count
	//fmt.Fprintf(&buf, "%d:", len(X.Groups))
	//io = append(io, byte('0' + len(X.Groups)))

	Xg := X.VtxGroups()

	for i, gi := range Xg {
		//b.Reset()
		if i > 0 {
			io = append(io, ' ')
		}
		// numSigns := gi.SignCardinality()
		// if numSigns > 1 {
		// 	io = append(io, '(')
		// }

		//for i, ci := range gi.Counts {

		// if numSigns > 1 {
		// 	io = append(io, ')')
		// }
		{
			N := gi.count
			if N >= 10 {
				io = append(io, '0'+byte(N/10))
			}
			if N == 1 {
				io = append(io, ' ')
			} else {
				io = append(io, '0'+byte(N%10))
			}
		}

		for _, ei := range gi.edges {
			c := byte('?')
			switch {
			case ei.isLoop && ei.edgeSign > 0:
				c = '*'
			case ei.isLoop && ei.edgeSign < 0:
				c = '^'
			case ei.edgeSign > 0:
				c = '+'
			case ei.edgeSign < 0:
				c = '-'
			}
			io = append(io, c)
		}

		io = gi.AppendEdgesLabel(io)

		//io = append(io, b.String()...)
	}

	return io
}

/*
func (X *graphState) computeUID() {
	if X.status >= uidComputed {
		return
	}

	X.canonize()

	{
		hasher := splitMix{}
		hasher.Reset()

		{
			Nv := X.vtxCount
			for _, vi := range X.vtx[:Nv] {
				hasher.Write(int64(vi.groupID))
				for _, e := range vi.edges {
					hasher.Write(int64(e.edgeCode))
				}
				// for ci := int32(0); ci < Nv; ci++ {
				// 	// Open mystery: removing [0:3]input (below) decreases total graph count as expected, but it also decreases the unique Traces count!?!
				// 	// The unique Traces count of a complete catalog should be *unaffected* by GraphUID choices and conventions, no!?
				// 	hasher.Write(vi.traces[ci].cycles)
				// 	hasher.Write(vi.traces[ci].input[0])
				// 	hasher.Write(vi.traces[ci].input[1])
				// 	hasher.Write(vi.traces[ci].input[2])
				// 	hasher.Write(vi.traces[ci].extSum)
				// 	hasher.Write(vi.VtxType())
				// }
			}
		}

		hash := hasher.Sum()
		copy(X.uid[:], hash[:])
		X.status = uidComputed
	}
}
*/

var labels = []string{
	//	"FAMILY ID    ",
	"FAMILY EDGES ",
	"CYCLES ID    ",
	"SIGNS        ",
	"COUNT        ",
}

func (X *graphState) PrintTriCodes(out io.Writer) {
	X.canonize()

	Xg := X.VtxGroups()

	var buf [256]byte
	var groupTri, thisTri [3]byte

	// Vertex type str
	for line := 0; line < 4; line++ {
		fmt.Fprintf(out, "\n   %s  ", labels[line])

		for vi := 0; vi < len(Xg); {

			// Calc col width based on grouping
			groupCols := 1
			Xg[vi].printEdges(groupTri[:])
			for gi := vi + 1; gi < len(Xg); gi++ {
				Xg[gi].printEdges(thisTri[:])
				if thisTri != groupTri {
					break
				}
				groupCols++
			}
			col_ := buf[:3*groupCols+5*groupCols]
			col := col_[:len(col_)-5]
			for i := range col_ {
				col_[i] = ' '
			}

			v := Xg[vi]
			c := byte('?')

			switch line {

			//case 0: // FAMILY ID
			// case 1: // EDGES  MASK
			// 	for gi := 0; gi < groupCols; gi++ {
			// 		v = Xg[vi+gi]
			// 		for ii, e := range v.edges {
			// 			if e.isLoop || e.srcGroup == v.CyclesID {
			// 				c = 'o'
			// 			} else {
			// 				c = 'A' + byte(e.srcGroup) - 1
			// 			}
			// 			col[gi*8+ii] = c
			// 		}
			// 	}
			case 0: // FAMILY EDGES
				center := (len(col) - 3) / 2
				v.printEdges(col[center:])
			case 1: // CYCLES ID
				for i := range col {
					col[i] = ':'
				}
				{
					for gi := 0; gi < groupCols; gi++ {
						v = Xg[vi+gi]
						c = 'A' + byte(v.CyclesID) - 1
						for j := 0; j < 3; j++ {
							col[gi*8+j] = c
						}
					}
				}

			case 2: // SIGNS
				for gi := 0; gi < groupCols; gi++ {
					v = Xg[vi+gi]

					for ei, e := range v.edges {
						if e.edgeSign < 0 {
							c = '-'
						} else {
							c = '+'
						}
						col[gi*8+ei] = c
					}
				}
			case 3: // COUNT
				for gi := 0; gi < groupCols; gi++ {
					v = Xg[vi+gi]

					NNN := v.count
					for j := 2; j >= 0; j-- {
						col[gi*8+j] = '0' + byte(NNN%10)
						NNN /= 10
					}
				}
			}
			out.Write(col_)
			vi += groupCols
		}
	}

}

func (X *graphState) PrintCycleSpectrum(out io.Writer) {
	X.canonize()

	Nv := X.vtxCount
	Xg := X.VtxGroups()

	for ci := int32(0); ci < Nv; ci++ {
		fmt.Fprintf(out, "\n   T%d:%-7d", ci+1, X.traces[ci])
		for _, vi := range Xg {
			fmt.Fprintf(out, "%8d", vi.cycles[ci])
		}
	}
}

func (X *graphState) Traces(numTraces int) Traces {
	if numTraces <= 0 {
		numTraces = int(X.vtxCount)
	}

	X.calcUpTo(int32(numTraces))
	return X.traces[:numTraces]
}

// AP Hash Function
// https://www.partow.net/programming/hashfunctions/#AvailableHashFunctions
func APHash64(buf []byte) uint64 {
	var hash uint64 = 0xaaaaaaaaaaaaaaaa
	for i, b := range buf {
		if (i & 1) == 0 {
			hash ^= ((hash << 7) ^ uint64(b) ^ (hash >> 3))
		} else {
			hash ^= (^((hash << 11) ^ uint64(b) ^ (hash >> 5)) + 1)
		}
	}
	return hash
}

func writeU64(buf []byte, val uint64) {
	buf[0] = byte((val) & 0xFF)
	buf[1] = byte((val >> 8) & 0xFF)
	buf[2] = byte((val >> 16) & 0xFF)
	buf[3] = byte((val >> 24) & 0xFF)
	buf[4] = byte((val >> 32) & 0xFF)
	buf[5] = byte((val >> 40) & 0xFF)
	buf[6] = byte((val >> 48) & 0xFF)
	buf[7] = byte((val >> 56) & 0xFF)
}

type splitMix struct {
	h1 uint64
	h2 uint64
}

func (mix *splitMix) Reset() {
	mix.h1 = 0xaaaaaaaaaaaaaaaa
	mix.h2 = 0
}

func (mix *splitMix) Write(val int64) {
	x := mix.h1 ^ mix.h2

	// https://github.com/skeeto/hash-prospector
	x1 := x
	x1 = x1 + 0x9e3779b97f4a7c15 + uint64(val)
	x1 ^= (x1 >> 30)
	x1 *= 0xbf58476d1ce4e5b9
	x1 ^= (x1 >> 27)
	x1 *= 0x94d049bb133111eb
	x1 ^= (x1 >> 31)
	mix.h1 = x1

	// https://gist.github.com/badboy/6267743
	x2 := x
	x2 = (^x2) + (x2 << 21) // x2 = (x2 << 21) - x2 - 1;
	x2 = x2 ^ (x2 >> 24)
	x2 = (x2 + (x2 << 3)) + (x2 << 8) // x2 * 265
	x2 = x2 ^ (x2 >> 14)
	x2 = (x2 + (x2 << 2)) + (x2 << 4) // x2 * 21
	x2 = x2 ^ (x2 >> 28)
	x2 = x2 + (x2 << 31)
	mix.h2 = x2
}

func (mix *splitMix) Sum() [16]byte {
	var hash [16]byte

	writeU64(hash[:8], mix.h1)
	writeU64(hash[8:16], mix.h2)

	return hash
}
