package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/draw"
)

// wheelLines is how far one notch of the wheel scrolls the detail view.
const wheelLines = 3

// click opens the ticket or related issue under cell (x, y). A click on
// anything else does nothing.
func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	switch {
	case m.view == detailView:
		rows, targets := m.detailRows()
		i := clampScroll(m.detail.scroll, len(rows), m.detailHeight()) + y
		if y < m.detailHeight() && i < len(rows) && rows[i].target >= 0 {
			return m.recentre(append(slices.Clip(m.detail.stack), targets[rows[i].target]))
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
			return m.openDetail(ref{m.shown.Repo, n})
		}
	}
	return m, nil
}

// ticketAt is the ticket drawn under cell (x, y) of the graph area, or 0.
// It places the boxes as Render draws them; a compact ticket is the
// circle, not the square around it.
func (m Model) ticketAt(x, y int) int {
	tr := draw.Fit(m.res(), m.cols*m.cellW, m.graphRows()*m.cellH, m.maxScale())
	px, py := (float64(x)+0.5)*float64(m.cellW), (float64(y)+0.5)*float64(m.cellH)
	for _, t := range m.shown.Tickets {
		b := tr.Rect(m.res().Boxes[t.Number])
		if m.look == draw.Compact {
			r := b.W / 2
			if dx, dy := px-(b.X+r), py-(b.Y+b.H/2); dx*dx+dy*dy <= r*r {
				return t.Number
			}
		} else if px >= b.X && px < b.X+b.W && py >= b.Y && py < b.Y+b.H {
			return t.Number
		}
	}
	return 0
}

// wheel scrolls the detail view, or moves the picker's cursor.
func (m Model) wheel(b tea.MouseButton) (tea.Model, tea.Cmd) {
	up := b == tea.MouseWheelUp
	if !up && b != tea.MouseWheelDown {
		return m, nil
	}
	switch m.view {
	case detailView:
		rows, _ := m.detailRows()
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
