package layout_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// chain214 is map #214's shape: 215 and 216 are sources, 222 is the sink.
func chain214() layout.Graph {
	g := layout.Graph{}
	for n := 215; n <= 222; n++ {
		g.Nodes = append(g.Nodes, layout.Node{ID: n, W: 120, H: 40})
	}
	for _, e := range [][2]int{{216, 217}, {216, 218}, {218, 219}, {216, 219}, {219, 220}, {218, 220}, {217, 220}, {215, 220}, {220, 221}, {215, 221}, {221, 222}} {
		g.Edges = append(g.Edges, layout.Edge{From: e[0], To: e[1]})
	}
	return g
}

// fixture is a real map's graph, with the card sizes draw uses.
func fixture(t testing.TB, number int) layout.Graph {
	t.Helper()
	raw, err := os.ReadFile(fmt.Sprintf("../../fixtures/map-%d.json", number))
	if err != nil {
		t.Fatal(err)
	}
	m, err := wayfinder.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return draw.Graph(m)
}

func randomDAG(seed uint64, n int) layout.Graph {
	r := rand.New(rand.NewPCG(seed, 1))
	g := layout.Graph{}
	for i := 1; i <= n; i++ {
		g.Nodes = append(g.Nodes, layout.Node{ID: i, W: 120, H: 40})
		for k := r.IntN(4); k > 0 && i > 1; k-- {
			g.Edges = append(g.Edges, layout.Edge{From: 1 + r.IntN(i-1), To: i})
		}
	}
	return g
}

func overlaps(a, b layout.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func checkInvariants(t *testing.T, g layout.Graph, dir layout.Dir) layout.Result {
	t.Helper()
	res := layout.Layered(g, dir)
	if len(res.Boxes) != len(g.Nodes) {
		t.Fatalf("%d boxes for %d nodes", len(res.Boxes), len(g.Nodes))
	}
	for _, a := range g.Nodes {
		ra := res.Boxes[a.ID]
		if ra.W != a.W || ra.H != a.H {
			t.Errorf("node %d is %vx%v, want %vx%v", a.ID, ra.W, ra.H, a.W, a.H)
		}
		if ra.X < 0 || ra.Y < 0 || ra.X+ra.W > res.W || ra.Y+ra.H > res.H {
			t.Errorf("node %d at %+v lies outside %vx%v", a.ID, ra, res.W, res.H)
		}
		for _, b := range g.Nodes {
			if a.ID < b.ID && overlaps(ra, res.Boxes[b.ID]) {
				t.Errorf("nodes %d and %d overlap", a.ID, b.ID)
			}
		}
	}
	if len(res.Curves) != len(g.Edges) {
		t.Fatalf("%d curves for %d edges", len(res.Curves), len(g.Edges))
	}
	for i, c := range res.Curves {
		e := g.Edges[i]
		if c.From != e.From || c.To != e.To || len(c.Pts) < 4 || len(c.Pts)%3 != 1 {
			t.Fatalf("curve %d is %d→%d with %d points, for edge %d→%d", i, c.From, c.To, len(c.Pts), e.From, e.To)
		}
		from, to := res.Boxes[e.From], res.Boxes[e.To]
		// No edge joins two nodes in one layer, even in a cycle.
		sameLayer := from.X < to.X+to.W && to.X < from.X+from.W
		if dir == layout.TB {
			sameLayer = from.Y < to.Y+to.H && to.Y < from.Y+from.H
		}
		if e.From != e.To && sameLayer {
			t.Errorf("edge %d→%d joins two nodes in one layer", e.From, e.To)
		}
		if p := c.Pts[0]; !onEdge(p, from) {
			t.Errorf("edge %d→%d starts at %v, off its card %+v", e.From, e.To, p, from)
		}
		if p := c.Pts[len(c.Pts)-1]; !onEdge(p, to) {
			t.Errorf("edge %d→%d ends at %v, off its card %+v", e.From, e.To, p, to)
		}
		for _, p := range sample(c.Pts) {
			if p.X < 0 || p.Y < 0 || p.X > res.W || p.Y > res.H {
				t.Errorf("edge %d→%d leaves the %vx%v layout at %v", e.From, e.To, res.W, res.H, p)
				break
			}
			for _, n := range g.Nodes {
				if b := res.Boxes[n.ID]; n.ID != e.From && n.ID != e.To && inside(p, b) {
					t.Errorf("edge %d→%d passes through node %d at %v", e.From, e.To, n.ID, p)
				}
			}
		}
	}
	return res
}

// inside reports whether p is in b, by more than half a unit.
func inside(p layout.Point, b layout.Rect) bool {
	return p.X > b.X+0.5 && p.X < b.X+b.W-0.5 && p.Y > b.Y+0.5 && p.Y < b.Y+b.H-0.5
}

func onEdge(p layout.Point, b layout.Rect) bool {
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }
	within := p.X >= b.X-0.01 && p.X <= b.X+b.W+0.01 && p.Y >= b.Y-0.01 && p.Y <= b.Y+b.H+0.01
	return within && (near(p.X, b.X) || near(p.X, b.X+b.W) || near(p.Y, b.Y) || near(p.Y, b.Y+b.H))
}

