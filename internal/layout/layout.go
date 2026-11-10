// Package layout places a directed graph's nodes in layers along a main axis,
// left to right or top to bottom, and routes its edges around them. It knows
// nothing about Wayfinder.
//
// Layered is a Sugiyama layout (ADR 0001), aiming for Graphviz dot's look:
//
//  1. Cycles are broken by reversing the edges that close them.
//  2. Layering: longest path, then each node is pulled toward the side where
//     it has more edges, a cheap stand-in for dot's network simplex.
//  3. Dummy nodes on every edge spanning more than one layer, so long edges
//     get their own slot in each layer they cross and never pass through a
//     node.
//  4. Ordering: weighted-median sweeps plus adjacent-swap transpose, keeping
//     the order with the fewest crossings.
//  5. Cross-axis positions: weighted least squares toward neighbours under
//     separation constraints, with dummy chains weighted to run straight.
//  6. Edges: smooth cubic pieces through the dummy slots, leaving and
//     arriving along the main axis, with ports spread along each node's side.
package layout

import (
	"math"
	"slices"
	"sort"
)

type Dir int

const (
	LR Dir = iota
	TB
)

func (d Dir) String() string { return [...]string{"LR", "TB"}[d] }

// Node is a box W×H, or with Round set, the circle inside a W×W box.
type Node struct {
	ID    int
	W, H  float64
	Round bool
}

type Edge struct{ From, To int }

type Graph struct {
	Nodes []Node
	Edges []Edge
}

type Point struct{ X, Y float64 }

type Rect struct{ X, Y, W, H float64 }

// Curve is a cubic Bézier path: Pts[0], then (control, control, end) triples.
// It runs from the From node's side to the To node's side. An edge to a node
// that isn't in the graph has no points.
type Curve struct {
	From, To int
	Pts      []Point
}

type Result struct {
	Boxes  map[int]Rect
	Curves []Curve // one per graph edge, in the same order
	W, H   float64
}

// Scale is how much r must be scaled to fit a w×h area, but never above
// maxScale.
func (r Result) Scale(w, h, maxScale float64) float64 {
	return math.Min(math.Min(w/r.W, h/r.H), maxScale)
}

// Translate moves every box and curve point of r by (dx, dy), and grows its
// extent by as much, so the far margins stay as they were. It changes r's
// boxes and curves in place.
func (r *Result) Translate(dx, dy float64) {
	for n, b := range r.Boxes {
		r.Boxes[n] = Rect{X: b.X + dx, Y: b.Y + dy, W: b.W, H: b.H}
	}
	for _, c := range r.Curves {
		for i, p := range c.Pts {
			c.Pts[i] = Point{X: p.X + dx, Y: p.Y + dy}
		}
	}
	r.W += dx
	r.H += dy
}

// BestFit returns the direction whose layout draws larger in a w×h area,
// given the two layouts of one graph. LR wins a tie.
func BestFit(lr, tb Result, w, h, maxScale float64) Dir {
	if tb.Scale(w, h, maxScale) > lr.Scale(w, h, maxScale) {
		return TB
	}
	return LR
}

// Margin is the space a layout leaves around its drawing.
const Margin = 16

const (
	dummySize = 8
	// orderRounds caps the median sweeps; each keeps the best order seen.
	orderRounds = 24
	placeRounds = 40
)

// gaps are the space between layers (main) and between neighbours in a
// layer (cross).
func gaps(dir Dir) (main, cross float64) {
	if dir == TB {
		return 56, 22
	}
	return 70, 20
}

// vnode is a node of the layered graph: a real node or a dummy slot on a
// long edge.
type vnode struct {
	id         int // the graph node's ID; unused for dummies
	dummy      bool
	layer, idx int     // layer, and index within it
	pos        float64 // cross-axis centre
	cross      float64 // size along the cross axis
	main       float64 // size along the main axis
	round      bool
	up, down   []int // neighbours in the layer before and after
}

// dagEdge is a graph edge as laid out: pointing forward, through a chain of
// layered nodes from its source to its target.
type dagEdge struct {
	edge     int // index into Graph.Edges
	from, to int // layered node indexes, after any reversal
	reversed bool
	chain    []int
}

type layered struct {
	dir                Dir
	mainGap, crossGap  float64
	nodes              []*vnode
	layers             [][]int
	edges              []dagEdge
	selfLoops          []int // indexes into Graph.Edges
	bandStart, bandEnd []float64
}

