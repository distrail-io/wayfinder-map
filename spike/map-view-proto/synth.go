// PROTOTYPE, throwaway: wayfinder-map#6. Random DAG maps to time the layouts at
// GitHub's cap (100 sub-issues), since the fixtures are all small.
package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

func synthMap(n int, seed uint64) *Map {
	r := rand.New(rand.NewPCG(seed, 1))
	m := &Map{Num: 1, Title: "synthetic", Repo: "x/y", Tickets: map[int]*Ticket{}}
	types := []string{"research", "prototype", "grilling", "task"}
	for i := 1; i <= n; i++ {
		t := &Ticket{Num: i, Title: fmt.Sprintf("Synthetic ticket %d with a longish title to wrap", i), Type: types[r.IntN(4)], Closed: i < n/3}
		// each ticket gets 0-3 blockers from earlier tickets, biased to recent ones
		for k := r.IntN(4); k > 0 && i > 1; k-- {
			b := max(1, i-1-r.IntN(min(i-1, 12)))
			t.Blockers = append(t.Blockers, Ref{Num: b, Closed: b < n/3})
		}
		m.Tickets[i] = t
		m.Order = append(m.Order, i)
	}
	for _, n := range m.Order {
		for _, b := range m.Tickets[n].Blockers {
			m.Tickets[b.Num].Blocks = append(m.Tickets[b.Num].Blocks, n)
		}
	}
	m.classify()
	return m
}

func benchLayouts(n int) {
	m := synthMap(n, 42)
	edges := 0
	for _, t := range m.Tickets {
		edges += len(t.Blockers)
	}
	for _, eng := range []string{"dot", "sugiyama"} {
		for _, tb := range []bool{false, true} {
			t0 := time.Now()
			layoutWith(m, "cards", eng, tb)
			fmt.Printf("n=%d edges=%d %-8s tb=%-5v %v\n", n, edges, eng, tb, time.Since(t0).Round(time.Microsecond))
		}
	}
}
