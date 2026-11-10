// Package draw renders a laid-out map to an image, using golang.org/x/image
// only. The background is transparent so the terminal's theme shows through.
package draw

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// Look is how tickets are drawn: as cards (a box with the title) or compact
// (a circle with the type icon and number).
type Look int

const (
	Cards Look = iota
	Compact
)

func (l Look) String() string { return [...]string{"cards", "compact"}[l] }

// Card size and compact circle diameter in layout units; one unit is one
// pixel at 18px-high cells.
const (
	cardW, cardH = 230, 66
	circleD      = 62
)

var (
	fillOf = map[wayfinder.State]color.RGBA{
		wayfinder.Done:     {0x4d, 0x55, 0x60, 0xff},
		wayfinder.Claimed:  {0x2f, 0x6f, 0xde, 0xff},
		wayfinder.Blocked:  {0xb0, 0x3e, 0x4c, 0xff},
		wayfinder.Frontier: {0x23, 0x9a, 0x4a, 0xff},
	}
	textOn   = color.RGBA{0xff, 0xff, 0xff, 0xff}
	textDone = color.RGBA{0xc4, 0xcb, 0xd2, 0xff}
	edgeCol  = color.NRGBA{0xa8, 0xb1, 0xbb, 0xe6}
	fadedCol = color.NRGBA{0xa8, 0xb1, 0xbb, 0x50}
	ringCol  = color.RGBA{0xff, 0xd3, 0x3d, 0xff}
)

// The banner's size in layout units, by look, and the gap between it and
// the tickets below.
var bannerSize = map[Look]layout.Point{Cards: {X: 2 * cardW, Y: 84}, Compact: {X: 2 * circleD, Y: 40}}

const (
	bannerGap = 28
	margin    = 16 // the layout's margin around the drawing
)

// bannerFill is the banner's colour while the map is open: indigo, which no
// ticket state uses.
var bannerFill = color.RGBA{0x5b, 0x4b, 0xc4, 0xff}

// FillOf is the card colour of each state, for a legend.
func FillOf(s wayfinder.State) color.RGBA { return fillOf[s] }

// Graph is the map as a layout graph in a look: one node per ticket, one
// edge per in-map blocker.
func Graph(m *wayfinder.Map, look Look) layout.Graph {
	var g layout.Graph
	for _, t := range m.Tickets {
		n := layout.Node{ID: t.Number, W: cardW, H: cardH}
		if look == Compact {
			n = layout.Node{ID: t.Number, W: circleD, H: circleD, Round: true}
		}
		g.Nodes = append(g.Nodes, n)
	}
	for _, e := range m.Edges {
		g.Edges = append(g.Edges, layout.Edge{From: e.From, To: e.To})
	}
	return g
}

// Layout lays the map out in dir: Graph(m, look) in layers, with the banner
// centred above the tickets, as the box of the map's number. The banner is
// not a node and has no edges, so the layers are as Graph's alone, moved
// down to make room. The extent covers both.
func Layout(m *wayfinder.Map, look Look, dir layout.Dir) layout.Result {
	res := layout.Layered(Graph(m, look), dir)
	size := bannerSize[look]
	mid := res.W / 2
	if len(m.Tickets) > 0 {
		left, right := math.Inf(1), math.Inf(-1)
		for _, t := range m.Tickets {
			b := res.Boxes[t.Number]
			left, right = math.Min(left, b.X), math.Max(right, b.X+b.W)
		}
		mid = (left + right) / 2
	}
	// Move the tickets right if the banner would stick out to the left, and
	// down below it.
	dx, dy := math.Max(0, margin-(mid-size.X/2)), size.Y+bannerGap
	for n, b := range res.Boxes {
		res.Boxes[n] = layout.Rect{X: b.X + dx, Y: b.Y + dy, W: b.W, H: b.H}
	}
	for _, c := range res.Curves {
		for i, p := range c.Pts {
			c.Pts[i] = layout.Point{X: p.X + dx, Y: p.Y + dy}
		}
	}
	banner := layout.Rect{X: mid + dx - size.X/2, Y: margin, W: size.X, H: size.Y}
	res.Boxes[m.Number] = banner
	res.W = math.Max(res.W+dx, banner.X+banner.W+margin)
	res.H += dy
	if len(m.Tickets) == 0 {
		res.H = banner.Y + banner.H + margin
	}
	return res
}

// Transform takes layout units to pixels in the image Render draws: scaled
// by S, then offset by OX, OY to centre the drawing. Click hit-testing uses
// it too, so it finds tickets where they are drawn.
type Transform struct{ S, OX, OY float64 }

