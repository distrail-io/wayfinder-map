// Package wayfinder is the pure model of a Wayfinder map: it parses the raw
// GitHub GraphQL response and classifies every ticket's state.
package wayfinder

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// State is a ticket's state, checked in the order declared.
type State int

const (
	Done State = iota
	Claimed
	Blocked
	Frontier
)

func (s State) String() string {
	return [...]string{"done", "claimed", "blocked", "frontier"}[s]
}

// Type is the kind of work a ticket's wayfinder:* label names.
type Type string

const (
	Research  Type = "research"
	Prototype Type = "prototype"
	Grilling  Type = "grilling"
	Task      Type = "task"
	NoType    Type = "?"
)

// Blocker is a ticket that another is blocked by; it may live in another repo.
type Blocker struct {
	Repo   string
	Number int
	Closed bool
}

type Ticket struct {
	Number    int
	Title     string
	Type      Type
	Closed    bool
	Assignees []string
	Blockers  []Blocker
	State     State
}

// Edge runs from a blocker to the ticket it blocks, both in the map.
type Edge struct {
	From, To int
	Faded    bool // the blocker is closed
}

type Map struct {
	Repo    string
	Number  int
	Title   string
	Closed  bool
	Tickets []*Ticket // in number order
	Edges   []Edge
}

// Ticket returns the map's ticket with this number, or nil.
func (m *Map) Ticket(n int) *Ticket {
	i, ok := slices.BinarySearchFunc(m.Tickets, n, func(t *Ticket, n int) int { return t.Number - n })
	if !ok {
		return nil
	}
	return m.Tickets[i]
}

type nodes[T any] struct{ Nodes []T }

type rawMap struct {
	Data struct {
		Repository *struct {
			NameWithOwner string
			Issue         *struct {
				Number    int
				Title     string
				State     string
				SubIssues struct {
					Nodes []struct {
						Number    int
						Title     string
						State     string
						Labels    nodes[struct{ Name string }]
						Assignees nodes[struct{ Login string }]
						BlockedBy nodes[struct {
							Number     int
							State      string
							Repository struct{ NameWithOwner string }
						}]
					}
				}
			}
		}
	}
}

// ErrNotFound means the repo or the issue doesn't exist, or isn't visible.
var ErrNotFound = errors.New("map not found")

// Parse reads the response to github.MapQuery.
func Parse(raw []byte) (*Map, error) {
	var r rawMap
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse map: %w", err)
	}
	repo := r.Data.Repository
	if repo == nil || repo.Issue == nil {
		return nil, ErrNotFound
	}
	is := repo.Issue
	m := &Map{Repo: repo.NameWithOwner, Number: is.Number, Title: Sanitize(is.Title), Closed: is.State == "CLOSED"}
	for _, n := range is.SubIssues.Nodes {
		t := &Ticket{Number: n.Number, Title: Sanitize(n.Title), Type: NoType, Closed: n.State == "CLOSED"}
		for _, l := range n.Labels.Nodes {
			name, ok := strings.CutPrefix(l.Name, "wayfinder:")
			switch typ := Type(name); typ {
			case Research, Prototype, Grilling, Task:
				if ok {
					t.Type = typ
				}
			}
		}
		for _, a := range n.Assignees.Nodes {
			t.Assignees = append(t.Assignees, a.Login)
		}
		for _, b := range n.BlockedBy.Nodes {
			t.Blockers = append(t.Blockers, Blocker{b.Repository.NameWithOwner, b.Number, b.State == "CLOSED"})
		}
		t.State = classify(t)
		m.Tickets = append(m.Tickets, t)
	}
	slices.SortFunc(m.Tickets, func(a, b *Ticket) int { return a.Number - b.Number })
	for _, t := range m.Tickets {
		for _, b := range t.Blockers {
			if b.Repo == m.Repo && m.Ticket(b.Number) != nil {
				m.Edges = append(m.Edges, Edge{From: b.Number, To: t.Number, Faded: b.Closed})
			}
		}
	}
	return m, nil
}

// Summary is one open map in a repo.
type Summary struct {
	Number int
	Title  string
}

// ParseList reads the response to github.ListQuery: the repo's open maps,
// most recently updated first.
func ParseList(raw []byte) ([]Summary, error) {
	var r struct {
		Data struct {
			Repository *struct {
				Issues nodes[Summary]
			}
		}
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse map list: %w", err)
	}
	if r.Data.Repository == nil {
		return nil, ErrNotFound
	}
	maps := r.Data.Repository.Issues.Nodes
	for i := range maps {
		maps[i].Title = Sanitize(maps[i].Title)
	}
	return maps, nil
}

func classify(t *Ticket) State {
	switch {
	case t.Closed:
		return Done
	case len(t.Assignees) > 0:
		return Claimed
	}
	for _, b := range t.Blockers {
		if !b.Closed {
			return Blocked
		}
	}
	return Frontier
}

// Sanitize makes an untrusted issue title safe to show: it drops C0 and C1
// controls (so no escape sequences survive), bidi overrides, zero-width
// characters and invalid UTF-8.
func Sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r < 0x20, r >= 0x7f && r <= 0x9f,
			r >= 0x200b && r <= 0x200f, r >= 0x202a && r <= 0x202e,
			r >= 0x2066 && r <= 0x2069, r == 0xfeff, r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
}
