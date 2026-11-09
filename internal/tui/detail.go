package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/kitty"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/markdown"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// onMap names issue n in the map's repo.
func (m Model) onMap(n int) wayfinder.IssueRef {
	return wayfinder.IssueRef{Repo: m.shown.Repo, Number: n}
}

// isMap reports whether r is the map issue.
func (m Model) isMap(r wayfinder.IssueRef) bool { return r == m.onMap(m.shown.Number) }

// detail is the detail view's state: where it has been, its cursor and
// scroll, the mini-map's direction, and the focus issue once fetched.
type detail struct {
	stack  []wayfinder.IssueRef // the back stack; the last is the focus
	cursor int                  // the related issue under the cursor, or -1
	scroll int                  // the first line shown
	dir    layout.Dir           // the mini-map's, LR on opening whatever the map's

	issue *wayfinder.Issue // the focus, fetched; nil until then
	body  []string         // the issue's body, rendered to the pane's width
	err   error            // the last fetch's error

	bodyImages []markdown.Image // the body's images
	slots      []slot           // where each is shown, if it is
	sent       []slot           // what each image ID was last sent as
}

type issueMsg struct {
	ref wayfinder.IssueRef
	is  *wayfinder.Issue
	err error
}

func (m Model) focus() wayfinder.IssueRef { return m.detail.stack[len(m.detail.stack)-1] }

// openDetail shows issue r in the detail view, on top of the back stack.
// The map image would cover it, so it goes.
func (m Model) openDetail(r wayfinder.IssueRef) (tea.Model, tea.Cmd) {
	var stack []wayfinder.IssueRef
	if m.view == detailView {
		stack = slices.Clip(m.detail.stack)
	}
	m.view = detailView
	m, load := m.recentre(append(stack, r)) // its redraw supersedes the map's
	return m, tea.Batch(m.clear(), load)
}

// recentre makes the top of stack the focus, fetches it and draws its
// mini-map. The last focus's images go.
func (m Model) recentre(stack []wayfinder.IssueRef) (Model, tea.Cmd) {
	del := m.deleteBodyImages()
	m.detail = detail{stack: stack, cursor: -1, dir: m.detail.dir}
	m, redraw := m.redraw()
	return m, tea.Batch(del, m.loadIssue(), redraw)
}

// detailBack steps back to the previous focus, or leaves for the map with
// the last focus selected if it is on the map.
func (m Model) detailBack() (tea.Model, tea.Cmd) {
	if len(m.detail.stack) > 1 {
		return m.recentre(m.detail.stack[:len(m.detail.stack)-1])
	}
	if f := m.focus(); m.ticket(f) != nil {
		m.sel = f.Number
	}
	del := m.deleteImages()
	m.view, m.detail = mapView, detail{}
	m, redraw := m.relayout().redraw()
	return m, tea.Batch(del, redraw)
}

// loadIssue fetches the focus issue.
func (m Model) loadIssue() tea.Cmd {
	src, r := m.cfg.Source, m.focus()
	return func() tea.Msg {
		raw, err := src.LoadIssue(context.Background(), r.Repo, r.Number)
		if err != nil {
			return issueMsg{ref: r, err: err}
		}
		is, err := wayfinder.ParseIssue(raw)
		return issueMsg{r, is, err}
	}
}

// fetched takes the focus issue and fetches its body's images straight
// away. A failed fetch keeps what was shown, with the error in place of the
// body.
func (m Model) fetched(msg issueMsg) (tea.Model, tea.Cmd) {
	m.detail.err = msg.err
	if msg.err != nil {
		return m, nil
	}
	m.detail.issue = msg.is
	m.detail.bodyImages = markdown.Images(msg.is.Body, msg.is.BodyHTML)
	m, fetch := m.fetchImages()
	m = m.renderBody()
	if !m.unsent() {
		return m, fetch
	}
	// Images already fetched, for an issue seen before.
	m, redraw := m.redraw()
	return m, tea.Batch(fetch, redraw)
}