// Fit is the transform that centres res in a W×H image, scaled to fit but
// never above maxScale.
func Fit(res layout.Result, W, H int, maxScale float64) Transform {
	s := res.Scale(float64(W), float64(H), maxScale)
	return Transform{s, (float64(W) - res.W*s) / 2, (float64(H) - res.H*s) / 2}
}

// Point is p in pixels.
func (t Transform) Point(p layout.Point) layout.Point {
	return layout.Point{X: t.OX + p.X*t.S, Y: t.OY + p.Y*t.S}
}

// Rect is r in pixels.
func (t Transform) Rect(r layout.Rect) layout.Rect {
	p := t.Point(layout.Point{X: r.X, Y: r.Y})
	return layout.Rect{X: p.X, Y: p.Y, W: r.W * t.S, H: r.H * t.S}
}

// Render draws a layout of the map, from Layout or of Graph(m, look), centred
// in a W×H image, scaled to fit but never above maxScale. It draws the
// banner if the layout has a box for it, as Layout's has and a mini-map's
// hasn't. Ticket or banner sel, if any, gets a ring, and ticket focus, if
// any, a double border.
func Render(m *wayfinder.Map, res layout.Result, look Look, sel, focus int, W, H int, maxScale float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	tr := Fit(res, W, H, maxScale)
	s, pt := tr.S, tr.Point

	for i, c := range res.Curves {
		if len(c.Pts) == 0 {
			continue
		}
		col, dash := edgeCol, 0.0
		if m.Edges[i].Faded {
			col, dash = fadedCol, 6*s
		}
		pts := make([]layout.Point, len(c.Pts))
		for k, p := range c.Pts {
			pts[k] = pt(p)
		}
		line := flatten(pts)
		stroke(img, line, math.Max(1, 1.8*s), col, dash)
		arrowhead(img, line[len(line)-1], line[max(0, len(line)-3)], 9*s, col)
	}
	if b, ok := res.Boxes[m.Number]; ok {
		banner(img, m, tr.Rect(b), look, s, m.Number == sel)
	}
	for _, t := range m.Tickets {
		b := tr.Rect(res.Boxes[t.Number])
		if look == Compact {
			circle(img, t, b.X+b.W/2, b.Y+b.H/2, b.W/2, s, t.Number == sel, t.Number == focus)
		} else {
			card(img, t, b.X, b.Y, b.W, b.H, s, t.Number == sel, t.Number == focus)
		}
	}
	return img
}

// circle draws a ticket as a disc of radius r filled by state, with its
// type icon above its number.
func circle(img *image.RGBA, t *wayfinder.Ticket, cx, cy, r, s float64, selected, focus bool) {
	discBand := func(from, to float64, col color.Color) {
		p := &path{}
		discTo(p, cx, cy, r+to*s)
		discBackTo(p, cx, cy, r+from*s)
		fill(img, p, col)
	}
	if focus {
		for _, b := range focusBands {
			discBand(b[0], b[1], fillOf[t.State])
		}
	}
	if selected {
		discBand(1, 4, ringCol)
	}
	p := &path{}
	discTo(p, cx, cy, r)
	fill(img, p, fillOf[t.State])
	tc := textOn
	if t.State == wayfinder.Done {
		tc = textDone
	}
	is := 20 * s
	icon(img, t.Type, cx-is/2, cy-is-1*s, is, tc)
	fs := 11.5 * s
	num := fmt.Sprintf("#%d", t.Number)
	text(img, cx-measure(num, fs, true)/2, cy+fs+2*s, num, fs, true, tc)
}

// card draws a ticket as a rounded box filled by state, with its type icon,
// number and title wrapped to two lines.
func card(img *image.RGBA, t *wayfinder.Ticket, x, y, w, h, s float64, selected, focus bool) {
	r := 8 * s
	if focus {
		for _, b := range focusBands {
			boxBand(img, x, y, w, h, r, b[0]*s, b[1]*s, fillOf[t.State])
		}
	}
	if selected {
		boxBand(img, x, y, w, h, r, 1*s, 4*s, ringCol)
	}
	fill(img, roundRect(x, y, w, h, r), fillOf[t.State])
	tc := textOn
	if t.State == wayfinder.Done {
		tc = textDone
	}
	is := 22 * s
	icon(img, t.Type, x+9*s, y+(h-is)/2, is, tc)
	tx := x + 9*s + is + 9*s
	tw := x + w - tx - 8*s
	fs := 12.5 * s
	text(img, tx, y+fs+7*s, fmt.Sprintf("#%d", t.Number), fs*0.9, true, tc)
	for i, l := range wrap(t.Title, fs, tw, 2) {
		text(img, tx, y+fs*2+12*s+float64(i)*fs*1.25, l, fs, false, tc)
	}
}

