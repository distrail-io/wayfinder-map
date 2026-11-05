// PROTOTYPE, throwaway. Drawing with Go-team-owned golang.org/x/image only
// (vector rasteriser + opentype + Go fonts): no third-party drawing library.
package main

import (
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

var (
	fillOf = map[State]color.NRGBA{
		Done:     {0x4d, 0x55, 0x60, 0xff},
		Claimed:  {0x2f, 0x6f, 0xde, 0xff},
		Frontier: {0x23, 0x9a, 0x4a, 0xff},
		Blocked:  {0xb0, 0x3e, 0x4c, 0xff},
	}
	textOn   = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	textDone = color.NRGBA{0xc4, 0xcb, 0xd2, 0xff}
	edgeCol  = color.NRGBA{0xa8, 0xb1, 0xbb, 0xe6}
	fadedCol = color.NRGBA{0xa8, 0xb1, 0xbb, 0x50}
	ringCol  = color.NRGBA{0xff, 0xd3, 0x3d, 0xff}
	laneBg   = color.NRGBA{0xff, 0xff, 0xff, 0x0c}
	laneText = color.NRGBA{0xa8, 0xb1, 0xbb, 0xff}
)

// ---- path + fill (rasteriser sized to the shape's bounding box) ----

type path struct {
	ops  []pop
	bb   [4]float32 // minx, miny, maxx, maxy
	init bool
}

type pop struct {
	k   byte
	pts []float32
}

func (p *path) add(k byte, pts ...float32) {
	for i := 0; i < len(pts); i += 2 {
		x, y := pts[i], pts[i+1]
		if !p.init {
			p.bb, p.init = [4]float32{x, y, x, y}, true
		}
		p.bb[0], p.bb[1] = min(p.bb[0], x), min(p.bb[1], y)
		p.bb[2], p.bb[3] = max(p.bb[2], x), max(p.bb[3], y)
	}
	p.ops = append(p.ops, pop{k, pts})
}
func (p *path) M(x, y float64)                { p.add('M', float32(x), float32(y)) }
func (p *path) L(x, y float64)                { p.add('L', float32(x), float32(y)) }
func (p *path) Q(a, b, x, y float64)          { p.add('Q', float32(a), float32(b), float32(x), float32(y)) }
func (p *path) C(a, b, c, d, x, y float64)    { p.add('C', float32(a), float32(b), float32(c), float32(d), float32(x), float32(y)) }
func (p *path) Z()                            { p.ops = append(p.ops, pop{k: 'Z'}) }

func fill(img *image.RGBA, p *path, col color.Color) {
	if !p.init {
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
	z.Draw(img, r, image.NewUniform(col), image.Point{})
}

func roundRect(x, y, w, h, r float64) *path {
	p := &path{}
	p.M(x+r, y)
	p.L(x+w-r, y)
	p.Q(x+w, y, x+w, y+r)
	p.L(x+w, y+h-r)
	p.Q(x+w, y+h, x+w-r, y+h)
	p.L(x+r, y+h)
	p.Q(x, y+h, x, y+h-r)
	p.L(x, y+r)
	p.Q(x, y, x+r, y)
	p.Z()
	return p
}

func circle(cx, cy, r float64) *path {
	k := 0.5523 * r
	p := &path{}
	p.M(cx+r, cy)
	p.C(cx+r, cy+k, cx+k, cy+r, cx, cy+r)
	p.C(cx-k, cy+r, cx-r, cy+k, cx-r, cy)
	p.C(cx-r, cy-k, cx-k, cy-r, cx, cy-r)
	p.C(cx+k, cy-r, cx+r, cy-k, cx+r, cy)
	p.Z()
	return p
}

// stroke draws a polyline as one quad per segment (same winding, so overlaps clamp).
// dash > 0 alternates on/off every dash pixels.
func stroke(img *image.RGBA, pts []P, w float64, col color.Color, dash float64) {
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
		// extend slightly along the segment to hide joins
		ex, ey := dx/l*w/4, dy/l*w/4
		p.M(a.X+nx-ex, a.Y+ny-ey)
		p.L(b.X+nx+ex, b.Y+ny+ey)
		p.L(b.X-nx+ex, b.Y-ny+ey)
		p.L(a.X-nx-ex, a.Y-ny-ey)
		p.Z()
	}
	fill(img, p, col)
}

func flatten(pts []P) []P {
	out := []P{pts[0]}
	for i := 1; i+2 < len(pts); i += 3 {
		p0, c1, c2, p3 := pts[i-1], pts[i], pts[i+1], pts[i+2]
		for s := 1; s <= 24; s++ {
			t := float64(s) / 24
			u := 1 - t
			out = append(out, P{
				u*u*u*p0.X + 3*u*u*t*c1.X + 3*u*t*t*c2.X + t*t*t*p3.X,
				u*u*u*p0.Y + 3*u*u*t*c1.Y + 3*u*t*t*c2.Y + t*t*t*p3.Y,
			})
		}
	}
	return out
}

func arrowhead(img *image.RGBA, tip, from P, size float64, col color.Color) {
	dx, dy := tip.X-from.X, tip.Y-from.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return
	}
	ux, uy := dx/l, dy/l
	p := &path{}
	p.M(tip.X, tip.Y)
	p.L(tip.X-ux*size-uy*size*0.55, tip.Y-uy*size+ux*size*0.55)
	p.L(tip.X-ux*size+uy*size*0.55, tip.Y-uy*size-ux*size*0.55)
	p.Z()
	fill(img, p, col)
}

