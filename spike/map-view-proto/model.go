// PROTOTYPE, throwaway. Answers "What should the map look like in the terminal,
// and which layout and drawing stack produces it?" (distrail-io/wayfinder-map#3).
// Not production code: no tests, minimal error handling.
package main

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type State int

const (
	Done State = iota
	Claimed
	Frontier
	Blocked
)

var stateNames = [...]string{"done", "claimed", "frontier", "blocked"}

type Ref struct {
	Num    int
	Repo   string
	Closed bool
}

type Ticket struct {
	Num      int
	Title    string
	Type     string
	Closed   bool
	Assignee string
	Blockers []Ref
	Blocks   []int
	State    State
}

type Map struct {
	Num     int
	Title   string
	Repo    string
	Tickets map[int]*Ticket
	Order   []int
}

type rawFixture struct {
	Data struct {
		Repository struct {
			Issue struct {
				Number    int
				Title     string
				SubIssues struct {
					Nodes []struct {
						Number    int
						Title     string
						State     string
						Labels    struct{ Nodes []struct{ Name string } }
						Assignees struct{ Nodes []struct{ Login string } }
						BlockedBy struct {
							Nodes []struct {
								Number     int
								State      string
								Repository struct{ NameWithOwner string }
							}
						} `json:"blockedBy"`
					}
				} `json:"subIssues"`
			}
		}
	}
}

// sanitize strips control characters, escape sequences and bidi overrides:
// issue titles are untrusted.
func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r < 0x20, r >= 0x7f && r <= 0x9f:
		case r >= 0x202a && r <= 0x202e, r >= 0x2066 && r <= 0x2069:
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func loadMap(path, repo string, synthClaim bool) (*Map, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f rawFixture
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	is := f.Data.Repository.Issue
	m := &Map{Num: is.Number, Title: sanitize(is.Title), Repo: repo, Tickets: map[int]*Ticket{}}
	for _, n := range is.SubIssues.Nodes {
		t := &Ticket{Num: n.Number, Title: sanitize(n.Title), Type: "?", Closed: n.State == "CLOSED"}
		for _, l := range n.Labels.Nodes {
			if strings.HasPrefix(l.Name, "wayfinder:") {
				t.Type = strings.TrimPrefix(l.Name, "wayfinder:")
			}
		}
		if len(n.Assignees.Nodes) > 0 {
			t.Assignee = n.Assignees.Nodes[0].Login
		}
		for _, b := range n.BlockedBy.Nodes {
			t.Blockers = append(t.Blockers, Ref{b.Number, b.Repository.NameWithOwner, b.State == "CLOSED"})
		}
		m.Tickets[t.Num] = t
		m.Order = append(m.Order, t.Num)
	}
	sort.Ints(m.Order)
	for _, n := range m.Order {
		for _, b := range m.Tickets[n].Blockers {
			if bt, ok := m.inMap(b); ok {
				bt.Blocks = append(bt.Blocks, n)
			}
		}
	}
	m.classify()
	if synthClaim {
		// None of the fixtures has a claimed ticket, so claim the first frontier one.
		for _, n := range m.Order {
			if t := m.Tickets[n]; t.State == Frontier {
				t.Assignee = "synthetic-claim"
				break
			}
		}
		m.classify()
	}
	return m, nil
}

func (m *Map) inMap(r Ref) (*Ticket, bool) {
	if r.Repo != "" && r.Repo != m.Repo {
		return nil, false
	}
	t, ok := m.Tickets[r.Num]
	return t, ok
}

func (m *Map) classify() {
	for _, t := range m.Tickets {
		switch {
		case t.Closed:
			t.State = Done
		case t.Assignee != "":
			t.State = Claimed
		default:
			t.State = Frontier
			for _, b := range t.Blockers {
				if !b.Closed {
					t.State = Blocked
				}
			}
		}
	}
}

// layers puts each ticket one layer after its deepest in-map blocker.
func (m *Map) layers() map[int]int {
	L := map[int]int{}
	var visit func(n int, depth int) int
	visit = func(n int, depth int) int {
		if v, ok := L[n]; ok {
			return v
		}
		if depth > len(m.Tickets) { // cycle guard
			return 0
		}
		best := 0
		for _, b := range m.Tickets[n].Blockers {
			if bt, ok := m.inMap(b); ok {
				if v := visit(bt.Num, depth+1) + 1; v > best {
					best = v
				}
			}
		}
		L[n] = best
		return best
	}
	for _, n := range m.Order {
		visit(n, 0)
	}
	return L
}
