package tui

import (
	"cmp"
	"math"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/kitty"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// miniMapID is the mini-map's image ID. Sending it again replaces it.
var miniMapID = kitty.ID(0)

// mini is the detail view's mini-map, laid out to fit the pane.
type mini struct {
	mp         *wayfinder.Map
	res        layout.Result
	focus      int
	neighbours []int // in the order Tab visits them
	cols, rows int   // the cells it spans; none without neighbours
}

// inlineImages is whether the detail view shows images inline, with Unicode
// placeholders. List mode never does.
func (m Model) inlineImages() bool { return m.cfg.Images && !m.cfg.List }

// miniMap is the focus ticket's mini-map, drawn at the map's largest scale
// and shrunk only to fit the pane's width. It is nil without images, or if
// the focus isn't a ticket on the map.
func (m Model) miniMap() *mini {
	if !m.inlineImages() || m.shown == nil || m.cols <= 0 || m.cellW <= 0 || m.cellH <= 0 {
		return nil
	}
	t := m.ticket(m.focus())
	if t == nil {
		return nil
	}
	mi := &mini{mp: m.shown.MiniMap(t.Number), focus: t.Number}
	if len(mi.mp.Tickets) == 1 {
		return mi
	}
	mi.res = layout.Layered(draw.Graph(mi.mp, m.look), m.detail.dir)
	maxCols := min(m.cols, kitty.MaxCells)
	s := mi.res.Scale(float64(maxCols*m.cellW), float64(kitty.MaxCells*m.cellH), m.maxScale())
	mi.cols = min(maxCols, max(1, int(math.Ceil(mi.res.W*s/float64(m.cellW)))))
	mi.rows = min(kitty.MaxCells, max(1, int(math.Ceil(mi.res.H*s/float64(m.cellH)))))
	// Tab reads along the main axis, so blockers come before what the
	// focus blocks, then across it.
	main, cross := func(r layout.Rect) float64 { return r.X }, func(r layout.Rect) float64 { return r.Y }
	if m.detail.dir == layout.TB {
		main, cross = cross, main
	}
	for _, n := range mi.mp.Tickets {
		if n.Number != t.Number {
			mi.neighbours = append(mi.neighbours, n.Number)
		}
	}
	slices.SortStableFunc(mi.neighbours, func(a, b int) int {
		ra, rb := mi.res.Boxes[a], mi.res.Boxes[b]
		return cmp.Or(cmp.Compare(main(ra), main(rb)), cmp.Compare(cross(ra), cross(rb)))
	})
	return mi
}

// transform places the mini-map's boxes in its image, as Render does.
func (m Model) transform(mi *mini) draw.Transform {
	return draw.Fit(mi.res, mi.cols*m.cellW, mi.rows*m.cellH, m.maxScale())
}

// ticketRows are the first and last rows of the mini-map that ticket n
// covers.
func (m Model) ticketRows(mi *mini, n int) (int, int) {
	b := m.transform(mi).Rect(mi.res.Boxes[n])
	return int(b.Y) / m.cellH, int(math.Ceil(b.Y+b.H))/m.cellH - 1
}

// miniMapImage sends the focus ticket's mini-map, with a ring on the
// neighbour under the cursor, for the placeholders to show. There is
// nothing to send without one.
func (m Model) miniMapImage() tea.Cmd {
	l := m.detailLayout()
	mi := l.mini
	if mi == nil || len(mi.neighbours) == 0 {
		return nil
	}
	sel := l.neighbourAt(m.detail.cursor)
	img := draw.Render(mi.mp, mi.res, m.look, sel, mi.focus, mi.cols*m.cellW, mi.rows*m.cellH, m.maxScale())
	return tea.Raw(kitty.Transmit(miniMapID, img, mi.cols, mi.rows))
}

// deleteImages deletes the mini-map's image and the body's when leaving
// the detail view; Clear doesn't remove their virtual placements.
func (m Model) deleteImages() tea.Cmd {
	if m.view != detailView || !m.inlineImages() {
		return nil
	}
	return tea.Batch(tea.Raw(kitty.Delete(miniMapID)), m.deleteBodyImages())
}
