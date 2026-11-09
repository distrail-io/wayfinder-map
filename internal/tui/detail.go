package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/markdown"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// ref names an issue, which may live outside the map's repo.
type ref struct {
	repo   string
	number int
}

// detail is the detail view's state: where it has been, its cursor and
// scroll, and the focus issue once fetched.
type detail struct {
	stack  []ref // the back stack; the last is the focus
	cursor int   // the related issue under the cursor, or -1
	scroll int   // the first line shown

	issue *wayfinder.Issue // the focus, fetched; nil until then
	body  []string         // the issue's body, rendered to the pane's width
	err   error            // the last fetch's error
}

type issueMsg struct {
	ref ref
	is  *wayfinder.Issue
	err error
}

func (m Model) focus() ref { return m.detail.stack[len(m.detail.stack)-1] }

// openDetail shows issue r in the detail view, on top of the back stack.
// The map image would cover it, so it goes.
func (m Model) openDetail(r ref) (tea.Model, tea.Cmd) {
	var stack []ref
	if m.view == detailView {
		stack = slices.Clip(m.detail.stack)
	}
	m.view = detailView
	m.gen++ // a pending draw would cover the view
	m, load := m.recentre(append(stack, r))
	return m, tea.Batch(m.clear(), load)
}

// recentre makes the top of stack the focus and fetches it.
func (m Model) recentre(stack []ref) (Model, tea.Cmd) {
	m.detail = detail{stack: stack, cursor: -1}
	return m, m.loadIssue()
}

// detailBack steps back to the previous focus, or leaves for the map with
// the last focus selected if it is on the map.
func (m Model) detailBack() (tea.Model, tea.Cmd) {
	if len(m.detail.stack) > 1 {
		return m.recentre(m.detail.stack[:len(m.detail.stack)-1])
	}
	if f := m.focus(); f.repo == m.shown.Repo && m.shown.Ticket(f.number) != nil {
		m.sel = f.number
	}
	m.view, m.detail = mapView, detail{}
	return m.relayout().redraw()
}

// loadIssue fetches the focus issue.
func (m Model) loadIssue() tea.Cmd {
	src, r := m.cfg.Source, m.focus()
	return func() tea.Msg {
		raw, err := src.LoadIssue(context.Background(), r.repo, r.number)
		if err != nil {
			return issueMsg{ref: r, err: err}
		}
		is, err := wayfinder.ParseIssue(raw)
		return issueMsg{r, is, err}
	}
}

// fetched takes the focus issue. A failed fetch keeps what was shown, with
// the error in place of the body.
func (m Model) fetched(msg issueMsg) Model {
	m.detail.err = msg.err
	if msg.err == nil {
		m.detail.issue = msg.is
		m = m.renderBody()
	}
	return m
}

func (m Model) renderBody() Model {
	if m.detail.issue != nil {
		m.detail.body = markdown.Render(m.detail.issue.Body, m.cols)
	}
	return m
}