// ---- text ----

var (
	fReg, _  = opentype.Parse(goregular.TTF)
	fBold, _ = opentype.Parse(gobold.TTF)
	faces    = map[string]font.Face{}
)

func face(size float64, bold bool) font.Face {
	key := fmt.Sprintf("%v/%.1f", bold, size)
	if f, ok := faces[key]; ok {
		return f
	}
	src := fReg
	if bold {
		src = fBold
	}
	f, _ := opentype.NewFace(src, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	faces[key] = f
	return f
}

func text(img *image.RGBA, x, y float64, s string, size float64, bold bool, col color.Color) {
	d := font.Drawer{Dst: img, Src: image.NewUniform(col), Face: face(size, bold), Dot: fixed.P(int(x), int(y))}
	d.DrawString(s)
}

func measure(s string, size float64, bold bool) float64 {
	return float64(font.MeasureString(face(size, bold), s)) / 64
}

// wrap breaks s into at most n lines of width w, ending in an ellipsis if cut.
func wrap(s string, size, w float64, n int) []string {
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for i, wd := range words {
		try := strings.TrimSpace(cur + " " + wd)
		if measure(try, size, false) <= w || cur == "" {
			cur = try
			continue
		}
		lines = append(lines, cur)
		cur = wd
		if len(lines) == n {
			cur = strings.Join(words[i:], " ")
			break
		}
	}
	lines = append(lines, cur)
	if len(lines) > n {
		lines = lines[:n]
		last := lines[n-1] + "…"
		for measure(last, size, false) > w && len(last) > 1 {
			r := []rune(strings.TrimSuffix(last, "…"))
			last = strings.TrimSpace(string(r[:len(r)-1])) + "…"
		}
		lines[n-1] = last
	}
	for i, l := range lines {
		for measure(l, size, false) > w && len([]rune(l)) > 2 {
			r := []rune(strings.TrimSuffix(l, "…"))
			l = string(r[:len(r)-1]) + "…"
		}
		lines[i] = l
	}
	return lines
}

// ---- type icons, drawn as strokes in a size×size square at (x, y) ----

func icon(img *image.RGBA, typ string, x, y, s float64, col color.Color) {
	w := math.Max(1.2, s*0.11)
	ring := func(cx, cy, r float64) []P {
		var pts []P
		for i := 0; i <= 32; i++ {
			a := float64(i) / 32 * 2 * math.Pi
			pts = append(pts, P{cx + r*math.Cos(a), cy + r*math.Sin(a)})
		}
		return pts
	}
	switch typ {
	case "research": // magnifier
		stroke(img, ring(x+s*0.42, y+s*0.42, s*0.28), w, col, 0)
		stroke(img, []P{{x + s*0.62, y + s*0.62}, {x + s*0.9, y + s*0.9}}, w*1.4, col, 0)
	case "prototype": // flask
		stroke(img, []P{{x + s*0.38, y + s*0.1}, {x + s*0.38, y + s*0.42}, {x + s*0.12, y + s*0.88},
			{x + s*0.88, y + s*0.88}, {x + s*0.62, y + s*0.42}, {x + s*0.62, y + s*0.1}}, w, col, 0)
		stroke(img, []P{{x + s*0.3, y + s*0.1}, {x + s*0.7, y + s*0.1}}, w, col, 0)
		stroke(img, []P{{x + s*0.25, y + s*0.68}, {x + s*0.75, y + s*0.68}}, w, col, 0)
	case "grilling": // speech bubble
		b := []P{{x + s*0.1, y + s*0.15}, {x + s*0.9, y + s*0.15}, {x + s*0.9, y + s*0.68},
			{x + s*0.45, y + s*0.68}, {x + s*0.22, y + s*0.9}, {x + s*0.26, y + s*0.68},
			{x + s*0.1, y + s*0.68}, {x + s*0.1, y + s*0.15}}
		stroke(img, b, w, col, 0)
		stroke(img, []P{{x + s*0.28, y + s*0.36}, {x + s*0.72, y + s*0.36}}, w*0.8, col, 0)
		stroke(img, []P{{x + s*0.28, y + s*0.5}, {x + s*0.6, y + s*0.5}}, w*0.8, col, 0)
	case "task": // checkbox with tick
		stroke(img, []P{{x + s*0.12, y + s*0.12}, {x + s*0.88, y + s*0.12}, {x + s*0.88, y + s*0.88},
			{x + s*0.12, y + s*0.88}, {x + s*0.12, y + s*0.12}}, w, col, 0)
		stroke(img, []P{{x + s*0.28, y + s*0.5}, {x + s*0.45, y + s*0.68}, {x + s*0.74, y + s*0.32}}, w*1.2, col, 0)
	default:
		text(img, x+s*0.28, y+s*0.82, "?", s*0.9, true, col)
	}
}

// ---- scene ----

type scene struct {
	img    *image.RGBA
	s      float64 // layout units → pixels
	ox, oy float64
}

func (c *scene) pt(p P) P       { return P{c.ox + p.X*c.s, c.oy + p.Y*c.s} }
func (c *scene) box(b Box) Box  { q := c.pt(P{b.X, b.Y}); return Box{q.X, q.Y, b.W * c.s, b.H * c.s} }

func render(m *Map, lay *Layout, look string, sel int, W, H int, maxScale float64) (*image.RGBA, float64) {
	img := image.NewRGBA(image.Rect(0, 0, W, H)) // transparent: the terminal theme shows through
	s := math.Min(math.Min(float64(W)/lay.W, float64(H)/lay.H), maxScale)
	c := &scene{img, s, (float64(W) - lay.W*s) / 2, (float64(H) - lay.H*s) / 2}
	if look == "lanes" {
		c.ox = 0
		for _, ln := range lay.Lanes {
			b := c.box(Box{4, ln.Y, lay.W - 4, ln.H})
			fill(img, roundRect(b.X, b.Y, float64(W)-b.X-4, b.H, 6*s), laneBg)
			fs := 13 * s
			text(img, b.X+8*s, b.Y+fs+8*s, strings.ToUpper(stateNames[ln.State]), fs, true, laneText)
		}
	}
	for _, e := range lay.Edges {
		col, dash := edgeCol, 0.0
		if e.Faded {
			col, dash = fadedCol, 6*s
		}
		var pts []P
		for _, p := range e.Pts {
			pts = append(pts, c.pt(p))
		}
		fl := flatten(pts)
		stroke(img, fl, math.Max(1, 1.8*s), col, dash)
		// dot's spline ends at the arrow base; push the tip to the target box edge.
		arrowhead(img, extendTo(fl, c.box(lay.Boxes[e.To])), fl[max(0, len(fl)-3)], 9*s, col)
	}
	for _, n := range m.Order {
		t := m.Tickets[n]
		b := c.box(lay.Boxes[n])
		switch look {
		case "compact":
			drawCompact(img, t, b, s, n == sel)
		case "lanes":
			drawChip(img, t, b, s, n == sel)
		default:
			drawCard(img, t, b, s, n == sel)
		}
	}
	return img, s
}

// extendTo walks the last edge segment forward until it meets the box.
func extendTo(fl []P, b Box) P {
	tip := fl[len(fl)-1]
	prev := fl[max(0, len(fl)-3)]
	dx, dy := tip.X-prev.X, tip.Y-prev.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return tip
	}
	for i := 0; i < 40; i++ {
		if tip.X >= b.X-0.5 && tip.X <= b.X+b.W+0.5 && tip.Y >= b.Y-0.5 && tip.Y <= b.Y+b.H+0.5 {
			return tip
		}
		tip = P{tip.X + dx/l, tip.Y + dy/l}
	}
	return fl[len(fl)-1]
}

