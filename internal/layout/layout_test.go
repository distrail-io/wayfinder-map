package layout_test

import (
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/layout"
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
	return res
}

func TestLayeredInvariants(t *testing.T) {
	graphs := map[string]layout.Graph{"214": chain214()}
	for seed := range uint64(5) {
		graphs["random"+string(rune('0'+seed))] = randomDAG(seed, 40)
	}
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

func TestLayeredSourcesFirst(t *testing.T) {
	res := layout.Layered(chain214(), layout.LR)
	if a, b := res.Boxes[215].X, res.Boxes[216].X; a != b {
		t.Errorf("sources 215 and 216 are in different layers: x=%v and x=%v", a, b)
	}
	if a, b := res.Boxes[216].X, res.Boxes[222].X; a >= b {
		t.Errorf("sink 222 (x=%v) is not after source 216 (x=%v)", b, a)
	}
}

func TestLayeredCyclesTerminate(t *testing.T) {
	g := layout.Graph{
		Nodes: []layout.Node{{ID: 1, W: 50, H: 20}, {ID: 2, W: 50, H: 20}, {ID: 3, W: 50, H: 20}},
		Edges: []layout.Edge{{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 1}, {From: 2, To: 2}},
	}
	checkInvariants(t, g, layout.LR)
}

func TestLayeredEmpty(t *testing.T) {
	res := layout.Layered(layout.Graph{}, layout.LR)
	if len(res.Boxes) != 0 || res.W <= 0 || res.H <= 0 {
		t.Errorf("empty graph gave %+v", res)
	}
}
