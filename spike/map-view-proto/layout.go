// PROTOTYPE, throwaway. Two layout engines over the same Box/Edge output:
// Graphviz `dot` (positions and edge splines only) and a hand-rolled layered layout.
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type P struct{ X, Y float64 }

type Box struct{ X, Y, W, H float64 }

func (b Box) C() P { return P{b.X + b.W/2, b.Y + b.H/2} }

type Edge struct {
	From, To int
	Pts      []P  // cubic control points: P0, (C1, C2, P)...
	Faded    bool // blocker is closed
}

type Lane struct {
	State State
	Y, H  float64
}

type Layout struct {
	Boxes map[int]Box
	Edges []Edge
	W, H  float64
	Lanes []Lane
}

const margin = 16

func nodeSize(look string) (w, h float64) {
	switch look {
	case "compact":
		return 56, 62
	case "lanes":
		return 190, 34
	}
	return 230, 66
}

func gaps(look string, tb bool) (main, cross float64) {
	switch {
	case look == "compact":
		return 46, 18
	case tb:
		return 56, 22
	}
	return 70, 20
}

// ---- hand-rolled layered layout ----

func orderLayers(m *Map) [][]int {
	L := m.layers()
	maxL := 0
	for _, v := range L {
		maxL = max(maxL, v)
	}
	layers := make([][]int, maxL+1)
	for _, n := range m.Order {
		layers[L[n]] = append(layers[L[n]], n)
	}
	pos := map[int]float64{}
	reindex := func() {
		for _, ly := range layers {
			for i, n := range ly {
				pos[n] = float64(i) - float64(len(ly)-1)/2
			}
		}
	}
	reindex()
	bary := func(ly []int, nbrs func(int) []int) {
		key := map[int]float64{}
		for _, n := range ly {
			sum, c := 0.0, 0
			for _, x := range nbrs(n) {
				sum += pos[x]
				c++
			}
			if c == 0 {
				key[n] = pos[n]
			} else {
				key[n] = sum / float64(c)
			}
		}
		sort.SliceStable(ly, func(i, j int) bool { return key[ly[i]] < key[ly[j]] })
	}
	up := func(n int) (out []int) {
		for _, b := range m.Tickets[n].Blockers {
			if bt, ok := m.inMap(b); ok {
				out = append(out, bt.Num)
			}
		}
		return
	}
	down := func(n int) []int { return m.Tickets[n].Blocks }
	for it := 0; it < 4; it++ {
		for i := 1; i < len(layers); i++ {
			bary(layers[i], up)
			reindex()
		}
		for i := len(layers) - 2; i >= 0; i-- {
			bary(layers[i], down)
			reindex()
		}
	}
	return layers
}

func connect(m *Map, boxes map[int]Box, tb bool) []Edge {
	var es []Edge
	for _, n := range m.Order {
		t := m.Tickets[n]
		for _, b := range t.Blockers {
			bt, ok := m.inMap(b)
			if !ok {
				continue
			}
			f, to := boxes[bt.Num], boxes[n]
			var p0, p3 P
			if tb {
				p0, p3 = P{f.X + f.W/2, f.Y + f.H}, P{to.X + to.W/2, to.Y}
			} else {
				p0, p3 = P{f.X + f.W, f.Y + f.H/2}, P{to.X, to.Y + to.H/2}
			}
			var c1, c2 P
			if tb {
				d := (p3.Y - p0.Y) / 2
				c1, c2 = P{p0.X, p0.Y + d}, P{p3.X, p3.Y - d}
			} else {
				d := (p3.X - p0.X) / 2
				c1, c2 = P{p0.X + d, p0.Y}, P{p3.X - d, p3.Y}
			}
			es = append(es, Edge{bt.Num, n, []P{p0, c1, c2, p3}, b.Closed})
		}
	}
	return es
}