// banner draws the map's banner in box b: a rounded box in the cards look,
// a pill in the compact look, indigo while the map is open and in the done
// fill once it is closed.
func banner(img *image.RGBA, m *wayfinder.Map, b layout.Rect, look Look, s float64, selected bool) {
	r := 10 * s
	if look == Compact {
		r = b.H / 2
	}
	if selected {
		roundBand(img, b.X, b.Y, b.W, b.H, r, 1*s, 4*s, ringCol)
	}
	col, tc := bannerFill, textOn
	if m.Closed {
		col, tc = fillOf[wayfinder.Done], textDone
	}
	fill(img, roundRect(b.X, b.Y, b.W, b.H, r), col)
	num := fmt.Sprintf("#%d", m.Number)
	if look == Compact {
		// The icon and number side by side, centred.
		is, fs := 20*s, 13*s
		w := is + 6*s + measure(num, fs, true)
		x := b.X + (b.W-w)/2
		icon(img, mapIcon, x, b.Y+(b.H-is)/2, is, tc)
		text(img, x+is+6*s, b.Y+b.H/2+fs*0.36, num, fs, true, tc)
		return
	}
	is := 30 * s
	icon(img, mapIcon, b.X+14*s, b.Y+(b.H-is)/2, is, tc)
	tx := b.X + 14*s + is + 14*s
	tw := b.X + b.W - tx - 14*s
	fs := 14 * s
	lines := wrap(m.Title, fs, tw, 2)
	// A title on one line leaves the text block centred.
	top := b.Y + fs + 9*s + float64(2-len(lines))*fs*1.25/2
	text(img, tx, top, num, fs*0.9, true, tc)
	progress := fmt.Sprintf("%d/%d done", m.Done(), len(m.Tickets))
	text(img, tx+tw-measure(progress, fs*0.9, true), top, progress, fs*0.9, true, tc)
	for i, l := range lines {
		text(img, tx, top+fs*1.45+float64(i)*fs*1.25, l, fs, false, tc)
	}
}

// roundBand draws a band around a rounded box, from offset in to out beyond
// its edge, following its corners, so it rings a pill as well as a card.
func roundBand(img *image.RGBA, x, y, w, h, r, in, out float64, col color.Color) {
	p := roundRect(x-out, y-out, w+2*out, h+2*out, r+out)
	roundRectBackTo(p, x-in, y-in, w+2*in, h+2*in, r+in)
	fill(img, p, col)
}

// focusBands are the focus ticket's double border: two bands in its own
// colour, as offsets from its edge in layout units. The selection's ring
// lies over the first.
var focusBands = [][2]float64{{1, 3}, {5, 7}}

// boxBand draws a band around a card, from offset in to out beyond its edge,
// punching out its middle by winding the inner path the other way.
func boxBand(img *image.RGBA, x, y, w, h, r, in, out float64, col color.Color) {
	o := out
	p := roundRect(x-o, y-o, w+2*o, h+2*o, r+o)
	p.moveTo(x-in, y-in)
	p.lineTo(x-in, y+h+in)
	p.lineTo(x+w+in, y+h+in)
	p.lineTo(x+w+in, y-in)
	p.close()
	fill(img, p, col)
}

// ---- paths, filled with the vector rasteriser over their bounding box ----
//
// The rasteriser takes the absolute winding, so overlapping shapes must wind
// the same way to add up, and opposite windings cancel out.

type path struct {
	ops []op
	bb  [4]float32 // minx, miny, maxx, maxy
	any bool
}

type op struct {
	k   byte // M, L, Q, C or Z
	pts []float32
}

func (p *path) add(k byte, pts ...float32) {
	for i := 0; i < len(pts); i += 2 {
		x, y := pts[i], pts[i+1]
		if !p.any {
			p.bb, p.any = [4]float32{x, y, x, y}, true
		}
		p.bb = [4]float32{min(p.bb[0], x), min(p.bb[1], y), max(p.bb[2], x), max(p.bb[3], y)}
	}
	p.ops = append(p.ops, op{k, pts})
}

func (p *path) moveTo(x, y float64) { p.add('M', float32(x), float32(y)) }
func (p *path) lineTo(x, y float64) { p.add('L', float32(x), float32(y)) }
func (p *path) quadTo(a, b, x, y float64) {
	p.add('Q', float32(a), float32(b), float32(x), float32(y))
}
func (p *path) cubeTo(a, b, c, d, x, y float64) {
	p.add('C', float32(a), float32(b), float32(c), float32(d), float32(x), float32(y))
}
func (p *path) close() { p.ops = append(p.ops, op{k: 'Z'}) }

