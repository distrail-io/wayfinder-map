// Package draw renders a laid-out map to an image, using golang.org/x/image
// only. The background is transparent so the terminal's theme shows through.
package draw

import (
	"fmt"
	"image"
	"image/color"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"

	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// Box size in layout units; one unit is one pixel at 18px-high cells.
const boxW, boxH = 96, 40

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
)

// Graph is the map as a layout graph: one box per ticket, one edge per
// in-map blocker.
func Graph(m *wayfinder.Map) layout.Graph {
	var g layout.Graph
	for _, t := range m.Tickets {
		g.Nodes = append(g.Nodes, layout.Node{ID: t.Number, W: boxW, H: boxH})
	}
	for _, e := range m.Edges {
		g.Edges = append(g.Edges, layout.Edge{From: e.From, To: e.To})
	}
	return g
}

// Render draws the layout of Graph(m) centred in a W×H image, scaled to fit
// but never above maxScale.
func Render(m *wayfinder.Map, res layout.Result, W, H int, maxScale float64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	s := math.Min(math.Min(float64(W)/res.W, float64(H)/res.H), maxScale)
	ox, oy := (float64(W)-res.W*s)/2, (float64(H)-res.H*s)/2
	pt := func(p layout.Point) layout.Point { return layout.Point{X: ox + p.X*s, Y: oy + p.Y*s} }

	for i, c := range res.Curves {
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
	fs := 13 * s
	for _, t := range m.Tickets {
		b := res.Boxes[t.Number]
		p := pt(layout.Point{X: b.X, Y: b.Y})
		w, h := b.W*s, b.H*s
		fill(img, roundRect(p.X, p.Y, w, h, 6*s), fillOf[t.State])
		tc := textOn
		if t.State == wayfinder.Done {
			tc = textDone
		}
		label := fmt.Sprintf("#%d", t.Number)
		text(img, p.X+(w-measure(label, fs))/2, p.Y+h/2+fs*0.35, label, fs, tc)
	}
	return img
}

// ---- paths, filled with the vector rasteriser over their bounding box ----

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

// stroke draws a polyline as one quad per segment; dash > 0 alternates
// on and off every dash pixels.
func stroke(img *image.RGBA, pts []layout.Point, w float64, col color.Color, dash float64) {
	p := &path{}
	dist := 0.0
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		on := dash == 0 || int(dist/dash)%2 == 0
		dist += l
		if !on {
			continue
		}
		nx, ny := -dy/l*w/2, dx/l*w/2
		ex, ey := dx/l*w/4, dy/l*w/4 // overlap neighbours slightly to hide joins
		p.moveTo(a.X+nx-ex, a.Y+ny-ey)
		p.lineTo(b.X+nx+ex, b.Y+ny+ey)
		p.lineTo(b.X-nx+ex, b.Y-ny+ey)
		p.lineTo(a.X-nx-ex, a.Y-ny-ey)
		p.close()
	}
	fill(img, p, col)
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
	bold, _ = opentype.Parse(gobold.TTF)
	faces   = map[float64]font.Face{}
)

func face(size float64) font.Face {
	size = math.Round(size*2) / 2
	if f, ok := faces[size]; ok {
		return f
	}
	f, _ := opentype.NewFace(bold, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	faces[size] = f
	return f
}

func text(img *image.RGBA, x, y float64, s string, size float64, col color.RGBA) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(col), Face: face(size), Dot: fixed.P(int(x), int(y))}
	d.DrawString(s)
}

func measure(s string, size float64) float64 {
	return float64(font.MeasureString(face(size), s)) / 64
}
