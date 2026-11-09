package wayfinder_test

import (
	"slices"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

func numbers(m *wayfinder.Map) []int {
	var out []int
	for _, t := range m.Tickets {
		out = append(out, t.Number)
	}
	return out
}

// The mini-map is a true filter of the map: the focus ticket, its blockers
// and what it blocks, and every edge between them, not just those touching
// the focus.
func TestTheMiniMapFiltersTheMapToTheFocusAndItsNeighbours(t *testing.T) {
	m, err := wayfinder.Parse(load(t, "map-214.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		focus   int
		tickets []int
		edges   []wayfinder.Edge
	}{
		// #215 blocks #220 and #221, and #220 blocks #221 too.
		{215, []int{215, 220, 221}, []wayfinder.Edge{{From: 215, To: 220}, {From: 215, To: 221}, {From: 220, To: 221}}},
		// #220's four blockers, one of which (#218) blocks another (#219).
		{220, []int{215, 217, 218, 219, 220, 221}, []wayfinder.Edge{
			{From: 218, To: 219}, {From: 219, To: 220}, {From: 218, To: 220}, {From: 217, To: 220},
			{From: 215, To: 220}, {From: 220, To: 221}, {From: 215, To: 221},
		}},
	} {
		mini := m.MiniMap(c.focus)
		if got := numbers(mini); !slices.Equal(got, c.tickets) {
			t.Errorf("#%d: tickets %v, want %v", c.focus, got, c.tickets)
		}
		if !sameEdges(mini.Edges, c.edges) {
			t.Errorf("#%d: edges %v, want %v", c.focus, mini.Edges, c.edges)
		}
		if mini.Repo != m.Repo || mini.Number != m.Number {
			t.Errorf("#%d: the mini-map is of %s#%d", c.focus, mini.Repo, mini.Number)
		}
	}
	if len(m.Tickets) != 8 || len(m.Edges) != 11 {
		t.Errorf("filtering changed the map: %d tickets, %d edges", len(m.Tickets), len(m.Edges))
	}
}

func TestTheMiniMapOfALoneTicketIsJustIt(t *testing.T) {
	m, err := wayfinder.Parse(load(t, "map-214.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Edges = nil
	if mini := m.MiniMap(216); !slices.Equal(numbers(mini), []int{216}) || len(mini.Edges) != 0 {
		t.Errorf("mini-map of a lone ticket: %v %v", numbers(mini), mini.Edges)
	}
}

func sameEdges(a, b []wayfinder.Edge) bool {
	key := func(e wayfinder.Edge) [2]int { return [2]int{e.From, e.To} }
	if len(a) != len(b) {
		return false
	}
	for _, e := range b {
		if !slices.ContainsFunc(a, func(f wayfinder.Edge) bool { return key(f) == key(e) && f.Faded == e.Faded }) {
			return false
		}
	}
	return true
}