func layeredLayout(m *Map, look string, tb bool) *Layout {
	w, h := nodeSize(look)
	gm, gc := gaps(look, tb)
	layers := orderLayers(m)
	maxCount := 0
	for _, ly := range layers {
		maxCount = max(maxCount, len(ly))
	}
	boxes := map[int]Box{}
	for li, ly := range layers {
		off := float64(maxCount-len(ly)) / 2
		for i, n := range ly {
			if tb {
				boxes[n] = Box{margin + (float64(i)+off)*(w+gc), margin + float64(li)*(h+gm), w, h}
			} else {
				boxes[n] = Box{margin + float64(li)*(w+gm), margin + (float64(i)+off)*(h+gc), w, h}
			}
		}
	}
	lay := &Layout{Boxes: boxes, Edges: connect(m, boxes, tb)}
	if tb {
		lay.W = 2*margin + float64(maxCount)*(w+gc) - gc
		lay.H = 2*margin + float64(len(layers))*(h+gm) - gm
	} else {
		lay.W = 2*margin + float64(len(layers))*(w+gm) - gm
		lay.H = 2*margin + float64(maxCount)*(h+gc) - gc
	}
	return lay
}

// ---- lanes: x = dependency depth, y = state swimlane ----

var laneOrder = []State{Frontier, Claimed, Blocked, Done}

const laneGutter = 92

func lanesLayout(m *Map) *Layout {
	w, h := nodeSize("lanes")
	gm, gc := 44.0, 10.0
	layers := orderLayers(m)
	lay := &Layout{Boxes: map[int]Box{}}
	y := float64(margin)
	for _, st := range laneOrder {
		stack := map[int]int{}
		rows := 0
		for li, ly := range layers {
			for _, n := range ly {
				if m.Tickets[n].State != st {
					continue
				}
				lay.Boxes[n] = Box{laneGutter + float64(li)*(w+gm), y + gc + float64(stack[li])*(h+gc), w, h}
				stack[li]++
				rows = max(rows, stack[li])
			}
		}
		lh := float64(max(rows, 1))*(h+gc) + gc
		lay.Lanes = append(lay.Lanes, Lane{st, y, lh})
		y += lh + 4
	}
	lay.Edges = connect(m, lay.Boxes, false)
	lay.W = laneGutter + float64(len(layers))*(w+gm) - gm + margin
	lay.H = y - 4 + margin
	return lay
}

// ---- Graphviz dot: positions and splines only, we draw everything ----

func dotLayout(m *Map, look string, tb bool) (*Layout, error) {
	w, h := nodeSize(look)
	gm, gc := gaps(look, tb)
	var src bytes.Buffer
	dir := "LR"
	if tb {
		dir = "TB"
	}
	fmt.Fprintf(&src, "digraph G { rankdir=%s; nodesep=%.3f; ranksep=%.3f; splines=spline;\n", dir, gc/72, gm/72)
	fmt.Fprintf(&src, " node [shape=box, fixedsize=true, width=%.3f, height=%.3f, label=\"\"];\n", w/72, h/72)
	for _, n := range m.Order {
		fmt.Fprintf(&src, " n%d;\n", n)
	}
	faded := map[[2]int]bool{}
	for _, n := range m.Order {
		for _, b := range m.Tickets[n].Blockers {
			if bt, ok := m.inMap(b); ok {
				fmt.Fprintf(&src, " n%d -> n%d;\n", bt.Num, n)
				faded[[2]int{bt.Num, n}] = b.Closed
			}
		}
	}
	src.WriteString("}\n")
	cmd := exec.Command("dot", "-Tplain")
	cmd.Stdin = &src
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("dot: %w", err)
	}
	lay := &Layout{Boxes: map[int]Box{}}
	var gh float64
	num := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v * 72 }
	id := func(s string) int { v, _ := strconv.Atoi(strings.TrimPrefix(s, "n")); return v }
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "graph":
			lay.W, gh = num(f[2]), num(f[3])
		case "node":
			x, y, bw, bh := num(f[2]), num(f[3]), num(f[4]), num(f[5])
			lay.Boxes[id(f[1])] = Box{x - bw/2 + margin, gh - y - bh/2 + margin, bw, bh}
		case "edge":
			from, to := id(f[1]), id(f[2])
			k, _ := strconv.Atoi(f[3])
			pts := make([]P, k)
			for i := range k {
				pts[i] = P{num(f[4+2*i]) + margin, gh - num(f[5+2*i]) + margin}
			}
			lay.Edges = append(lay.Edges, Edge{from, to, pts, faded[[2]int{from, to}]})
		}
	}
	lay.W += 2 * margin
	lay.H = gh + 2*margin
	return lay, nil
}