func (m Model) detailKey(k string) (tea.Model, tea.Cmd) {
	l := m.detailLayout()
	rows, targets := l.rows, l.targets
	h := m.detailHeight()
	d := &m.detail
	switch k {
	case "esc", "b":
		return m.detailBack()
	case "o":
		f := m.focus()
		return m, m.open(f.Repo, f.Number)
	case "c":
		m.look = otherLook[m.look]
		return m.redraw()
	case "d":
		d.dir = map[layout.Dir]layout.Dir{layout.LR: layout.TB, layout.TB: layout.LR}[d.dir]
		return m.redraw()
	case "tab", "shift+tab":
		if len(targets) == 0 {
			break
		}
		if k == "tab" {
			d.cursor = (d.cursor + 1) % len(targets)
		} else {
			d.cursor = (max(d.cursor, 0) + len(targets) - 1) % len(targets)
		}
		// Scroll the cursor's row, or its ticket in the mini-map, into view.
		from := slices.IndexFunc(rows, func(r detailRow) bool { return r.target == d.cursor })
		to := from
		if n := l.neighbourAt(d.cursor); n != 0 {
			from, to = m.ticketRows(l.mini, n)
			from, to = l.miniAt+from, l.miniAt+to
		}
		d.scroll = clampScroll(min(max(d.scroll, to-h+1), from), len(rows), h)
		// The ring moves with the cursor.
		return m.redraw()
	case "enter":
		if d.cursor >= 0 && d.cursor < len(targets) {
			return m.recentre(append(slices.Clip(d.stack), targets[d.cursor]))
		}
	case "j", "down":
		d.scroll++
	case "k", "up":
		d.scroll--
	case "pgdown", "space":
		d.scroll += max(1, h-1)
	case "pgup":
		d.scroll -= max(1, h-1)
	case "g":
		d.scroll = 0
	case "G":
		d.scroll = len(rows)
	}
	d.scroll = clampScroll(d.scroll, len(rows), h)
	return m, nil
}

func clampScroll(scroll, rows, h int) int { return max(0, min(scroll, rows-h)) }

// detailHeight is how many of the view's lines fit above its status line.
func (m Model) detailHeight() int { return max(0, m.rows-statusRows) }

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