func fill(img *image.RGBA, p *path, col color.Color) {
	if !p.any {
		return
	}
	r := image.Rect(int(math.Floor(float64(p.bb[0])))-1, int(math.Floor(float64(p.bb[1])))-1,
		int(math.Ceil(float64(p.bb[2])))+1, int(math.Ceil(float64(p.bb[3])))+1).Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	z := vector.NewRasterizer(r.Dx(), r.Dy())
	ox, oy := float32(r.Min.X), float32(r.Min.Y)
	for _, o := range p.ops {
		q := o.pts
		switch o.k {
		case 'M':
			z.MoveTo(q[0]-ox, q[1]-oy)
		case 'L':
			z.LineTo(q[0]-ox, q[1]-oy)
		case 'Q':
			z.QuadTo(q[0]-ox, q[1]-oy, q[2]-ox, q[3]-oy)
		case 'C':
			z.CubeTo(q[0]-ox, q[1]-oy, q[2]-ox, q[3]-oy, q[4]-ox, q[5]-oy)
		case 'Z':
			z.ClosePath()
		}
	}
	z.Draw(img, r, image.NewUniform(col), r.Min)
}

func roundRect(x, y, w, h, r float64) *path {
	p := &path{}
	p.moveTo(x+r, y)
	p.lineTo(x+w-r, y)
	p.quadTo(x+w, y, x+w, y+r)
	p.lineTo(x+w, y+h-r)
	p.quadTo(x+w, y+h, x+w-r, y+h)
	p.lineTo(x+r, y+h)
	p.quadTo(x, y+h, x, y+h-r)
	p.lineTo(x, y+r)
	p.quadTo(x, y, x+r, y)
	p.close()
	return p
}

// roundRectBackTo adds roundRect's box to p wound the other way, to punch a
// hole in it.
func roundRectBackTo(p *path, x, y, w, h, r float64) {
	p.moveTo(x+r, y)
	p.quadTo(x, y, x, y+r)
	p.lineTo(x, y+h-r)
	p.quadTo(x, y+h, x+r, y+h)
	p.lineTo(x+w-r, y+h)
	p.quadTo(x+w, y+h, x+w, y+h-r)
	p.lineTo(x+w, y+r)
	p.quadTo(x+w, y, x+w-r, y)
	p.close()
}

// strokeTo adds a polyline to p as one quad per segment, all wound the same
// way; dash > 0 alternates on and off every dash pixels.
func strokeTo(p *path, pts []layout.Point, w, dash float64) {
	on, left := true, dash // whether the current dash is drawn, and how much of it is left
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		l := math.Hypot(b.X-a.X, b.Y-a.Y)
		if l == 0 {
			continue
		}
		if dash == 0 {
			quad(p, a, b, l, w)
			continue
		}
		lerp := func(u float64) layout.Point {
			return layout.Point{X: a.X + (b.X-a.X)*u/l, Y: a.Y + (b.Y-a.Y)*u/l}
		}
		// Cut the segment where dashes start and end.
		for t := 0.0; t < l; {
			step := math.Min(left, l-t)
			if on {
				quad(p, lerp(t), lerp(t+step), step, w)
			}
			t += step
			if left -= step; left <= 0 {
				on, left = !on, dash
			}
		}
	}
}

