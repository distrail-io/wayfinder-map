// Package wayfinder is the pure model of a Wayfinder map: it parses the raw
// GitHub GraphQL response and classifies every ticket's state.
package wayfinder

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
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

// IssueRef names an issue, which may live in another repo than the map's.
type IssueRef struct {
	Repo   string
	Number int
}

// Ref names the issue as seen from repo: #N, or owner/repo#N when it lives
// in another repo.
func (r IssueRef) Ref(repo string) string {
	if r.Repo != repo {
		return fmt.Sprintf("%s#%d", r.Repo, r.Number)
	}
	return fmt.Sprintf("#%d", r.Number)
}

// Blocker is a ticket that another is blocked by; it may live in another repo.
type Blocker struct {
	IssueRef
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

	// Warnings flag what the map can't show faithfully, such as truncation
	// or a cycle, so it is never silently wrong.
	Warnings []string
}

// Blocks returns the numbers of the tickets that ticket n blocks.
func (m *Map) Blocks(n int) []int {
	var out []int
	for _, e := range m.Edges {
		if e.From == n {
			out = append(out, e.To)
		}
	}
	return out
}

// MiniMap is the map filtered to ticket n and its direct neighbours on the
// map, the tickets it is blocked by and those it blocks, with every edge
// between them. It shares its tickets with m, and has no warnings.
func (m *Map) MiniMap(n int) *Map {
	in := map[int]bool{n: true}
	for _, e := range m.Edges {
		if e.To == n {
			in[e.From] = true
		}
		if e.From == n {
			in[e.To] = true
		}
	}
	mini := &Map{Repo: m.Repo, Number: m.Number, Title: m.Title, Closed: m.Closed}
	for _, t := range m.Tickets {
		if in[t.Number] {
			mini.Tickets = append(mini.Tickets, t)
		}
	}
	for _, e := range m.Edges {
		if in[e.From] && in[e.To] {
			mini.Edges = append(mini.Edges, e)
		}
	}
	return mini
}

