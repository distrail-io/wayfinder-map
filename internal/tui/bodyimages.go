package tui

import (
	"image"
	"maps"
	"math"
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/images"
	"github.com/distrail-io/wayfinder-map/internal/kitty"
	"github.com/distrail-io/wayfinder-map/internal/markdown"
)

// The detail view shows the images in an issue's body inline, with Unicode
// placeholders like the mini-map's, where the mini-map is drawn. Each is
// fetched once, by asset, as soon as the issue arrives, since its signed
// URL soon expires; refreshing signs the URL afresh but finds the asset
// already fetched. Only an image whose fetch failed, as in a network blip,
// is fetched again on refreshing, from its fresh URL.

// picture is a body image fetched, or being fetched.
type picture struct {
	img    image.Image // nil while it loads, or if it failed
	failed bool
	retry  bool // it failed to fetch, rather than to decode
}

type pictureMsg struct {
	asset   string
	img     image.Image
	err     error
	fetched bool // the error is the decode's, not the fetch's
}

// slot is where a body image is shown: the asset and the cells it spans.
// The zero slot shows none.
type slot struct {
	asset      string
	cols, rows int
}

// imageID is the n-th body image's ID; the mini-map's is the first.
func imageID(n int) int { return kitty.ID(1 + n) }

// imageShare is the most of the view's height, in percent, an image takes.
const imageShare = 60

// fetchImages fetches the focus body's images from GitHub's image hosts:
// those not fetched, or being fetched, and those that failed to fetch.
// Without inline images there is no need.
func (m Model) fetchImages() (Model, tea.Cmd) {
	if !m.inlineImages() {
		return m, nil
	}
	var cmds []tea.Cmd
	for _, im := range m.detail.bodyImages {
		a := images.Asset(im.URL)
		p, ok := m.pictures[a]
		if (ok && !p.retry) || !images.Allowed(im.URL) {
			continue
		}
		// One fetched again stays a placeholder meanwhile, not loading.
		m = m.withPicture(a, picture{failed: p.failed})
		fetch, url := m.cfg.FetchImage, im.URL
		cmds = append(cmds, func() tea.Msg {
			data, err := fetch(url)
			if err != nil {
				return pictureMsg{asset: a, err: err}
			}
			img, err := images.Decode(data)
			return pictureMsg{a, img, err, true}
		})
	}
	return m, tea.Batch(cmds...)
}

// withPicture records an asset's picture in a copy of the pictures, as
// earlier models share the map.
func (m Model) withPicture(asset string, p picture) Model {
	pics := make(map[string]picture, len(m.pictures)+1)
	maps.Copy(pics, m.pictures)
	pics[asset] = p
	m.pictures = pics
	return m
}

// pictured takes a fetched image, or its failure, and shows it if the
// detail view is open.
func (m Model) pictured(msg pictureMsg) (tea.Model, tea.Cmd) {
	failed := msg.err != nil
	m = m.withPicture(msg.asset, picture{img: msg.img, failed: failed, retry: failed && !msg.fetched})
	if m.view != detailView || m.detail.issue == nil {
		return m, nil
	}
	return m.renderBody().redraw()
}

// renderBody renders the focus issue's body to the pane's width, with its
// images in their slots.
func (m Model) renderBody() Model {
	if m.detail.issue == nil {
		return m
	}
	slots := make([]slot, len(m.detail.bodyImages))
	m.detail.body = markdown.Render(m.detail.issue.Body, m.cols, func(n, width int) []string {
		if n >= len(slots) {
			return nil
		}
		s, lines := m.slot(n, width)
		slots[n] = s
		return lines
	})
	m.detail.slots = slots
	return m
}

// slot lays body image n out in at most width cells: its placeholders once
// it has arrived, and while it loads a line saying so, over the rows its
// tag's width and height ask for if it gives both. Without inline images,
// or for an image that can't be shown, there is no slot, and the image is a
// 🖼 placeholder.
func (m Model) slot(n, width int) (slot, []string) {
	im := m.detail.bodyImages[n]
	if !m.inlineImages() || !images.Allowed(im.URL) || m.cellW <= 0 || m.cellH <= 0 {
		return slot{}, nil
	}
	a := images.Asset(im.URL)
	p, ok := m.pictures[a]
	switch {
	case !ok || p.failed:
		return slot{}, nil
	case p.img == nil:
		lines := []string{"\x1b[2mloading image…\x1b[0m"}
		if im.Width > 0 && im.Height > 0 {
			_, rows := m.cells(im.Width, im.Height, width)
			lines = append(lines, make([]string, rows-1)...)
		}
		return slot{}, lines
	}
	// The size the tag asks for, as GitHub shows it, else the image's own.
	b := p.img.Bounds()
	w, h := b.Dx(), b.Dy()
	switch {
	case im.Width > 0 && im.Height > 0:
		w, h = im.Width, im.Height
	case im.Width > 0:
		w, h = im.Width, max(1, h*im.Width/w)
	case im.Height > 0:
		w, h = max(1, w*im.Height/h), im.Height
	}
	s := slot{asset: a}
	s.cols, s.rows = m.cells(w, h, width)
	return s, kitty.Placeholders(imageID(n), s.cols, s.rows)
}

// cells is how many cells an image of w×h pixels spans, scaled down, if
// need be, to width cells and imageShare of the view's height, keeping its
// shape.
func (m Model) cells(w, h, width int) (cols, rows int) {
	maxCols := min(width, kitty.MaxCells)
	maxRows := min(max(1, m.detailHeight()*imageShare/100), kitty.MaxCells)
	s := min(1, float64(maxCols*m.cellW)/float64(w), float64(maxRows*m.cellH)/float64(h))
	// Ceil, but not up past a whole number that rounding missed.
	ceil := func(x float64) int { return int(math.Ceil(x - 1e-9)) }
	cols = min(maxCols, max(1, ceil(float64(w)*s/float64(m.cellW))))
	rows = min(maxRows, max(1, ceil(float64(h)*s/float64(m.cellH))))
	return cols, rows
}

// unsent reports whether a body image has arrived that isn't sent at its
// size.
func (m Model) unsent() bool {
	for n, s := range m.detail.slots {
		if s.asset != "" && (n >= len(m.detail.sent) || m.detail.sent[n] != s) {
			return true
		}
	}
	return false
}

// sendImages sends each body image that has arrived and isn't sent at its
// size, scaled to fill its cells, for its placeholders to show. Scaling is
// slow, so it runs off the event loop.
func (m Model) sendImages() (Model, tea.Cmd) {
	sent := slices.Clone(m.detail.sent)
	sent = append(sent, make([]slot, max(0, len(m.detail.slots)-len(sent)))...)
	var cmds []tea.Cmd
	for n, s := range m.detail.slots {
		if s.asset == "" || sent[n] == s {
			continue
		}
		sent[n] = s
		img, id, w, h := m.pictures[s.asset].img, imageID(n), s.cols*m.cellW, s.rows*m.cellH
		cmds = append(cmds, func() tea.Msg {
			return tea.RawMsg{Msg: kitty.Transmit(id, images.Scale(img, w, h), s.cols, s.rows)}
		})
	}
	m.detail.sent = sent
	return m, tea.Batch(cmds...)
}

// deleteBodyImages deletes the body's images that were sent, when moving
// to another issue or leaving the view; Clear doesn't remove them.
func (m Model) deleteBodyImages() tea.Cmd {
	var cmds []tea.Cmd
	for n, s := range m.detail.sent {
		if s.asset != "" {
			cmds = append(cmds, tea.Raw(kitty.Delete(imageID(n))))
		}
	}
	return tea.Batch(cmds...)
}