// Layered lays g out in layers along dir. The result is deterministic, and
// edges in a cycle are drawn against the flow, with their arrows intact.
func Layered(g Graph, dir Dir) Result {
	l := &layered{dir: dir}
	l.mainGap, l.crossGap = gaps(dir)
	index := l.addNodes(g)
	l.breakCycles(g, index)
	l.assignLayers()
	l.addDummies()
	l.order()
	l.place()
	return l.result(g, index)
}

func (l *layered) addNodes(g Graph) map[int]int {
	index := make(map[int]int, len(g.Nodes))
	for _, n := range g.Nodes {
		v := &vnode{id: n.ID, cross: n.H, main: n.W, round: n.Round}
		if l.dir == TB {
			v.cross, v.main = n.W, n.H
		}
		index[n.ID] = len(l.nodes)
		l.nodes = append(l.nodes, v)
	}
	return index
}

// breakCycles turns the edges into a DAG: a depth-first search in node order
// reverses each edge that leads back to a node on its stack. Self-loops are
// set aside, and edges to unknown nodes dropped.
func (l *layered) breakCycles(g Graph, index map[int]int) {
	out := make([][]int, len(l.nodes))
	for i, e := range g.Edges {
		f, okf := index[e.From]
		t, okt := index[e.To]
		switch {
		case !okf || !okt:
		case f == t:
			l.selfLoops = append(l.selfLoops, i)
		default:
			out[f] = append(out[f], i)
		}
	}
	const (
		unseen = iota
		open
		done
	)
	state := make([]int, len(l.nodes))
	var visit func(n int)
	visit = func(n int) {
		state[n] = open
		for _, i := range out[n] {
			t := index[g.Edges[i].To]
			if state[t] == open {
				l.edges = append(l.edges, dagEdge{edge: i, from: t, to: n, reversed: true})
				continue
			}
			l.edges = append(l.edges, dagEdge{edge: i, from: n, to: t})
			if state[t] == unseen {
				visit(t)
			}
		}
		state[n] = done
	}
	for n := range l.nodes {
		if state[n] == unseen {
			visit(n)
		}
	}
	slices.SortStableFunc(l.edges, func(a, b dagEdge) int { return a.edge - b.edge })
}

func (l *layered) assignLayers() {
	n := len(l.nodes)
	preds, succs := make([][]int, n), make([][]int, n)
	for _, e := range l.edges {
		preds[e.to] = append(preds[e.to], e.from)
		succs[e.from] = append(succs[e.from], e.to)
	}
	layer := make([]int, n)
	known := make([]bool, n)
	var depth func(v int) int
	depth = func(v int) int {
		if !known[v] {
			d := 0
			for _, p := range preds[v] {
				d = max(d, depth(p)+1)
			}
			layer[v], known[v] = d, true
		}
		return layer[v]
	}
	for v := range n {
		depth(v)
	}
	// Pull each node toward the side with more edges, within the layers its
	// neighbours leave free, so edges get shorter overall.
	for range 8 {
		moved := false
		for v := range n {
			lo, hi := 0, math.MaxInt
			for _, p := range preds[v] {
				lo = max(lo, layer[p]+1)
			}
			for _, s := range succs[v] {
				hi = min(hi, layer[s]-1)
			}
			want := layer[v]
			switch {
			case len(succs[v]) > len(preds[v]) && hi != math.MaxInt:
				want = hi
			case len(preds[v]) > len(succs[v]):
				want = lo
			}
			if want != layer[v] && want >= lo && want <= hi {
				layer[v], moved = want, true
			}
		}
		if !moved {
			break
		}
	}
	lowest := 0
	if n > 0 {
		lowest = slices.Min(layer)
	}
	for v, d := range layer {
		l.nodes[v].layer = d - lowest
	}
	deepest := -1
	for _, v := range l.nodes {
		deepest = max(deepest, v.layer)
	}
	l.layers = make([][]int, deepest+1)

	// Start from a depth-first order from the sources, so related nodes
	// start together.
	seen := make([]bool, n)
	var dfs func(v int)
	dfs = func(v int) {
		if seen[v] {
			return
		}
		seen[v] = true
		ly := l.nodes[v].layer
		l.layers[ly] = append(l.layers[ly], v)
		for _, s := range succs[v] {
			dfs(s)
		}
	}
	for v := range n {
		if len(preds[v]) == 0 {
			dfs(v)
		}
	}
}

func (l *layered) addDummies() {
	for i := range l.edges {
		e := &l.edges[i]
		prev := e.from
		e.chain = []int{prev}
		for ly := l.nodes[e.from].layer + 1; ly < l.nodes[e.to].layer; ly++ {
			d := len(l.nodes)
			l.nodes = append(l.nodes, &vnode{dummy: true, layer: ly, cross: dummySize})
			l.layers[ly] = append(l.layers[ly], d)
			l.link(prev, d)
			e.chain = append(e.chain, d)
			prev = d
		}
		l.link(prev, e.to)
		e.chain = append(e.chain, e.to)
	}
}