// Done counts the map's tickets that are done, its progress out of
// len(m.Tickets).
func (m *Map) Done() int {
	n := 0
	for _, t := range m.Tickets {
		if t.State == Done {
			n++
		}
	}
	return n
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

type rawLabels nodes[struct{ Name string }]

// names are the labels' names, sanitised.
func (l rawLabels) names() []string {
	var out []string
	for _, n := range l.Nodes {
		out = append(out, Sanitize(n.Name))
	}
	return out
}

type rawLogins nodes[struct{ Login string }]

// logins are the users' logins, sanitised.
func (l rawLogins) logins() []string {
	var out []string
	for _, n := range l.Nodes {
		out = append(out, Sanitize(n.Login))
	}
	return out
}

type pageInfo struct{ HasNextPage bool }

// gqlErrors is the top-level errors array of a GraphQL response.
type gqlErrors struct {
	Errors []struct{ Message string }
}

// err joins the messages, sanitised, or is nil when there are none.
func (g gqlErrors) err() error {
	if len(g.Errors) == 0 {
		return nil
	}
	msgs := make([]string, len(g.Errors))
	for i, e := range g.Errors {
		msgs[i] = Sanitize(e.Message)
	}
	return errors.New("github: " + strings.Join(msgs, "; "))
}

type rawMap struct {
	gqlErrors
	Data struct {
		Repository *struct {
			NameWithOwner string
			Issue         *struct {
				Number           int
				Title            string
				State            string
				SubIssuesSummary struct{ Total int }
				SubIssues        struct {
					PageInfo pageInfo
					Nodes    []struct {
						Number    int
						Title     string
						State     string
						Labels    rawLabels
						Assignees rawLogins
						BlockedBy struct {
							PageInfo pageInfo
							Nodes    []struct {
								Number     int
								State      string
								Repository struct{ NameWithOwner string }
							}
						}
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
	if err := r.err(); err != nil {
		return nil, err
	}
	repo := r.Data.Repository
	if repo == nil || repo.Issue == nil {
		return nil, ErrNotFound
	}
	is := repo.Issue
	m := &Map{Repo: Sanitize(repo.NameWithOwner), Number: is.Number, Title: Sanitize(is.Title), Closed: is.State == "CLOSED"}
	var moreBlockers []int
	for _, n := range is.SubIssues.Nodes {
		t := &Ticket{Number: n.Number, Title: Sanitize(n.Title), Closed: n.State == "CLOSED"}
		t.Type, t.Assignees = typeOf(n.Labels.names()), n.Assignees.logins()
		for _, b := range n.BlockedBy.Nodes {
			t.Blockers = append(t.Blockers, Blocker{IssueRef{Sanitize(b.Repository.NameWithOwner), b.Number}, b.State == "CLOSED"})
		}
		if n.BlockedBy.PageInfo.HasNextPage {
			moreBlockers = append(moreBlockers, n.Number)
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
	slices.Sort(moreBlockers)
	m.warn(max(is.SubIssuesSummary.Total, len(m.Tickets)), is.SubIssues.PageInfo.HasNextPage, moreBlockers)
	return m, nil
}

func (m *Map) warn(total int, moreTickets bool, moreBlockers []int) {
	if m.Closed {
		m.Warnings = append(m.Warnings, "the map is closed")
	}
	if moreTickets || total > len(m.Tickets) {
		m.Warnings = append(m.Warnings, fmt.Sprintf("truncated: showing %d of %d tickets", len(m.Tickets), total))
	}
	if len(moreBlockers) > 0 {
		ns := make([]string, len(moreBlockers))
		for i, n := range moreBlockers {
			ns[i] = fmt.Sprintf("#%d", n)
		}
		m.Warnings = append(m.Warnings, "truncated: more blockers than shown on "+strings.Join(ns, ", "))
	}
	var crossRepo, untyped []string
	for _, t := range m.Tickets {
		for _, b := range t.Blockers {
			if b.Repo != m.Repo {
				crossRepo = append(crossRepo, fmt.Sprintf("#%d by %s", t.Number, b.Ref(m.Repo)))
			}
		}
		if t.Type == NoType {
			untyped = append(untyped, fmt.Sprintf("#%d", t.Number))
		}
	}
	if len(crossRepo) > 0 {
		m.Warnings = append(m.Warnings, "blocked from another repo: "+strings.Join(crossRepo, ", "))
	}
	if len(untyped) > 0 {
		m.Warnings = append(m.Warnings, "no wayfinder:* type: "+strings.Join(untyped, ", "))
	}
	for _, c := range m.cycles() {
		ns := make([]string, len(c))
		for i, n := range c {
			ns[i] = fmt.Sprintf("#%d", n)
		}
		m.Warnings = append(m.Warnings, "cycle: "+strings.Join(ns, ", "))
	}
}

// cycles returns each group of tickets that block each other in a loop,
// including a ticket that blocks itself, by Tarjan's algorithm. Each group is
// in number order, and the groups are ordered by their first ticket.
func (m *Map) cycles() [][]int {
	succs := map[int][]int{}
	self := map[int]bool{}
	for _, e := range m.Edges {
		succs[e.From] = append(succs[e.From], e.To)
		self[e.From] = self[e.From] || e.From == e.To
	}
	index, low := map[int]int{}, map[int]int{}
	onStack := map[int]bool{}
	var stack []int
	var out [][]int
	var visit func(n int)
	visit = func(n int) {
		index[n], low[n] = len(index), len(index)
		stack = append(stack, n)
		onStack[n] = true
		for _, s := range succs[n] {
			if _, seen := index[s]; !seen {
				visit(s)
				low[n] = min(low[n], low[s])
			} else if onStack[s] {
				low[n] = min(low[n], index[s])
			}
		}
		if low[n] != index[n] {
			return
		}
		var group []int
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			group = append(group, top)
			if top == n {
				break
			}
		}
		if len(group) > 1 || self[n] {
			slices.Sort(group)
			out = append(out, group)
		}
	}
	for _, t := range m.Tickets {
		if _, seen := index[t.Number]; !seen {
			visit(t.Number)
		}
	}
	slices.SortFunc(out, func(a, b []int) int { return a[0] - b[0] })
	return out
}

// List is a repo's open maps, most recently updated first.
type List struct {
	Repo string
	Maps []Summary
	More bool // the repo has more open maps than one query fetches
}

// Summary is one open map in a repo, with its tickets counted by state.
// Total and Done count every ticket; the others only those fetched.
type Summary struct {
	Number int
	Title  string
	Total  int
	Done   int

	Frontier, Claimed, Blocked int
}

// ParseList reads the response to github.ListQuery.
func ParseList(raw []byte) (List, error) {
	var r struct {
		gqlErrors
		Data struct {
			Repository *struct {
				NameWithOwner string
				Issues        struct {
					TotalCount int
					Nodes      []struct {
						Number           int
						Title            string
						SubIssuesSummary struct{ Total, Completed int }
						SubIssues        nodes[struct {
							State                    string
							Assignees                struct{ TotalCount int }
							IssueDependenciesSummary struct{ BlockedBy int }
						}]
					}
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return List{}, fmt.Errorf("parse map list: %w", err)
	}
	if err := r.err(); err != nil {
		return List{}, err
	}
	repo := r.Data.Repository
	if repo == nil {
		return List{}, ErrNotFound
	}
	l := List{Repo: Sanitize(repo.NameWithOwner), More: repo.Issues.TotalCount > len(repo.Issues.Nodes)}
	for _, n := range repo.Issues.Nodes {
		s := Summary{Number: n.Number, Title: Sanitize(n.Title), Total: n.SubIssuesSummary.Total, Done: n.SubIssuesSummary.Completed}
		for _, t := range n.SubIssues.Nodes {
			switch stateOf(t.State == "CLOSED", t.Assignees.TotalCount > 0, t.IssueDependenciesSummary.BlockedBy > 0) {
			case Claimed:
				s.Claimed++
			case Blocked:
				s.Blocked++
			case Frontier:
				s.Frontier++
			}
		}
		l.Maps = append(l.Maps, s)
	}
	return l, nil
}

// typeOf is the type the last wayfinder:* type label names, else NoType.
func typeOf(labels []string) Type {
	t := NoType
	for _, l := range labels {
		name, ok := strings.CutPrefix(l, "wayfinder:")
		switch typ := Type(name); typ {
		case Research, Prototype, Grilling, Task:
			if ok {
				t = typ
			}
		}
	}
	return t
}

func classify(t *Ticket) State {
	openBlocker := slices.ContainsFunc(t.Blockers, func(b Blocker) bool { return !b.Closed })
	return stateOf(t.Closed, len(t.Assignees) > 0, openBlocker)
}

// stateOf checks a ticket's state in the order the states are declared.
func stateOf(closed, assigned, openBlocker bool) State {
	switch {
	case closed:
		return Done
	case assigned:
		return Claimed
	case openBlocker:
		return Blocked
	}
	return Frontier
}

// Sanitize makes an untrusted string from GitHub, such as a title, a repo
// name or a login, safe to show: it removes escape sequences whole, then
// drops controls, format characters (bidi overrides, zero-width and
// invisible tag characters), line and paragraph separators, and invalid
// UTF-8.
func Sanitize(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if end := escapeEnd(s, i, r, size); end > i {
			i = end
			continue
		}
		if !unicode.IsControl(r) && !unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) && r != utf8.RuneError {
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}

// escapeEnd returns where the escape sequence that rune r starts at s[i]
// ends, or i if r starts none. An unterminated sequence runs to the end.
func escapeEnd(s string, i int, r rune, size int) int {
	j := i + size
	switch r {
	case 0x1b: // ESC
		if j == len(s) {
			return j
		}
		switch c := s[j]; {
		case c == '[':
			return csiEnd(s, j+1)
		case c == ']' || c == 'P' || c == 'X' || c == '^' || c == '_':
			return stringEnd(s, j+1)
		case c >= 0x20 && c <= 0x2f: // intermediates, then a final byte
			for j < len(s) && s[j] >= 0x20 && s[j] <= 0x2f {
				j++
			}
			if j < len(s) && s[j] >= 0x30 && s[j] <= 0x7e {
				j++
			}
			return j
		case c >= 0x30 && c <= 0x7e:
			return j + 1
		}
		return j // a lone ESC
	case 0x9b: // C1 CSI
		return csiEnd(s, j)
	case 0x90, 0x98, 0x9d, 0x9e, 0x9f: // C1 DCS, SOS, OSC, PM, APC
		return stringEnd(s, j)
	}
	return i
}

// csiEnd skips a control sequence's parameter and intermediate bytes, and
// its final byte if one follows.
func csiEnd(s string, j int) int {
	for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
		j++
	}
	if j < len(s) && s[j] >= 0x40 && s[j] <= 0x7e {
		j++
	}
	return j
}

// stringEnd skips a control string through its terminator: BEL, or ST as
// ESC \ or U+009C.
func stringEnd(s string, j int) int {
	for ; j < len(s); j++ {
		switch {
		case s[j] == 0x07:
			return j + 1
		case s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\':
			return j + 2
		case strings.HasPrefix(s[j:], "\u009c"):
			return j + len("\u009c")
		}
	}
	return j
}
