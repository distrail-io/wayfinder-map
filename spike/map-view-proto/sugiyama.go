// PROTOTYPE, throwaway: wayfinder-map#6 option B. Our own layered (Sugiyama) layout
// aiming for the `dot` look with zero dependencies:
//
//  1. layering: longest path, then pull each node toward the side with more edges
//     (a cheap stand-in for dot's network simplex: shorter edges overall)
//  2. dummy nodes on every edge spanning more than one layer, so long edges get
//     their own slot in each layer they cross and never pass through a card
//  3. ordering: weighted-median sweeps plus adjacent-swap transpose, keeping the
//     order with the fewest crossings (what dot's mincross does)
//  4. cross-axis coordinates: weighted least squares toward neighbours under
//     separation constraints (dummy chains weighted heavily so they run straight)
//  5. edges: smooth cubic pieces through the dummy slots, with tangents along
//     the main axis
package main

import (
	"math"
	"sort"
)

type sgNode struct {
	ticket   int // ticket number, 0 for dummies
	layer    int
	idx      int     // index within its layer
	pos      float64 // cross-axis centre
	size     float64 // cross-axis size
	up, down []int   // neighbour node ids in the previous / next layer
}

type sgEdge struct {
	from, to int // ticket numbers
	chain    []int
	closed   bool
}

func (n *sgNode) dummy() bool { return n.ticket == 0 }