func ringAround(img *image.RGBA, b Box, s, r float64) {
	o := 4 * s
	p := roundRect(b.X-o, b.Y-o, b.W+2*o, b.H+2*o, r+o)
	// reverse the inner path's winding to punch a hole
	rev := &path{}
	rev.M(b.X-o+3*s, b.Y-o+3*s)
	rev.L(b.X-o+3*s, b.Y+b.H+o-3*s)
	rev.L(b.X+b.W+o-3*s, b.Y+b.H+o-3*s)
	rev.L(b.X+b.W+o-3*s, b.Y-o+3*s)
	rev.Z()
	p.ops = append(p.ops, rev.ops...)
	fill(img, p, ringCol)
}

func drawCard(img *image.RGBA, t *Ticket, b Box, s float64, selected bool) {
	r := 8 * s
	if selected {
		ringAround(img, b, s, r)
	}
	fill(img, roundRect(b.X, b.Y, b.W, b.H, r), fillOf[t.State])
	tc := textOn
	if t.State == Done {
		tc = textDone
	}
	is := 22 * s
	icon(img, t.Type, b.X+9*s, b.Y+(b.H-is)/2, is, tc)
	tx := b.X + 9*s + is + 9*s
	tw := b.X + b.W - tx - 8*s
	fs := 12.5 * s
	text(img, tx, b.Y+fs+7*s, fmt.Sprintf("#%d", t.Num), fs*0.9, true, tc)
	for i, l := range wrap(t.Title, fs, tw, 2) {
		text(img, tx, b.Y+fs*2+12*s+float64(i)*fs*1.25, l, fs, false, tc)
	}
}

