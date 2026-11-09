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
	m          *wayfinder.Map
	res        layout.Result
	focus      int
	cards      []int // the neighbours, in the order Tab visits them
	cols, rows int   // the cells it spans; none without neighbours
}

// images is whether the detail view shows images inline, with Unicode
// placeholders. List mode never does.
func (m Model) images() bool { return m.cfg.Images && !m.cfg.List }

// miniMap is the focus ticket's mini-map, drawn at the map's largest scale
// and shrunk only to fit the pane's width. It is nil without images, or if
// the focus isn't a ticket on the map.
func (m Model) miniMap() *mini {
	if !m.images() || m.shown == nil || m.cols <= 0 || m.cellW <= 0 || m.cellH <= 0 {
		return nil
	}
	t := m.ticket(m.focus())
	if t == nil {
		return nil
	}
	mi := &mini{m: m.shown.MiniMap(t.Number), focus: t.Number}
	if len(mi.m.Tickets) == 1 {
		return mi
	}
	mi.res = layout.Layered(draw.Graph(mi.m, m.look), m.detail.dir)
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
	for _, n := range mi.m.Tickets {
		if n.Number != t.Number {
			mi.cards = append(mi.cards, n.Number)
		}
	}
	slices.SortStableFunc(mi.cards, func(a, b int) int {
		ra, rb := mi.res.Boxes[a], mi.res.Boxes[b]
		return cmp.Or(cmp.Compare(main(ra), main(rb)), cmp.Compare(cross(ra), cross(rb)))
	})
	return mi
}

// transform places the mini-map's boxes in its image, as Render does.
func (m Model) transform(mi *mini) draw.Transform {
	return draw.Fit(mi.res, mi.cols*m.cellW, mi.rows*m.cellH, m.maxScale())
}

// cardAt is the mini-map's ticket under cell (x, y) of its image, or 0.
func (m Model) cardAt(mi *mini, x, y int) int {
	return m.hit(mi.m, mi.res, m.transform(mi), x, y)
}

// cardRows are the first and last rows of the mini-map that ticket n
// covers.
func (m Model) cardRows(mi *mini, n int) (int, int) {
	b := m.transform(mi).Rect(mi.res.Boxes[n])
	return int(b.Y) / m.cellH, int(math.Ceil(b.Y+b.H))/m.cellH - 1
}

// miniMapImage sends the focus ticket's mini-map, with a ring on the card
// under the cursor, for the placeholders to show. There is nothing to send
// without one.
func (m Model) miniMapImage() tea.Cmd {
	l := m.detailLayout()
	mi := l.mini
	if mi == nil || len(mi.cards) == 0 {
		return nil
	}
	sel := 0
	if i := m.detail.cursor - l.firstCard; i >= 0 && i < len(mi.cards) {
		sel = mi.cards[i]
	}
	img := draw.Render(mi.m, mi.res, m.look, sel, mi.focus, mi.cols*m.cellW, mi.rows*m.cellH, m.maxScale())
	return tea.Raw(kitty.Transmit(miniMapID, img, mi.cols, mi.rows))
}

// deleteImages deletes the mini-map's image and the body's when leaving
// the detail view; Clear doesn't remove their virtual placements.
func (m Model) deleteImages() tea.Cmd {
	if m.view != detailView || !m.images() {
		return nil
	}
	return tea.Batch(tea.Raw(kitty.Delete(miniMapID)), m.deleteBodyImages())
}