func (m Model) detailKey(k string) (tea.Model, tea.Cmd) {
	rows, targets := m.detailRows()
	h := m.detailHeight()
	d := &m.detail
	switch k {
	case "esc", "b":
		return m.detailBack()
	case "o":
		f := m.focus()
		return m, m.open(f.repo, f.number)
	case "c":
		m.look = otherLook[m.look]
	case "tab", "shift+tab":
		if len(targets) == 0 {
			break
		}
		if k == "tab" {
			d.cursor = (d.cursor + 1) % len(targets)
		} else {
			d.cursor = (max(d.cursor, 0) + len(targets) - 1) % len(targets)
		}
		// Scroll the cursor's row into view.
		at := slices.IndexFunc(rows, func(r detailRow) bool { return r.target == d.cursor })
		d.scroll = min(max(d.scroll, at-h+1), at)
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

// detailRows lays the detail view out: the header, the relationships and
// the body. Whatever the map knows shows before the issue is fetched; the
// rest says it is loading. It also returns the related issues the cursor
// moves through, in the order shown.
func (m Model) detailRows() ([]detailRow, []ref) {
	f := m.focus()
	is := m.detail.issue
	ticket := m.ticket(f)
	isMap := f.repo == m.shown.Repo && f.number == m.shown.Number
	var rows []detailRow
	var targets []ref
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
		targets = append(targets, ref{r.Repo, r.Number})
	}
	heading := func(s string) { add("\x1b[1m" + s + "\x1b[0m") }
	const loading = "loading…"

	// The header.
	title := wayfinder.Related{Repo: f.repo, Number: f.number}.Ref(m.shown.Repo)
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
		parent = &wayfinder.Related{Repo: m.shown.Repo, Number: m.shown.Number, Title: m.shown.Title, Closed: m.shown.Closed}
	}
	if parent != nil {
		in := "in "
		if parent.Repo == m.shown.Repo && parent.Number == m.shown.Number {
			in = "in map "
		}
		rows = append(rows, detailRow{ansi.Truncate(in+parent.Ref(m.shown.Repo)+" "+parent.Title, m.cols, "…"), len(targets)})
		targets = append(targets, ref{parent.Repo, parent.Number})
	}

	// The relationships: blockers the map draws, what it blocks, blockers
	// it doesn't draw, then sub-issues.
	var by, blocks []wayfinder.Related
	switch {
	case is != nil:
		by, blocks = is.BlockedBy, is.Blocking
	case ticket != nil:
		for _, b := range ticket.Blockers {
			by = append(by, m.related(ref{b.Repo, b.Number}, b.Closed))
		}
		for _, n := range m.shown.Blocks(f.number) {
			blocks = append(blocks, m.related(ref{m.shown.Repo, n}, false))
		}
	}
	var drawn, undrawn []wayfinder.Related
	for _, b := range by {
		if m.ticket(ref{b.Repo, b.Number}) != nil {
			drawn = append(drawn, b)
		} else {
			undrawn = append(undrawn, b)
		}
	}
	if pending && ticket == nil {
		add("")
		add("Blocked by and blocks: " + loading)
	}
	for _, sec := range []struct {
		name    string
		rs      []wayfinder.Related
		undrawn bool
	}{{"Blocked by", drawn, false}, {"Blocks", blocks, false}, {"Also blocked by", undrawn, true}} {
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
			link(wayfinder.Related{Repo: m.shown.Repo, Number: t.Number, Title: t.Title, Closed: t.Closed}, t.State.String())
		}
		add("")
	case pending:
		add("Sub-issues: " + loading)
		add("")
	case is != nil && (is.SubTotal > 0 || len(is.SubIssues) > 0):
		heading(fmt.Sprintf("Sub-issues · %d/%d done", is.SubDone, is.SubTotal))
		for _, r := range is.SubIssues {
			link(r, m.stateOf(r))
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
	return rows, targets
}

// facts is the header's line of state, type and assignees. The map issue
// is just open or closed.
func (m Model) facts(f ref, is *wayfinder.Issue, t *wayfinder.Ticket, isMap bool) string {
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

// ticket is the map's ticket that r names, or nil.
func (m Model) ticket(r ref) *wayfinder.Ticket {
	if r.repo != m.shown.Repo {
		return nil
	}
	return m.shown.Ticket(r.number)
}

// related is r as far as the map knows it: its title only if on the map.
func (m Model) related(r ref, closed bool) wayfinder.Related {
	out := wayfinder.Related{Repo: r.repo, Number: r.number, Closed: closed}
	if t := m.ticket(r); t != nil {
		out.Title, out.Closed = t.Title, t.Closed
	}
	return out
}

// stateOf is a related issue's state on the map, else open or closed.
func (m Model) stateOf(r wayfinder.Related) string {
	switch t := m.ticket(ref{r.Repo, r.Number}); {
	case t != nil:
		return t.State.String()
	case r.Closed:
		return "closed"
	}
	return "open"
}

// detailView is the detail view's rows from the scroll offset, with the
// cursor's row marked, and its status line at the bottom.
func (m Model) detailView() tea.View {
	rows, _ := m.detailRows()
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
	f := m.focus()
	status := fmt.Sprintf("%s · tab select · enter open · b back · o browser · c look: %s · r refresh · q quit",
		wayfinder.Related{Repo: f.repo, Number: f.number}.Ref(m.shown.Repo), m.look)
	if m.err != nil {
		status = "error: " + m.err.Error()
	}
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", truncate(status, m.cols))
	return m.newView(b.String())
}
