// Package layout places a directed graph's nodes in layers along a main axis,
// left to right or top to bottom. It knows nothing about Wayfinder.
//
// Layered is the prototype's naive layered layout; the Sugiyama layout from
// ADR 0001 replaces it.
package layout

import (
	"slices"
	"sort"
)

type Dir int

const (
	LR Dir = iota
	TB
)

func (d Dir) String() string { return [...]string{"LR", "TB"}[d] }

type Node struct {
	ID   int
	W, H float64
}

type Edge struct{ From, To int }

type Graph struct {
	Nodes []Node
	Edges []Edge
}

type Point struct{ X, Y float64 }

type Rect struct{ X, Y, W, H float64 }

// Curve is a cubic Bézier path: Pts[0], then (control, control, end) triples.
type Curve struct {
	From, To int
	Pts      []Point
}

type Result struct {
	Boxes  map[int]Rect
	Curves []Curve // one per graph edge, in the same order
	W, H   float64
}

const (
	margin   = 16
	mainGap  = 70
	crossGap = 20
)

// Layered puts each node one layer after its deepest predecessor, orders each
// layer by barycentre sweeps, and centres the layers on the cross axis.
func Layered(g Graph, dir Dir) Result {
	var slotW, slotH float64
	for _, n := range g.Nodes {
		slotW, slotH = max(slotW, n.W), max(slotH, n.H)
	}
	mainSize, crossSize := slotW, slotH
	if dir == TB {
		mainSize, crossSize = slotH, slotW
	}
	layers := order(g, depths(g))
	widest := 0
	for _, ly := range layers {
		widest = max(widest, len(ly))
	}

	res := Result{Boxes: make(map[int]Rect, len(g.Nodes))}
	size := make(map[int]Node, len(g.Nodes))
	for _, n := range g.Nodes {
		size[n.ID] = n
	}
	for li, ly := range layers {
		off := float64(widest-len(ly)) / 2
		for i, id := range ly {
			n := size[id]
			main := margin + float64(li)*(mainSize+mainGap)
			cross := margin + (float64(i)+off)*(crossSize+crossGap)
			if dir == TB {
				res.Boxes[id] = Rect{cross + (slotW-n.W)/2, main + (slotH-n.H)/2, n.W, n.H}
			} else {
				res.Boxes[id] = Rect{main + (slotW-n.W)/2, cross + (slotH-n.H)/2, n.W, n.H}
			}
		}
	}
	mainLen := 2*margin + float64(len(layers))*(mainSize+mainGap) - mainGap
	crossLen := 2*margin + float64(widest)*(crossSize+crossGap) - crossGap
	mainLen, crossLen = max(mainLen, 2*margin), max(crossLen, 2*margin)
	res.W, res.H = mainLen, crossLen
	if dir == TB {
		res.W, res.H = crossLen, mainLen
	}
	for _, e := range g.Edges {
		res.Curves = append(res.Curves, connect(res.Boxes[e.From], res.Boxes[e.To], e, dir))
	}
	return res
}

// depths gives each node its longest-path layer, ignoring the edges that
// close a cycle (found by depth-first search in node order).
func depths(g Graph) map[int]int {
	preds := map[int][]int{}
	for _, e := range g.Edges {
		preds[e.To] = append(preds[e.To], e.From)
	}
	depth := make(map[int]int, len(g.Nodes))
	onStack := map[int]bool{}
	var visit func(id int) int
	visit = func(id int) int {
		if d, ok := depth[id]; ok {
			return d
		}
		onStack[id] = true
		d := 0
		for _, p := range preds[id] {
			if onStack[p] { // closes a cycle
				continue
			}
			d = max(d, visit(p)+1)
		}
		onStack[id] = false
		depth[id] = d
		return d
	}
	for _, n := range g.Nodes {
		visit(n.ID)
	}
	return depth
}

// order groups nodes into layers, then reorders each layer by the mean
// position of its neighbours in the layer before (and after, sweeping back).
func order(g Graph, depth map[int]int) [][]int {
	if len(g.Nodes) == 0 {
		return nil
	}
	deepest := 0
	for _, d := range depth {
		deepest = max(deepest, d)
	}
	layers := make([][]int, deepest+1)
	for _, n := range g.Nodes {
		layers[depth[n.ID]] = append(layers[depth[n.ID]], n.ID)
	}
	up, down := map[int][]int{}, map[int][]int{}
	for _, e := range g.Edges {
		if depth[e.From] < depth[e.To] {
			up[e.To] = append(up[e.To], e.From)
			down[e.From] = append(down[e.From], e.To)
		}
	}
	pos := map[int]float64{}
	reindex := func() {
		for _, ly := range layers {
			for i, id := range ly {
				pos[id] = float64(i) - float64(len(ly)-1)/2
			}
		}
	}
	bary := func(ly []int, nbrs map[int][]int) {
		key := make(map[int]float64, len(ly))
		for _, id := range ly {
			key[id] = pos[id]
			if ns := nbrs[id]; len(ns) > 0 {
				sum := 0.0
				for _, x := range ns {
					sum += pos[x]
				}
				key[id] = sum / float64(len(ns))
			}
		}
		sort.SliceStable(ly, func(i, j int) bool { return key[ly[i]] < key[ly[j]] })
	}
	reindex()
	for range 4 {
		for i := 1; i < len(layers); i++ {
			bary(layers[i], up)
			reindex()
		}
		for i := len(layers) - 2; i >= 0; i-- {
			bary(layers[i], down)
			reindex()
		}
	}
	return slices.Clip(layers)
}

// connect runs a curve from the side of one box to the facing side of the
// other, leaving and arriving along the main axis.
func connect(f, t Rect, e Edge, dir Dir) Curve {
	var p0, p3 Point
	if dir == TB {
		p0, p3 = Point{f.X + f.W/2, f.Y + f.H}, Point{t.X + t.W/2, t.Y}
		d := (p3.Y - p0.Y) / 2
		return Curve{e.From, e.To, []Point{p0, {p0.X, p0.Y + d}, {p3.X, p3.Y - d}, p3}}
	}
	p0, p3 = Point{f.X + f.W, f.Y + f.H/2}, Point{t.X, t.Y + t.H/2}
	d := (p3.X - p0.X) / 2
	return Curve{e.From, e.To, []Point{p0, {p0.X + d, p0.Y}, {p3.X - d, p3.Y}, p3}}
}