// detailLayout lays the detail view out: the header, the relationships and
// the body. Whatever the map knows shows before the issue is fetched; the
// rest says it is loading. With images, the focus ticket's blockers on the
// map and what it blocks are drawn as a mini-map rather than listed.
func (m Model) detailLayout() detailLayout {
	f := m.focus()
	is := m.detail.issue
	ticket := m.ticket(f)
	isMap := m.isMap(f)
	mi := m.miniMap()
	var rows []detailRow
	var targets []wayfinder.IssueRef
	add := func(s string) { rows = append(rows, detailRow{ansi.Truncate(s, m.cols, "…"), -1}) }
	link := func(r wayfinder.Related, state string) {
		text := r.Ref(m.shown.Repo)
		if r.Title != "" {
			text += " " + r.Title
		}
		if state != "" {
			text += " · " + state
		}
		rows = append(rows, detailRow{"  " + text, len(targets)})
		targets = append(targets, r.IssueRef)
	}
	heading := func(s string) { add("\x1b[1m" + s + "\x1b[0m") }
	const loading = "loading…"

	// The header.
	title := f.Ref(m.shown.Repo)
	switch {
	case is != nil:
		title += " " + is.Title
	case ticket != nil:
		title += " " + ticket.Title
	case isMap:
		title += " " + m.shown.Title
	}
	for _, l := range strings.Split(ansi.Wrap(title, m.cols, ""), "\n") {
		add("\x1b[1m" + l + "\x1b[0m")
	}
	add(m.facts(f, is, ticket, isMap))
	// Until the issue arrives, what only it can say is pending; if fetching
	// it failed, the error says so instead.
	pending := is == nil && m.detail.err == nil
	switch {
	case pending:
		add("labels: " + loading)
	case is != nil && len(is.Labels) > 0:
		add("labels: " + strings.Join(is.Labels, ", "))
	}
	parent := (*wayfinder.Related)(nil)
	switch {
	case is != nil:
		parent = is.Parent
	case ticket != nil:
		parent = &wayfinder.Related{IssueRef: m.onMap(m.shown.Number), Title: m.shown.Title, Closed: m.shown.Closed}
	}
	if parent != nil {
		in := "in "
		if m.isMap(parent.IssueRef) {
			in = "in map "
		}
		rows = append(rows, detailRow{ansi.Truncate(in+parent.Ref(m.shown.Repo)+" "+parent.Title, m.cols, "…"), len(targets)})
		targets = append(targets, parent.IssueRef)
	}

	// The relationships: blockers the map draws, what it blocks, blockers
	// it doesn't draw, then sub-issues.
	var by, blocks []wayfinder.Related
	switch {
	case is != nil:
		by, blocks = is.BlockedBy, is.Blocking
	case ticket != nil:
		for _, b := range ticket.Blockers {
			by = append(by, m.related(b.IssueRef, b.Closed))
		}
		for _, n := range m.shown.Blocks(f.Number) {
			blocks = append(blocks, m.related(m.onMap(n), false))
		}
	}
	var drawn, undrawn []wayfinder.Related
	for _, b := range by {
		if m.ticket(b.IssueRef) != nil {
			drawn = append(drawn, b)
		} else {
			undrawn = append(undrawn, b)
		}
	}
	if pending && ticket == nil {
		add("")
		add("Blocked by and blocks: " + loading)
	}
	l := detailLayout{firstNeighbour: len(targets)}
	// Without a mini-map, the focus stands between the lists of what blocks
	// it and what it blocks, where the mini-map would draw it.
	sections := []struct {
		name    string
		rs      []wayfinder.Related
		undrawn bool
		focus   bool
	}{{"Blocked by", drawn, false, false}, {"Focus", nil, false, len(drawn) > 0 || len(blocks) > 0},
		{"Blocks", blocks, false, false}, {"Also blocked by", undrawn, true, false}}
	switch {
	case mi != nil && len(mi.neighbours) == 0:
		// A blocker off the map is a dependency still.
		if len(undrawn) == 0 {
			add("")
			add("no dependencies")
		}
		sections = sections[3:]
	case mi != nil:
		// The mini-map, then in the compact look the titles its circles
		// hide.
		add("")
		l.mini, l.miniAt = mi, len(rows)
		for _, p := range kitty.Placeholders(miniMapID, mi.cols, mi.rows) {
			rows = append(rows, detailRow{p, -1})
		}
		for _, n := range mi.neighbours {
			r := m.related(m.onMap(n), false)
			if m.look == draw.Compact {
				link(r, m.stateOf(r))
			} else {
				targets = append(targets, r.IssueRef)
			}
		}
		sections = sections[3:]
	}
	for _, sec := range sections {
		if sec.focus {
			add("")
			heading(sec.name)
			add("  " + m.focusRow(title, ticket, is))
		}
		if len(sec.rs) == 0 {
			continue
		}
		add("")
		heading(sec.name)
		for _, r := range sec.rs {
			state := m.stateOf(r)
			if sec.undrawn && r.Repo == m.shown.Repo {
				state += ", not on map"
			}
			link(r, state)
		}
	}
	add("")
	switch {
	case isMap:
		done := 0
		for _, t := range m.shown.Tickets {
			if t.State == wayfinder.Done {
				done++
			}
		}
		heading(fmt.Sprintf("Sub-issues · %d/%d done", done, len(m.shown.Tickets)))
		for _, t := range m.shown.Tickets {
			link(wayfinder.Related{IssueRef: m.onMap(t.Number), Title: t.Title, Closed: t.Closed}, t.State.String()+withType(t.Type))
		}
		add("")
	case pending:
		add("Sub-issues: " + loading)
		add("")
	case is != nil && (is.SubTotal > 0 || len(is.SubIssues) > 0):
		heading(fmt.Sprintf("Sub-issues · %d/%d done", is.SubDone, is.SubTotal))
		for _, r := range is.SubIssues {
			link(r, m.stateOf(r)+withType(r.Type))
		}
		add("")
	}

	// The body.
	switch {
	case m.detail.err != nil:
		add("\x1b[31merror: " + m.detail.err.Error() + "\x1b[0m")
	case is == nil:
		add(loading)
	default:
		for _, l := range m.detail.body {
			rows = append(rows, detailRow{l, -1})
		}
	}
	l.rows, l.targets = rows, targets
	return l
}

