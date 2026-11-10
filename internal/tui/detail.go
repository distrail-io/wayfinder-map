package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// detailBack steps back to the previous focus, or leaves for the map.
func (m Model) detailBack() (tea.Model, tea.Cmd) {
	if len(m.detail.stack) > 1 {
		return m.recentre(m.detail.stack[:len(m.detail.stack)-1])
	}
	return m.leaveDetail()
}

// leaveDetail goes back to the map, however far the view has walked, with
// the focus selected if it is on the map, or the banner if it is the map.
func (m Model) leaveDetail() (tea.Model, tea.Cmd) {
	if f := m.focus(); m.ticket(f) != nil || m.isMap(f) {
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
	case "esc":
		return m.leaveDetail()
	case "b":
		return m.detailBack()
	case "o":
		f := m.focus()
		return m, m.open(f.Repo, f.Number)
	case "c":
		m.look = otherLook[m.look]
		return m.redraw()
	case "d":
		d.dir = otherDir[d.dir]
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
// first, but keeps the name and how to go back, to the map and quit.
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
		{"tab select", 6}, {"enter open", 5}, {"b back", kept}, {"esc map", kept}, {"o browser", 2}, {"c look: " + m.look.String(), 4}}
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
