package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// wheelLines is how far one notch of the wheel scrolls the detail view.
const wheelLines = 3

// click opens the ticket or related issue under cell (x, y). A click on
// anything else does nothing.
func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	switch {
	case m.view == detailView:
		l := m.detailLayout()
		i := clampScroll(m.detail.scroll, len(l.rows), m.detailHeight()) + y
		if y >= m.detailHeight() || i >= len(l.rows) {
			break
		}
		target := l.rows[i].target
		if mi := l.mini; mi != nil && i >= l.miniAt && i < l.miniAt+mi.rows {
			target = l.targetOf(m.hit(mi.mp, mi.res, m.transform(mi), x, i-l.miniAt))
		}
		if target >= 0 {
			return m.recentre(append(slices.Clip(m.detail.stack), l.targets[target]))
		}
	case m.view == mapView && m.shown != nil && y < m.graphRows():
		n := 0
		if m.cfg.List {
			_, numbers := m.listRows()
			n = numbers[y]
		} else {
			n = m.ticketAt(x, y)
		}
		if n != 0 {
			m.sel = n
			return m.openDetail(m.onMap(n))
		}
	}
	return m, nil
}

// ticketAt is the ticket drawn under cell (x, y) of the graph area, or 0.
func (m Model) ticketAt(x, y int) int {
	return m.hit(m.shown, m.res(), draw.Fit(m.res(), m.cols*m.cellW, m.graphRows()*m.cellH, m.maxScale()), x, y)
}

// hit is the ticket of mp under cell (x, y) of an image of its layout res,
// or the map's number for its banner, or 0. It places the boxes with tr, as
// Render draws them; in the compact look a ticket is the circle, not the
// square around it, and the banner the pill, not the box around it. Only
// the map's layout has a banner, not a mini-map's.
func (m Model) hit(mp *wayfinder.Map, res layout.Result, tr draw.Transform, x, y int) int {
	p := layout.Point{X: (float64(x) + 0.5) * float64(m.cellW), Y: (float64(y) + 0.5) * float64(m.cellH)}
	if b, ok := res.Boxes[mp.Number]; ok && m.within(tr.Rect(b), p) {
		return mp.Number
	}
	for _, t := range mp.Tickets {
		if m.within(tr.Rect(res.Boxes[t.Number]), p) {
			return t.Number
		}
	}
	return 0
}

// within is whether p is in box b as the look draws it: the whole box for
// cards; for compact, the box with its ends fully rounded, which makes a
// square box a circle and a wide one a pill.
func (m Model) within(b layout.Rect, p layout.Point) bool {
	if m.look != draw.Compact {
		return p.X >= b.X && p.X < b.X+b.W && p.Y >= b.Y && p.Y < b.Y+b.H
	}
	// p's distance from the line through the middle of the pill, end to end.
	r := min(b.W, b.H) / 2
	dx := max(b.X+r-p.X, 0, p.X-(b.X+b.W-r))
	dy := max(b.Y+r-p.Y, 0, p.Y-(b.Y+b.H-r))
	return dx*dx+dy*dy <= r*r
}

// wheel scrolls the detail view, or moves the picker's cursor.
func (m Model) wheel(b tea.MouseButton) (tea.Model, tea.Cmd) {
	up := b == tea.MouseWheelUp
	if !up && b != tea.MouseWheelDown {
		return m, nil
	}
	switch m.view {
	case detailView:
		rows := m.detailLayout().rows
		if up {
			m.detail.scroll -= wheelLines
		} else {
			m.detail.scroll += wheelLines
		}
		m.detail.scroll = clampScroll(m.detail.scroll, len(rows), m.detailHeight())
	case pickerView:
		if up {
			return m.pickerKey("up")
		}
		return m.pickerKey("down")
	}
	return m, nil
}