func (l *layered) link(a, b int) {
	l.nodes[a].down = append(l.nodes[a].down, b)
	l.nodes[b].up = append(l.nodes[b].up, a)
}

func (l *layered) reindex() {
	for _, ly := range l.layers {
		for i, v := range ly {
			l.nodes[v].idx = i
		}
	}
}

// gapCrossings counts the edge crossings between layer ly and the next, as
// inversions of the lower ends once the edges are sorted by their upper ends.
func (l *layered) gapCrossings(ly int) int {
	if ly < 0 || ly+1 >= len(l.layers) {
		return 0
	}
	tree := make([]int, len(l.layers[ly+1])+1) // Fenwick tree over lower indexes
	seen, c := 0, 0
	var lower []int
	for _, u := range l.layers[ly] {
		lower = lower[:0]
		for _, v := range l.nodes[u].down {
			lower = append(lower, l.nodes[v].idx)
		}
		slices.Sort(lower)
		for _, x := range lower {
			below := 0
			for i := x + 1; i > 0; i -= i & -i {
				below += tree[i]
			}
			c += seen - below
			for i := x + 1; i < len(tree); i += i & -i {
				tree[i]++
			}
			seen++
		}
	}
	return c
}

func (l *layered) crossings() int {
	c := 0
	for ly := range l.layers {
		c += l.gapCrossings(ly)
	}
	return c
}

func (l *layered) median(nbrs []int) float64 {
	if len(nbrs) == 0 {
		return -1
	}
	ps := make([]float64, len(nbrs))
	for i, x := range nbrs {
		ps[i] = float64(l.nodes[x].idx)
	}
	slices.Sort(ps)
	k := len(ps) / 2
	if len(ps)%2 == 1 {
		return ps[k]
	}
	return (ps[k-1] + ps[k]) / 2
}

// sweep reorders a layer by the median position of each node's neighbours
// in the layer before (downward) or after. Nodes without neighbours there
// keep their slot.
func (l *layered) sweep(ly int, downward bool) {
	layer := l.layers[ly]
	key := make(map[int]float64, len(layer))
	var movable []int
	for _, v := range layer {
		nbrs := l.nodes[v].up
		if !downward {
			nbrs = l.nodes[v].down
		}
		if key[v] = l.median(nbrs); key[v] >= 0 {
			movable = append(movable, v)
		}
	}
	sort.SliceStable(movable, func(i, j int) bool { return key[movable[i]] < key[movable[j]] })
	k := 0
	for i, v := range layer {
		if key[v] >= 0 {
			layer[i] = movable[k]
			k++
		}
	}
	l.reindex()
}

// pairCrossings counts the crossings between u's edges and v's edges, on
// both sides, when u sits just before v in their layer.
func (l *layered) pairCrossings(u, v int) int {
	c := 0
	count := func(us, vs []int) {
		for _, a := range us {
			for _, b := range vs {
				if l.nodes[a].idx > l.nodes[b].idx {
					c++
				}
			}
		}
	}
	count(l.nodes[u].up, l.nodes[v].up)
	count(l.nodes[u].down, l.nodes[v].down)
	return c
}

// transpose swaps neighbours while that removes crossings.
func (l *layered) transpose() {
	for improved := true; improved; {
		improved = false
		for _, layer := range l.layers {
			for i := 0; i+1 < len(layer); i++ {
				u, v := layer[i], layer[i+1]
				if l.pairCrossings(v, u) < l.pairCrossings(u, v) {
					layer[i], layer[i+1] = v, u
					l.nodes[v].idx, l.nodes[u].idx = i, i+1
					improved = true
				}
			}
		}
	}
}

func (l *layered) order() {
	l.reindex()
	best, fewest := clone(l.layers), l.crossings()
	for round := 0; round < orderRounds && fewest > 0; round++ {
		if round%2 == 0 {
			for ly := 1; ly < len(l.layers); ly++ {
				l.sweep(ly, true)
			}
		} else {
			for ly := len(l.layers) - 2; ly >= 0; ly-- {
				l.sweep(ly, false)
			}
		}
		l.transpose()
		if c := l.crossings(); c < fewest {
			best, fewest = clone(l.layers), c
		}
	}
	l.layers = best
	l.reindex()
}

func clone(layers [][]int) [][]int {
	out := make([][]int, len(layers))
	for i, ly := range layers {
		out[i] = slices.Clone(ly)
	}
	return out
}