// quad adds the segment a–b, of length l, as a quad w wide.
func quad(p *path, a, b layout.Point, l, w float64) {
	if l <= 0 {
		return
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	nx, ny := -dy/l*w/2, dx/l*w/2
	ex, ey := dx/l*w/4, dy/l*w/4 // overlap neighbours slightly to hide joins
	p.moveTo(a.X+nx-ex, a.Y+ny-ey)
	p.lineTo(b.X+nx+ex, b.Y+ny+ey)
	p.lineTo(b.X-nx+ex, b.Y-ny+ey)
	p.lineTo(a.X-nx-ex, a.Y-ny-ey)
	p.close()
}

// stroke draws a polyline w wide, dashed if dash > 0.
func stroke(img *image.RGBA, pts []layout.Point, w float64, col color.Color, dash float64) {
	p := &path{}
	strokeTo(p, pts, w, dash)
	fill(img, p, col)
}

// discTo adds a circle to p, wound the same way as strokeTo's quads, so it
// rounds their joins and caps.
func discTo(p *path, cx, cy, r float64) {
	k := 0.5523 * r
	p.moveTo(cx+r, cy)
	p.cubeTo(cx+r, cy-k, cx+k, cy-r, cx, cy-r)
	p.cubeTo(cx-k, cy-r, cx-r, cy-k, cx-r, cy)
	p.cubeTo(cx-r, cy+k, cx-k, cy+r, cx, cy+r)
	p.cubeTo(cx+k, cy+r, cx+r, cy+k, cx+r, cy)
	p.close()
}

// discBackTo adds a circle wound against discTo's, to punch a hole in it.
func discBackTo(p *path, cx, cy, r float64) {
	k := 0.5523 * r
	p.moveTo(cx+r, cy)
	p.cubeTo(cx+r, cy+k, cx+k, cy+r, cx, cy+r)
	p.cubeTo(cx-k, cy+r, cx-r, cy+k, cx-r, cy)
	p.cubeTo(cx-r, cy-k, cx-k, cy-r, cx, cy-r)
	p.cubeTo(cx+k, cy-r, cx+r, cy-k, cx+r, cy)
	p.close()
}

// flatten turns cubic Bézier pieces into a polyline.
func flatten(pts []layout.Point) []layout.Point {
	out := []layout.Point{pts[0]}
	for i := 1; i+2 < len(pts); i += 3 {
		p0, c1, c2, p3 := pts[i-1], pts[i], pts[i+1], pts[i+2]
		for k := 1; k <= 24; k++ {
			t := float64(k) / 24
			u := 1 - t
			out = append(out, layout.Point{
				X: u*u*u*p0.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*p3.X,
				Y: u*u*u*p0.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*p3.Y,
			})
		}
	}
	return out
}

func arrowhead(img *image.RGBA, tip, from layout.Point, size float64, col color.Color) {
	dx, dy := tip.X-from.X, tip.Y-from.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	ux, uy := dx/l, dy/l
	p := &path{}
	p.moveTo(tip.X, tip.Y)
	p.lineTo(tip.X-ux*size-uy*size*0.55, tip.Y-uy*size+ux*size*0.55)
	p.lineTo(tip.X-ux*size+uy*size*0.55, tip.Y-uy*size-ux*size*0.55)
	p.close()
	fill(img, p, col)
}

// ---- text ----

var (
	regular, _ = opentype.Parse(goregular.TTF)
	bold, _    = opentype.Parse(gobold.TTF)
	facesMu    sync.Mutex
	faces      = map[faceKey]font.Face{}
)

type faceKey struct {
	size float64
	bold bool
}

func face(size float64, isBold bool) font.Face {
	key := faceKey{math.Round(size*2) / 2, isBold}
	src := regular
	if isBold {
		src = bold
	}
	facesMu.Lock()
	defer facesMu.Unlock()
	if f, ok := faces[key]; ok {
		return f
	}
	f, _ := opentype.NewFace(src, &opentype.FaceOptions{Size: key.size, DPI: 72, Hinting: font.HintingFull})
	faces[key] = f
	return f
}

func text(img *image.RGBA, x, y float64, s string, size float64, isBold bool, col color.RGBA) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(col), Face: face(size, isBold), Dot: fixed.P(int(x), int(y))}
	d.DrawString(s)
}

func measure(s string, size float64, isBold bool) float64 {
	return float64(font.MeasureString(face(size, isBold), s)) / 64
}

// wrap breaks s into at most n lines no wider than w, ending in an ellipsis
// if anything is cut.
func wrap(s string, size, w float64, n int) []string {
	fits := func(l string) bool { return measure(l, size, false) <= w }
	// shorten cuts runes off l until it fits with an ellipsis.
	shorten := func(l string) string {
		r := []rune(strings.TrimSuffix(l, "…"))
		for len(r) > 0 && !fits(string(r)+"…") {
			r = r[:len(r)-1]
		}
		return strings.TrimSpace(string(r)) + "…"
	}
	var lines []string
	cur := ""
	words := strings.Fields(s)
	for i, wd := range words {
		if try := strings.TrimSpace(cur + " " + wd); cur == "" || fits(try) {
			cur = try
			continue
		}
		lines = append(lines, cur)
		if len(lines) == n {
			cur = strings.Join(words[i:], " ")
			break
		}
		cur = wd
	}
	if cur != "" || len(lines) == 0 {
		lines = append(lines, cur)
	}
	cut := len(lines) > n
	lines = lines[:min(n, len(lines))]
	for i, l := range lines {
		if !fits(l) || (cut && i == n-1) {
			lines[i] = shorten(l)
		}
	}
	return lines
}