func drawCompact(img *image.RGBA, t *Ticket, b Box, s float64, selected bool) {
	d := b.W * 0.78
	cx, cy := b.X+b.W/2, b.Y+d/2
	if selected {
		fill(img, circle(cx, cy, d/2+6*s), ringCol)
		fill(img, circle(cx, cy, d/2+3*s), color.NRGBA{0x1e, 0x1e, 0x24, 0xff})
	}
	fill(img, circle(cx, cy, d/2), fillOf[t.State])
	tc := textOn
	if t.State == Done {
		tc = textDone
	}
	is := d * 0.52
	icon(img, t.Type, cx-is/2, cy-is/2, is, tc)
	fs := 11 * s
	lbl := fmt.Sprintf("#%d", t.Num)
	text(img, cx-measure(lbl, fs, true)/2, b.Y+b.H, lbl, fs, true, laneText)
}

func drawChip(img *image.RGBA, t *Ticket, b Box, s float64, selected bool) {
	r := 6 * s
	if selected {
		ringAround(img, b, s, r)
	}
	fill(img, roundRect(b.X, b.Y, b.W, b.H, r), fillOf[t.State])
	tc := textOn
	if t.State == Done {
		tc = textDone
	}
	is := 18 * s
	icon(img, t.Type, b.X+7*s, b.Y+(b.H-is)/2, is, tc)
	fs := 12 * s
	tx := b.X + 7*s + is + 7*s
	lbl := fmt.Sprintf("#%d ", t.Num)
	text(img, tx, b.Y+b.H/2+fs*0.35, lbl, fs, true, tc)
	tx += measure(lbl, fs, true)
	l := wrap(t.Title, fs, b.X+b.W-tx-6*s, 1)
	text(img, tx, b.Y+b.H/2+fs*0.35, l[0], fs, false, tc)
}