// sep is the least distance between the centres of neighbours a and b.
func (l *layered) sep(a, b int) float64 {
	g := l.crossGap
	if l.nodes[a].dummy || l.nodes[b].dummy {
		g /= 2
	}
	return (l.nodes[a].cross+l.nodes[b].cross)/2 + g
}

func (l *layered) weight(a, b int) float64 {
	switch {
	case l.nodes[a].dummy && l.nodes[b].dummy:
		return 8
	case l.nodes[a].dummy || l.nodes[b].dummy:
		return 2
	}
	return 1
}

// placeLayer moves a layer's nodes toward the weighted mean of their
// neighbours, keeping their order and separation: adjacent blocks are pooled
// until the separations hold, which is weighted least squares.
func (l *layered) placeLayer(layer []int, useUp, useDown bool) {
	want := make([]float64, len(layer))
	wt := make([]float64, len(layer))
	for i, v := range layer {
		sum, ws := 0.0, 0.0
		add := func(nbrs []int) {
			for _, x := range nbrs {
				w := l.weight(v, x)
				sum += w * l.nodes[x].pos
				ws += w
			}
		}
		if useUp {
			add(l.nodes[v].up)
		}
		if useDown {
			add(l.nodes[v].down)
		}
		if ws == 0 {
			want[i], wt[i] = l.nodes[v].pos, 0.01
		} else {
			want[i], wt[i] = sum/ws, ws
		}
	}
	type block struct {
		first, last  int
		span, w, wsX float64
	}
	var bs []block
	for i := range layer {
		bs = append(bs, block{i, i, 0, wt[i], wt[i] * want[i]})
		for len(bs) > 1 {
			a, b := bs[len(bs)-2], bs[len(bs)-1]
			s := l.sep(layer[a.last], layer[b.first])
			if a.wsX/a.w+a.span+s <= b.wsX/b.w {
				break
			}
			shift := a.span + s
			a.wsX += b.wsX - b.w*shift
			a.w += b.w
			a.span = shift + b.span
			a.last = b.last
			bs = append(bs[:len(bs)-2], a)
		}
	}
	for _, b := range bs {
		x := b.wsX / b.w
		for i := b.first; i <= b.last; i++ {
			if i > b.first {
				x += l.sep(layer[i-1], layer[i])
			}
			l.nodes[layer[i]].pos = x
		}
	}
}

func (l *layered) place() {
	for _, layer := range l.layers { // packed start
		x := 0.0
		for i, v := range layer {
			if i > 0 {
				x += l.sep(layer[i-1], v)
			}
			l.nodes[v].pos = x
		}
	}
	for round := range placeRounds {
		switch round % 3 {
		case 0:
			for ly := 1; ly < len(l.layers); ly++ {
				l.placeLayer(l.layers[ly], true, false)
			}
		case 1:
			for ly := len(l.layers) - 2; ly >= 0; ly-- {
				l.placeLayer(l.layers[ly], false, true)
			}
		default:
			for _, layer := range l.layers {
				l.placeLayer(layer, true, true)
			}
		}
	}
	lo := math.Inf(1)
	for _, v := range l.nodes {
		lo = math.Min(lo, v.pos-v.cross/2)
	}
	for _, v := range l.nodes {
		v.pos += Margin - lo
	}
	// Main axis: one band per layer, as thick as its largest node.
	l.bandStart, l.bandEnd = make([]float64, len(l.layers)), make([]float64, len(l.layers))
	x := float64(Margin)
	for ly, layer := range l.layers {
		thick := 0.0
		for _, v := range layer {
			if !l.nodes[v].dummy {
				thick = max(thick, l.nodes[v].main)
			}
		}
		l.bandStart[ly], l.bandEnd[ly] = x, x+thick
		x += thick + l.mainGap
	}
}

// at turns main- and cross-axis coordinates into a point.
func (l *layered) at(main, cross float64) Point {
	if l.dir == TB {
		return Point{cross, main}
	}
	return Point{main, cross}
}

// sides returns where node v starts and ends on the main axis: centred in
// its layer's band.
func (l *layered) sides(v int) (start, end float64) {
	n := l.nodes[v]
	start = (l.bandStart[n.layer]+l.bandEnd[n.layer])/2 - n.main/2
	return start, start + n.main
}