// facts is the header's line of state, type and assignees. The map issue
// is just open or closed.
func (m Model) facts(f wayfinder.IssueRef, is *wayfinder.Issue, t *wayfinder.Ticket, isMap bool) string {
	var parts []string
	var typ wayfinder.Type
	var who []string
	switch {
	case isMap:
		state := "open"
		if (is != nil && is.Closed) || (is == nil && m.shown.Closed) {
			state = "closed"
		}
		return "map · " + state
	case t != nil:
		parts, typ, who = append(parts, t.State.String()), t.Type, t.Assignees
	case is != nil:
		parts = append(parts, is.State().String())
	default:
		return "loading…"
	}
	if is != nil { // fresher than the map
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

// focusRow is the focus as the text lists show it: its name, as the header
// gives it, and its state, if known yet.
func (m Model) focusRow(name string, t *wayfinder.Ticket, is *wayfinder.Issue) string {
	switch {
	case t != nil:
		return name + " · " + t.State.String()
	case is != nil:
		return name + " · " + is.State().String()
	}
	return name
}

// ticket is the map's ticket that r names, or nil.
func (m Model) ticket(r wayfinder.IssueRef) *wayfinder.Ticket {
	if r.Repo != m.shown.Repo {
		return nil
	}
	return m.shown.Ticket(r.Number)
}

// related is r as far as the map knows it: its title only if on the map.
func (m Model) related(r wayfinder.IssueRef, closed bool) wayfinder.Related {
	out := wayfinder.Related{IssueRef: r, Closed: closed}
	if t := m.ticket(r); t != nil {
		out.Title, out.Closed = t.Title, t.Closed
	}
	return out
}

// stateOf is a related issue's state on the map, else closed, claimed if
// it is known to be assigned, or open.
func (m Model) stateOf(r wayfinder.Related) string {
	switch t := m.ticket(r.IssueRef); {
	case t != nil:
		return t.State.String()
	case r.Closed:
		return "closed"
	case len(r.Assignees) > 0:
		return wayfinder.Claimed.String()
	}
	return "open"
}

// withType follows a state with a type, if it is known and one of
// Wayfinder's.
func withType(t wayfinder.Type) string {
	if t == "" || t == wayfinder.NoType {
		return ""
	}
	return " · " + string(t)
}

// detailView is the detail view's rows from the scroll offset, with the
// cursor's row marked, and its status line at the bottom.
func (m Model) detailView() tea.View {
	rows := m.detailLayout().rows
	h := m.detailHeight()
	from := clampScroll(m.detail.scroll, len(rows), h)
	rows = rows[from:min(len(rows), from+h)]
	var b strings.Builder
	for _, r := range rows {
		if r.target >= 0 && r.target == m.detail.cursor {
			r.text = "\x1b[1;33m› " + strings.TrimPrefix(ansi.Strip(r.text), "  ") + "\x1b[0m"
		}
		b.WriteString(ansi.Truncate(r.text, m.cols, "…") + "\n")
	}
	b.WriteString(strings.Repeat("\n", max(0, h-len(rows))))
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", truncate(m.detailStatus(), m.cols))
	return m.newView(b.String())
}

// detailStatus is the focus's name and the keys, with the c and d hints
// naming what they are set to. A narrow pane drops the hints least needed
// first, but keeps the name and how to go back and quit.
func (m Model) detailStatus() string {
	if m.err != nil {
		return "error: " + m.err.Error()
	}
	f := m.focus()
	// The lower a hint's rank, the sooner it goes.
	type hint struct {
		text string
		rank int
	}
	const kept = 7
	hints := []hint{{f.Ref(m.shown.Repo), kept},
		{"tab select", 6}, {"enter open", 5}, {"b back", kept}, {"o browser", 2}, {"c look: " + m.look.String(), 4}}
	if m.inlineImages() { // only the mini-map has a direction
		hints = append(hints, hint{"d dir: " + m.detail.dir.String(), 3})
	}
	hints = append(hints, hint{"r refresh", 1}, hint{"q quit", kept})
	for drop := 0; ; drop++ {
		var parts []string
		for _, h := range hints {
			if h.rank > drop {
				parts = append(parts, h.text)
			}
		}
		if s := strings.Join(parts, " · "); drop == kept-1 || ansi.StringWidth(s) <= m.cols {
			return s
		}
	}
}
