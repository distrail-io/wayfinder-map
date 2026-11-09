package tui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/kitty"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// detailRow is a line of the detail view, and the related issue it links
// to, if any.
type detailRow struct {
	text   string
	target int // an index into the targets, or -1
}

// detailLayout is the detail view laid out: its rows, the related issues
// the cursor moves through, in the order shown, and the mini-map, if any.
type detailLayout struct {
	rows           []detailRow
	targets        []wayfinder.IssueRef
	mini           *mini
	miniAt         int // the row the mini-map's image starts on
	firstNeighbour int // the target of the mini-map's first neighbour
}

// neighbourAt is the mini-map's neighbour that target is, or 0.
func (l detailLayout) neighbourAt(target int) int {
	if i := target - l.firstNeighbour; l.mini != nil && i >= 0 && i < len(l.mini.neighbours) {
		return l.mini.neighbours[i]
	}
	return 0
}

// targetOf is the target that the mini-map's neighbour n is, or -1.
func (l detailLayout) targetOf(n int) int {
	if l.mini == nil {
		return -1
	}
	if i := slices.Index(l.mini.neighbours, n); i >= 0 {
		return l.firstNeighbour + i
	}
	return -1
}

// detailLayout lays the detail view out: the header, the relationships,
// the sub-issues and the body. Whatever the map knows shows before the
// issue is fetched; the rest says it is loading.
func (m Model) detailLayout() detailLayout {
	f := m.focus()
	b := &detailBuilder{m: m, f: f, is: m.detail.issue, ticket: m.ticket(f), isMap: m.isMap(f)}
	// Until the issue arrives, what only it can say is pending; if fetching
	// it failed, the error says so instead.
	b.pending = b.is == nil && m.detail.err == nil
	b.name = f.Ref(m.shown.Repo)
	switch {
	case b.is != nil:
		b.name += " " + b.is.Title
	case b.ticket != nil:
		b.name += " " + b.ticket.Title
	case b.isMap:
		b.name += " " + m.shown.Title
	}
	b.header()
	b.relationships()
	b.subIssues()
	b.body()
	return b.l
}

// detailBuilder lays the detail view out row by row, from what is known of
// the focus.
type detailBuilder struct {
	m       Model
	f       wayfinder.IssueRef
	is      *wayfinder.Issue  // the focus, fetched; nil until then
	ticket  *wayfinder.Ticket // the focus on the map, or nil
	isMap   bool              // the focus is the map issue
	pending bool              // the focus is being fetched
	name    string            // the focus's ref and title, if known
	l       detailLayout
}

const loading = "loading…"

// add adds a row of s, cut to the pane's width.
func (b *detailBuilder) add(s string) { b.raw(ansi.Truncate(s, b.m.cols, "…")) }

// raw adds a row of s as it is.
func (b *detailBuilder) raw(s string) { b.l.rows = append(b.l.rows, detailRow{s, -1}) }

func (b *detailBuilder) heading(s string) { b.add("\x1b[1m" + s + "\x1b[0m") }

// target adds r to the targets, linked from a row of text, if not empty.
func (b *detailBuilder) target(r wayfinder.IssueRef, text string) {
	if text != "" {
		b.l.rows = append(b.l.rows, detailRow{text, len(b.l.targets)})
	}
	b.l.targets = append(b.l.targets, r)
}

// link adds a row that links to r, with its title, if known, and state.
func (b *detailBuilder) link(r wayfinder.Related, state string) {
	text := r.Ref(b.m.shown.Repo)
	if r.Title != "" {
		text += " " + r.Title
	}
	if state != "" {
		text += " · " + state
	}
	b.target(r.IssueRef, "  "+text)
}

// header is the focus's name, wrapped, its facts, its labels and a link to
// its parent.
func (b *detailBuilder) header() {
	for _, l := range strings.Split(ansi.Wrap(b.name, b.m.cols, ""), "\n") {
		b.heading(l)
	}
	b.add(b.facts())
	switch {
	case b.pending:
		b.add("labels: " + loading)
	case b.is != nil && len(b.is.Labels) > 0:
		b.add("labels: " + strings.Join(b.is.Labels, ", "))
	}
	if p := b.parent(); p != nil {
		in := "in "
		if b.m.isMap(p.IssueRef) {
			in = "in map "
		}
		b.target(p.IssueRef, ansi.Truncate(in+p.Ref(b.m.shown.Repo)+" "+p.Title, b.m.cols, "…"))
	}
}

// facts is the header's line of state, type and assignees. The map issue
// is just open or closed.
func (b *detailBuilder) facts() string {
	var parts []string
	var typ wayfinder.Type
	var who []string
	switch is, t := b.is, b.ticket; {
	case b.isMap:
		state := "open"
		if (is != nil && is.Closed) || (is == nil && b.m.shown.Closed) {
			state = "closed"
		}
		return "map · " + state
	case t != nil:
		parts, typ, who = append(parts, t.State.String()), t.Type, t.Assignees
	case is != nil:
		parts = append(parts, is.State().String())
	default:
		return loading
	}
	if is := b.is; is != nil { // fresher than the map
		typ, who = is.Type, is.Assignees
		if parts[0] == wayfinder.Done.String() && is.CloseReason != "" {
			parts = append(parts, is.CloseReason)
		}
	}
	if typ != wayfinder.NoType {
		parts = append(parts, string(typ))
	}
	if len(who) == 0 {
		parts = append(parts, "unassigned")
	} else {
		parts = append(parts, "assigned to "+strings.Join(who, ", "))
	}
	return strings.Join(parts, " · ")
}