func (l *layered) result(g Graph, index map[int]int) Result {
	res := Result{Boxes: make(map[int]Rect, len(g.Nodes)), Curves: make([]Curve, len(g.Edges))}
	for _, n := range g.Nodes {
		v := index[n.ID]
		start, _ := l.sides(v)
		p := l.at(start, l.nodes[v].pos-l.nodes[v].cross/2)
		res.Boxes[n.ID] = Rect{p.X, p.Y, n.W, n.H}
	}
	for i, e := range g.Edges {
		res.Curves[i] = Curve{From: e.From, To: e.To}
	}
	port := l.ports()
	for i, e := range l.edges {
		pts := l.route(e, port[i])
		if e.reversed {
			slices.Reverse(pts)
		}
		res.Curves[e.edge].Pts = pts
	}
	for _, i := range l.selfLoops {
		res.Curves[i].Pts = l.selfLoop(index[g.Edges[i].From])
	}

	// The extent covers every node and every curve: a Bézier stays within
	// its control points.
	res.W, res.H = 2*Margin, 2*Margin
	grow := func(x, y float64) { res.W, res.H = max(res.W, x+Margin), max(res.H, y+Margin) }
	for _, b := range res.Boxes {
		grow(b.X+b.W, b.Y+b.H)
	}
	for _, c := range res.Curves {
		for _, p := range c.Pts {
			grow(p.X, p.Y)
		}
	}
	return res
}

// ports spreads the edges leaving or entering a node along that side,
// ordered by where their other end lies, so they don't cross at the node.
// It returns each edge's cross-axis offsets at its source and target.
func (l *layered) ports() [][2]float64 {
	port := make([][2]float64, len(l.edges))
	outs, ins := map[int][]int{}, map[int][]int{}
	for i, e := range l.edges {
		outs[e.from] = append(outs[e.from], i)
		ins[e.to] = append(ins[e.to], i)
	}
	spread := func(v int, es []int, end int, far func(e int) int) {
		sort.SliceStable(es, func(i, j int) bool { return l.nodes[far(es[i])].pos < l.nodes[far(es[j])].pos })
		span := l.nodes[v].cross * 0.5
		for k, e := range es {
			if len(es) > 1 {
				port[e][end] = -span/2 + span*float64(k)/float64(len(es)-1)
			}
		}
	}
	for v, es := range outs {
		spread(v, es, 0, func(e int) int { return l.edges[e].chain[1] })
	}
	for v, es := range ins {
		spread(v, es, 1, func(e int) int { c := l.edges[e].chain; return c[len(c)-2] })
	}
	return port
}

// route runs an edge from its source's trailing side, through its dummy
// slots, to its target's leading side. Pieces that run between bands are
// curves; within a band, the path runs straight along the main axis.
func (l *layered) route(e dagEdge, port [2]float64) []Point {
	from, to := l.nodes[e.from], l.nodes[e.to]
	_, fromEnd := l.sides(e.from)
	toStart, _ := l.sides(e.to)
	fromEnd -= from.inset(port[0])
	toStart += to.inset(port[1])
	type mc struct{ main, cross float64 }
	way := []mc{{fromEnd, from.pos + port[0]}, {l.bandEnd[from.layer], from.pos + port[0]}}
	for _, d := range e.chain[1 : len(e.chain)-1] {
		dn := l.nodes[d]
		way = append(way, mc{l.bandStart[dn.layer], dn.pos}, mc{l.bandEnd[dn.layer], dn.pos})
	}
	way = append(way, mc{l.bandStart[to.layer], to.pos + port[1]}, mc{toStart, to.pos + port[1]})
	way = slices.CompactFunc(way, func(a, b mc) bool { return a.main == b.main && a.cross == b.cross })

	pts := []Point{l.at(way[0].main, way[0].cross)}
	for k := 1; k < len(way); k++ {
		a, b := way[k-1], way[k]
		d := (b.main - a.main) / 2
		pts = append(pts, l.at(a.main+d, a.cross), l.at(b.main-d, b.cross), l.at(b.main, b.cross))
	}
	return pts
}

// selfLoop leaves a node's trailing side and comes back into it.
func (l *layered) selfLoop(v int) []Point {
	n := l.nodes[v]
	_, end := l.sides(v)
	d, reach := n.cross*0.35, l.mainGap*0.45
	end -= n.inset(d)
	return []Point{
		l.at(end, n.pos-d),
		l.at(end+reach, n.pos-d-reach*0.3),
		l.at(end+reach, n.pos+d+reach*0.3),
		l.at(end, n.pos+d),
	}
}

// inset is how far inside its box's side a round node's circle lies, off
// centre along the cross axis; zero for a box.
func (n *vnode) inset(off float64) float64 {
	if !n.round {
		return 0
	}
	r := n.main / 2
	return r - math.Sqrt(max(0, r*r-off*off))
}