// sample walks a cubic Bézier path in small steps.
func sample(pts []layout.Point) []layout.Point {
	var out []layout.Point
	for i := 1; i+2 < len(pts); i += 3 {
		p0, c1, c2, p3 := pts[i-1], pts[i], pts[i+1], pts[i+2]
		for k := range 65 {
			t := float64(k) / 64
			u := 1 - t
			out = append(out, layout.Point{
				X: u*u*u*p0.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*p3.X,
				Y: u*u*u*p0.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*p3.Y,
			})
		}
	}
	return out
}

func TestLayeredInvariants(t *testing.T) {
	graphs := map[string]layout.Graph{"214": chain214()}
	for _, n := range []int{152, 175, 214} {
		graphs[fmt.Sprint("map-", n)] = fixture(t, n)
	}
	for seed := range uint64(8) {
		graphs[fmt.Sprint("random40-", seed)] = randomDAG(seed, 40)
	}
	graphs["random100"] = randomDAG(99, 100)
	for name, g := range graphs {
		for _, dir := range []layout.Dir{layout.LR, layout.TB} {
			res := checkInvariants(t, g, dir)
			// A blocker sits strictly before what it blocks along the main axis.
			for _, e := range g.Edges {
				from, to := res.Boxes[e.From], res.Boxes[e.To]
				before := from.X+from.W <= to.X
				if dir == layout.TB {
					before = from.Y+from.H <= to.Y
				}
				if !before {
					t.Errorf("%s %v: %d is not before %d", name, dir, e.From, e.To)
				}
			}
			if again := layout.Layered(g, dir); !reflect.DeepEqual(res, again) {
				t.Errorf("%s %v: layout is not deterministic", name, dir)
			}
		}
	}
}

// A node that blocks little sits next to what it blocks, not with the other
// sources, so its edges stay short, as in dot.
func TestLayeredPullsNodesNextToTheirNeighbours(t *testing.T) {
	res := layout.Layered(chain214(), layout.LR)
	x215, x220 := res.Boxes[215].X, res.Boxes[220].X
	for id, b := range res.Boxes {
		if b.X > x215 && b.X < x220 {
			t.Errorf("node %d's layer (x=%v) lies between 215 (x=%v) and 220 (x=%v), which it blocks", id, b.X, x215, x220)
		}
	}
	if a, b := res.Boxes[216].X, res.Boxes[222].X; a >= b {
		t.Errorf("sink 222 (x=%v) is not after source 216 (x=%v)", b, a)
	}
}

func TestLayeredCyclesAndSelfLoops(t *testing.T) {
	g := layout.Graph{
		Nodes: []layout.Node{{ID: 1, W: 50, H: 20}, {ID: 2, W: 50, H: 20}, {ID: 3, W: 50, H: 20}, {ID: 4, W: 50, H: 20}},
		Edges: []layout.Edge{{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 1}, {From: 2, To: 2}, {From: 3, To: 4}, {From: 4, To: 4}},
	}
	for _, dir := range []layout.Dir{layout.LR, layout.TB} {
		res := checkInvariants(t, g, dir)
		if again := layout.Layered(g, dir); !reflect.DeepEqual(res, again) {
			t.Errorf("%v: layout of a cycle is not deterministic", dir)
		}
	}
}

func TestLayeredDuplicateEdges(t *testing.T) {
	g := layout.Graph{
		Nodes: []layout.Node{{ID: 1, W: 50, H: 20}, {ID: 2, W: 50, H: 20}, {ID: 3, W: 50, H: 20}},
		Edges: []layout.Edge{{From: 1, To: 3}, {From: 1, To: 3}, {From: 1, To: 2}, {From: 2, To: 3}},
	}
	checkInvariants(t, g, layout.LR)
}

// The plan's budget: GitHub's cap of 100 tickets lays out in under 50 ms.
func TestLayered100InBudget(t *testing.T) {
	if raceEnabled || testing.Short() {
		t.Skip("timing is meaningless under -race or -short")
	}
	g := randomDAG(42, 100)
	r := testing.Benchmark(func(b *testing.B) {
		for b.Loop() {
			layout.Layered(g, layout.LR)
		}
	})
	if took := time.Duration(r.NsPerOp()); took > 50*time.Millisecond {
		t.Errorf("100 tickets took %v, want under 50ms", took)
	}
}

func BenchmarkLayered100(b *testing.B) {
	g := randomDAG(42, 100)
	for _, dir := range []layout.Dir{layout.LR, layout.TB} {
		b.Run(dir.String(), func(b *testing.B) {
			for b.Loop() {
				layout.Layered(g, dir)
			}
		})
	}
}

func TestLayeredEmpty(t *testing.T) {
	res := layout.Layered(layout.Graph{}, layout.LR)
	if len(res.Boxes) != 0 || res.W <= 0 || res.H <= 0 {
		t.Errorf("empty graph gave %+v", res)
	}
}