func sugiyamaLayout(m *Map, look string, tb bool) *Layout {
	w, h := nodeSize(look)
	gm, gc := gaps(look, tb)
	mainSize, crossSize := w, h
	if tb {
		mainSize, crossSize = h, w
	}
	const dummySize = 8.0

	// ---- 1. layering ----
	L := m.layers()
	preds, succs := map[int][]int{}, map[int][]int{}
	var edges []sgEdge
	for _, n := range m.Order {
		for _, b := range m.Tickets[n].Blockers {
			if bt, ok := m.inMap(b); ok {
				preds[n] = append(preds[n], bt.Num)
				succs[bt.Num] = append(succs[bt.Num], n)
				edges = append(edges, sgEdge{from: bt.Num, to: n, closed: b.Closed})
			}
		}
	}
	for it := 0; it < 8; it++ {
		moved := false
		for _, n := range m.Order {
			lo, hi := 0, math.MaxInt
			for _, p := range preds[n] {
				lo = max(lo, L[p]+1)
			}
			for _, s := range succs[n] {
				hi = min(hi, L[s]-1)
			}
			want := L[n]
			switch {
			case len(succs[n]) > len(preds[n]) && hi != math.MaxInt:
				want = hi
			case len(preds[n]) > len(succs[n]):
				want = lo
			}
			if want != L[n] && want >= lo && want <= hi {
				L[n], moved = want, true
			}
		}
		if !moved {
			break
		}
	}
	minL := math.MaxInt
	for _, v := range L {
		minL = min(minL, v)
	}
	maxL := 0
	for n := range L {
		L[n] -= minL
		maxL = max(maxL, L[n])
	}

	// ---- 2. nodes and dummy chains ----
	var nodes []*sgNode
	id := map[int]int{}
	layers := make([][]int, maxL+1)
	add := func(n *sgNode) int {
		nodes = append(nodes, n)
		i := len(nodes) - 1
		layers[n.layer] = append(layers[n.layer], i)
		return i
	}
	// initial order: DFS from sources in map order, so related tickets start together
	seen := map[int]bool{}
	var dfs func(n int)
	dfs = func(n int) {
		if seen[n] {
			return
		}
		seen[n] = true
		id[n] = add(&sgNode{ticket: n, layer: L[n], size: crossSize})
		for _, s := range succs[n] {
			dfs(s)
		}
	}
	for _, n := range m.Order {
		if len(preds[n]) == 0 {
			dfs(n)
		}
	}
	for _, n := range m.Order { // anything left (cycles)
		dfs(n)
	}
	for i := range edges {
		e := &edges[i]
		prev := id[e.from]
		e.chain = []int{prev}
		for l := L[e.from] + 1; l < L[e.to]; l++ {
			d := add(&sgNode{layer: l, size: dummySize})
			nodes[prev].down = append(nodes[prev].down, d)
			nodes[d].up = append(nodes[d].up, prev)
			e.chain = append(e.chain, d)
			prev = d
		}
		to := id[e.to]
		if L[e.to] > L[e.from] {
			nodes[prev].down = append(nodes[prev].down, to)
			nodes[to].up = append(nodes[to].up, prev)
		}
		e.chain = append(e.chain, to)
	}

	// ---- 3. ordering ----
	reindex := func() {
		for _, ly := range layers {
			for i, n := range ly {
				nodes[n].idx = i
			}
		}
	}
	// gapCrossings counts crossings between layer l and l+1.
	gapCrossings := func(l int) int {
		if l < 0 || l+1 >= len(layers) {
			return 0
		}
		c := 0
		var es [][2]int
		for _, u := range layers[l] {
			for _, v := range nodes[u].down {
				es = append(es, [2]int{nodes[u].idx, nodes[v].idx})
			}
		}
		for i := range es {
			for j := i + 1; j < len(es); j++ {
				if (es[i][0]-es[j][0])*(es[i][1]-es[j][1]) < 0 {
					c++
				}
			}
		}
		return c
	}
	crossings := func() int {
		c := 0
		for l := range layers {
			c += gapCrossings(l)
		}
		return c
	}
	median := func(n int, nbrs []int) float64 {
		if len(nbrs) == 0 {
			return -1
		}
		ps := make([]float64, len(nbrs))
		for i, x := range nbrs {
			ps[i] = float64(nodes[x].idx)
		}
		sort.Float64s(ps)
		k := len(ps) / 2
		if len(ps)%2 == 1 {
			return ps[k]
		}
		return (ps[k-1] + ps[k]) / 2
	}
	sweep := func(l int, downward bool) {
		ly := layers[l]
		key := make(map[int]float64, len(ly))
		var movable []int
		for _, n := range ly {
			nb := nodes[n].up
			if !downward {
				nb = nodes[n].down
			}
			key[n] = median(n, nb)
			if key[n] >= 0 {
				movable = append(movable, n)
			}
		}
		sort.SliceStable(movable, func(i, j int) bool { return key[movable[i]] < key[movable[j]] })
		k := 0
		for i, n := range ly { // nodes without neighbours keep their slot
			if key[n] >= 0 {
				ly[i] = movable[k]
				k++
			}
		}
		reindex()
	}
	transpose := func() {
		for improved := true; improved; {
			improved = false
			for l, ly := range layers {
				local := func() int { return gapCrossings(l-1) + gapCrossings(l) }
				for i := 0; i+1 < len(ly); i++ {
					before := local()
					ly[i], ly[i+1] = ly[i+1], ly[i]
					nodes[ly[i]].idx, nodes[ly[i+1]].idx = i, i+1
					if local() < before {
						improved = true
					} else {
						ly[i], ly[i+1] = ly[i+1], ly[i]
						nodes[ly[i]].idx, nodes[ly[i+1]].idx = i, i+1
					}
				}
			}
		}
	}
	reindex()
	best, bestC := snapshot(layers), crossings()
	for it := 0; it < 24 && bestC > 0; it++ {
		if it%2 == 0 {
			for l := 1; l < len(layers); l++ {
				sweep(l, true)
			}
		} else {
			for l := len(layers) - 2; l >= 0; l-- {
				sweep(l, false)
			}
		}
		transpose()
		if c := crossings(); c < bestC {
			best, bestC = snapshot(layers), c
		}
	}
	layers = best
	reindex()

	// ---- 4. cross-axis coordinates ----
	sep := func(a, b int) float64 {
		g := gc
		if nodes[a].dummy() || nodes[b].dummy() {
			g = gc / 2
		}
		return (nodes[a].size+nodes[b].size)/2 + g
	}
	for _, ly := range layers { // packed start
		x := 0.0
		for i, n := range ly {
			if i > 0 {
				x += sep(ly[i-1], n)
			}
			nodes[n].pos = x
		}
	}
	weight := func(a, b int) float64 {
		switch {
		case nodes[a].dummy() && nodes[b].dummy():
			return 8
		case nodes[a].dummy() || nodes[b].dummy():
			return 2
		}
		return 1
	}
	place := func(ly []int, useUp, useDown bool) {
		want := make([]float64, len(ly))
		wt := make([]float64, len(ly))
		for i, n := range ly {
			sum, ws := 0.0, 0.0
			if useUp {
				for _, x := range nodes[n].up {
					sum += weight(n, x) * nodes[x].pos
					ws += weight(n, x)
				}
			}
			if useDown {
				for _, x := range nodes[n].down {
					sum += weight(n, x) * nodes[x].pos
					ws += weight(n, x)
				}
			}
			if ws == 0 {
				want[i], wt[i] = nodes[n].pos, 0.01
			} else {
				want[i], wt[i] = sum/ws, ws
			}
		}
		// pool adjacent blocks until separations hold: weighted least squares
		type block struct {
			first, last int
			span, w, ws float64
		}
		var bs []block
		for i := range ly {
			bs = append(bs, block{i, i, 0, wt[i], wt[i] * want[i]})
			for len(bs) > 1 {
				a, b := bs[len(bs)-2], bs[len(bs)-1]
				s := sep(ly[a.last], ly[b.first])
				if a.ws/a.w+a.span+s <= b.ws/b.w {
					break
				}
				shift := a.span + s
				a.ws += b.ws - b.w*shift
				a.w += b.w
				a.span = shift + b.span
				a.last = b.last
				bs = append(bs[:len(bs)-2], a)
			}
		}
		for _, b := range bs {
			x := b.ws / b.w
			for i := b.first; i <= b.last; i++ {
				if i > b.first {
					x += sep(ly[i-1], ly[i])
				}
				nodes[ly[i]].pos = x
			}
		}
	}
	for it := 0; it < 40; it++ {
		switch it % 3 {
		case 0:
			for l := 1; l < len(layers); l++ {
				place(layers[l], true, false)
			}
		case 1:
			for l := len(layers) - 2; l >= 0; l-- {
				place(layers[l], false, true)
			}
		default:
			for _, ly := range layers {
				place(ly, true, true)
			}
		}
	}
	lo := math.Inf(1)
	for _, n := range nodes {
		lo = math.Min(lo, n.pos-n.size/2)
	}
	for _, n := range nodes {
		n.pos += margin - lo
	}

	// ---- main axis: one band per layer ----
	start, end := make([]float64, len(layers)), make([]float64, len(layers))
	x := float64(margin)
	for l, ly := range layers {
		th := 0.0
		for _, n := range ly {
			if !nodes[n].dummy() {
				th = mainSize
			}
		}
		start[l], end[l] = x, x+th
		x += th + gm
	}
	mainEnd := x - gm + margin

	lay := &Layout{Boxes: map[int]Box{}}
	at := func(main, cross float64) P {
		if tb {
			return P{cross, main}
		}
		return P{main, cross}
	}
	for _, n := range nodes {
		if n.dummy() {
			continue
		}
		c := at(start[n.layer], n.pos-crossSize/2)
		lay.Boxes[n.ticket] = Box{c.X, c.Y, w, h}
	}

	// ---- 5. edges: ports spread along the card side, ordered by the far end ----
	port := map[[2]int]float64{} // (node id, edge index) -> cross offset
	spread := func(n int, es []int, far func(e int) float64) {
		sort.SliceStable(es, func(i, j int) bool { return far(es[i]) < far(es[j]) })
		span := crossSize * 0.5
		for i, e := range es {
			off := 0.0
			if len(es) > 1 {
				off = -span/2 + span*float64(i)/float64(len(es)-1)
			}
			port[[2]int{n, e}] = off
		}
	}
	outs, ins := map[int][]int{}, map[int][]int{}
	for i, e := range edges {
		outs[e.chain[0]] = append(outs[e.chain[0]], i)
		ins[e.chain[len(e.chain)-1]] = append(ins[e.chain[len(e.chain)-1]], i)
	}
	for n, es := range outs {
		spread(n, es, func(e int) float64 { return nodes[edges[e].chain[1]].pos })
	}
	for n, es := range ins {
		spread(n, es, func(e int) float64 { return nodes[edges[e].chain[len(edges[e].chain)-2]].pos })
	}
	for i, e := range edges {
		f, t := e.chain[0], e.chain[len(e.chain)-1]
		if nodes[t].layer <= nodes[f].layer { // cycle or same layer: skip in the spike
			continue
		}
		pts := []P{at(end[nodes[f].layer], nodes[f].pos+port[[2]int{f, i}])}
		for _, d := range e.chain[1 : len(e.chain)-1] {
			dn := nodes[d]
			pts = append(pts, at(start[dn.layer], dn.pos), at(end[dn.layer], dn.pos))
		}
		pts = append(pts, at(start[nodes[t].layer], nodes[t].pos+port[[2]int{t, i}]))
		cps := []P{pts[0]}
		for k := 1; k < len(pts); k++ {
			a, b := pts[k-1], pts[k]
			if tb {
				d := (b.Y - a.Y) / 2
				cps = append(cps, P{a.X, a.Y + d}, P{b.X, b.Y - d}, b)
			} else {
				d := (b.X - a.X) / 2
				cps = append(cps, P{a.X + d, a.Y}, P{b.X - d, b.Y}, b)
			}
		}
		lay.Edges = append(lay.Edges, Edge{e.from, e.to, cps, e.closed})
	}

	crossEnd := 0.0
	for _, n := range nodes {
		crossEnd = math.Max(crossEnd, n.pos+n.size/2)
	}
	crossEnd += margin
	if tb {
		lay.W, lay.H = crossEnd, mainEnd
	} else {
		lay.W, lay.H = mainEnd, crossEnd
	}
	return lay
}

func snapshot(layers [][]int) [][]int {
	out := make([][]int, len(layers))
	for i, ly := range layers {
		out[i] = append([]int(nil), ly...)
	}
	return out
}