// parent is the focus's parent: the fetched issue's, else the map for a
// ticket on it, or nil.
func (b *detailBuilder) parent() *wayfinder.Related {
	switch {
	case b.is != nil:
		return b.is.Parent
	case b.ticket != nil:
		return &wayfinder.Related{IssueRef: b.m.onMap(b.m.shown.Number), Title: b.m.shown.Title, Closed: b.m.shown.Closed}
	}
	return nil
}

// relationships are the blockers the map draws and what the focus blocks,
// as a mini-map where images are shown inline and the focus is a ticket on
// the map, else as text lists, then the blockers the map doesn't draw.
func (b *detailBuilder) relationships() {
	by, blocks := b.dependencies()
	var drawn, undrawn []wayfinder.Related
	for _, r := range by {
		if b.m.ticket(r.IssueRef) != nil {
			drawn = append(drawn, r)
		} else {
			undrawn = append(undrawn, r)
		}
	}
	if b.pending && b.ticket == nil {
		b.add("")
		b.add("Blocked by and blocks: " + loading)
	}
	b.l.firstNeighbour = len(b.l.targets)
	switch mi := b.m.miniMap(); {
	case mi != nil && len(mi.neighbours) == 0:
		// A blocker off the map is a dependency still.
		if len(undrawn) == 0 {
			b.add("")
			b.add("no dependencies")
		}
	case mi != nil:
		b.miniMap(mi)
	default:
		// The focus stands between the lists of what blocks it and what it
		// blocks, where the mini-map would draw it.
		b.list("Blocked by", drawn, false)
		if len(drawn) > 0 || len(blocks) > 0 {
			b.add("")
			b.heading("Focus")
			b.add("  " + b.focusRow())
		}
		b.list("Blocks", blocks, false)
	}
	b.list("Also blocked by", undrawn, true)
}

// dependencies are what the focus is blocked by and what it blocks: the
// fetched issue's, else the map's for a ticket on it.
func (b *detailBuilder) dependencies() (by, blocks []wayfinder.Related) {
	switch {
	case b.is != nil:
		return b.is.BlockedBy, b.is.Blocking
	case b.ticket != nil:
		for _, r := range b.ticket.Blockers {
			by = append(by, b.m.related(r.IssueRef, r.Closed))
		}
		for _, n := range b.m.shown.Blocks(b.f.Number) {
			blocks = append(blocks, b.m.related(b.m.onMap(n), false))
		}
	}
	return by, blocks
}

// miniMap is the mini-map's placeholders, its neighbours' targets, and in
// the compact look the titles its circles hide.
func (b *detailBuilder) miniMap(mi *mini) {
	b.add("")
	b.l.mini, b.l.miniAt = mi, len(b.l.rows)
	for _, p := range kitty.Placeholders(miniMapID, mi.cols, mi.rows) {
		b.raw(p)
	}
	for _, n := range mi.neighbours {
		r := b.m.related(b.m.onMap(n), false)
		if b.m.look == draw.Compact {
			b.link(r, b.m.stateOf(r))
		} else {
			b.target(r.IssueRef, "")
		}
	}
}

// list is a heading over a link to each related issue, if there are any.
// Those in the map's repo that are off the map say so.
func (b *detailBuilder) list(name string, rs []wayfinder.Related, offMap bool) {
	if len(rs) == 0 {
		return
	}
	b.add("")
	b.heading(name)
	for _, r := range rs {
		state := b.m.stateOf(r)
		if offMap && r.Repo == b.m.shown.Repo {
			state += ", not on map"
		}
		b.link(r, state)
	}
}

// focusRow is the focus as the text lists show it: its name and its state,
// if known yet.
func (b *detailBuilder) focusRow() string {
	switch {
	case b.ticket != nil:
		return b.name + " · " + b.ticket.State.String()
	case b.is != nil:
		return b.name + " · " + b.is.State().String()
	}
	return b.name
}

// subIssues are the map issue's tickets, from the map, or the fetched
// issue's sub-issues, with how many are done.
func (b *detailBuilder) subIssues() {
	b.add("")
	switch {
	case b.isMap:
		done := 0
		for _, t := range b.m.shown.Tickets {
			if t.State == wayfinder.Done {
				done++
			}
		}
		b.heading(subIssuesHeading(done, len(b.m.shown.Tickets)))
		for _, t := range b.m.shown.Tickets {
			b.link(wayfinder.Related{IssueRef: b.m.onMap(t.Number), Title: t.Title, Closed: t.Closed}, t.State.String()+withType(t.Type))
		}
		b.add("")
	case b.pending:
		b.add("Sub-issues: " + loading)
		b.add("")
	case b.is != nil && (b.is.SubTotal > 0 || len(b.is.SubIssues) > 0):
		b.heading(subIssuesHeading(b.is.SubDone, b.is.SubTotal))
		for _, r := range b.is.SubIssues {
			b.link(r, b.m.stateOf(r)+withType(r.Type))
		}
		b.add("")
	}
}

func subIssuesHeading(done, total int) string {
	return fmt.Sprintf("Sub-issues · %d/%d done", done, total)
}

// withType follows a state with a type, if it is known and one of
// Wayfinder's.
func withType(t wayfinder.Type) string {
	if t == "" || t == wayfinder.NoType {
		return ""
	}
	return " · " + string(t)
}

// body is the focus's body, rendered, or the error fetching it.
func (b *detailBuilder) body() {
	switch {
	case b.m.detail.err != nil:
		b.add("\x1b[31merror: " + b.m.detail.err.Error() + "\x1b[0m")
	case b.is == nil:
		b.add(loading)
	default:
		for _, l := range b.m.detail.body {
			b.raw(l)
		}
	}
}
