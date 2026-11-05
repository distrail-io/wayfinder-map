package draw

// The ticket type icons are Lucide icons (https://lucide.dev), hand-converted
// from their SVG paths (lucide-icons/lucide@558bdd8) to the path operations
// below: SVG arcs became cubic corners, and strokes keep Lucide's width of 2
// on a 24-unit grid with round caps and joins.
//
//	research   search                (derived from Feather)
//	prototype  flask-conical
//	grilling   message-square
//	task       square-check
//	?          circle-question-mark
//
// ISC License
//
// Copyright (c) 2026 Lucide Icons and Contributors
//
// Permission to use, copy, modify, and/or distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
//
// The MIT License (MIT) (for icons derived from Feather, here: search)
//
// Copyright (c) 2013-present Cole Bemis
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

import (
	"image"
	"image/color"
	"math"

	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// glyph is an icon on Lucide's 24-unit grid: stroked polylines, joined and
// capped with discs at their corners, plus filled dots.
type glyph struct {
	lines   [][]layout.Point
	corners []layout.Point
	dots    []layout.Point // drawn as discs of the stroke's width
}

// pen builds one polyline of a glyph.
type pen struct {
	g   *glyph
	pts []layout.Point
}

func (g *glyph) at(x, y float64) *pen {
	g.corners = append(g.corners, layout.Point{X: x, Y: y})
	return &pen{g: g, pts: []layout.Point{{X: x, Y: y}}}
}

func (p *pen) last() layout.Point { return p.pts[len(p.pts)-1] }

func (p *pen) to(x, y float64) *pen {
	p.pts = append(p.pts, layout.Point{X: x, Y: y})
	p.g.corners = append(p.g.corners, layout.Point{X: x, Y: y})
	return p
}

func (p *pen) cubic(c1x, c1y, c2x, c2y, x, y float64) *pen {
	p0 := p.last()
	p.pts = append(p.pts, flatten([]layout.Point{p0, {X: c1x, Y: c1y}, {X: c2x, Y: c2y}, {X: x, Y: y}})[1:]...)
	p.g.corners = append(p.g.corners, layout.Point{X: x, Y: y})
	return p
}

// corner rounds from the current point to (x, y), as an SVG arc does at a
// corner whose sides meet at (cx, cy).
func (p *pen) corner(cx, cy, x, y float64) *pen {
	const k = 0.5523
	a := p.last()
	return p.cubic(a.X+k*(cx-a.X), a.Y+k*(cy-a.Y), x+k*(cx-x), y+k*(cy-y), x, y)
}

// circle traces a full circle as four quarter arcs.
func (p *pen) circle(cx, cy, r float64) *pen {
	return p.corner(cx+r, cy+r, cx, cy+r).corner(cx-r, cy+r, cx-r, cy).
		corner(cx-r, cy-r, cx, cy-r).corner(cx+r, cy-r, cx+r, cy)
}

func (p *pen) done() { p.g.lines = append(p.g.lines, p.pts) }

var glyphs = map[wayfinder.Type]*glyph{}

func init() {
	g := &glyph{} // search: <path d="m21 21-4.34-4.34"/><circle cx="11" cy="11" r="8"/>
	g.at(21, 21).to(16.66, 16.66).done()
	g.at(19, 11).circle(11, 11, 8).done()
	glyphs[wayfinder.Research] = g

	// flask-conical: <path d="M14 2v6a2 2 0 0 0 .245.96l5.51 10.08A2 2 0 0 1 18
	// 22H6a2 2 0 0 1-1.755-2.96l5.51-10.08A2 2 0 0 0 10 8V2"/>
	// <path d="M6.453 15h11.094"/><path d="M8.5 2h7"/>
	g = &glyph{}
	g.at(14, 2).to(14, 8).corner(14, 8.512, 14.245, 8.96).to(19.755, 19.04).corner(21.373, 22, 18, 22).
		to(6, 22).corner(2.627, 22, 4.245, 19.04).to(9.755, 8.96).corner(10, 8.512, 10, 8).to(10, 2).done()
	g.at(6.453, 15).to(17.547, 15).done()
	g.at(8.5, 2).to(15.5, 2).done()
	glyphs[wayfinder.Prototype] = g

	// message-square: <path d="M22 17a2 2 0 0 1-2 2H6.828a2 2 0 0 0-1.414.586
	// l-2.202 2.202A.71.71 0 0 1 2 21.286V5a2 2 0 0 1 2-2h16a2 2 0 0 1 2 2z"/>
	g = &glyph{}
	g.at(22, 17).corner(22, 19, 20, 19).to(6.828, 19).corner(6, 19, 5.414, 19.586).to(3.212, 21.788).
		corner(2, 23, 2, 21.286).to(2, 5).corner(2, 3, 4, 3).to(20, 3).corner(22, 3, 22, 5).to(22, 17).done()
	glyphs[wayfinder.Grilling] = g

	// square-check: <rect width="18" height="18" x="3" y="3" rx="2"/>
	// <path d="m16 9-5.5 5.5L8 12"/>
	g = &glyph{}
	g.at(5, 3).to(19, 3).corner(21, 3, 21, 5).to(21, 19).corner(21, 21, 19, 21).
		to(5, 21).corner(3, 21, 3, 19).to(3, 5).corner(3, 3, 5, 3).done()
	g.at(16, 9).to(10.5, 14.5).to(8, 12).done()
	glyphs[wayfinder.Task] = g

	// circle-question-mark: <circle cx="12" cy="12" r="10"/>
	// <path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>
	g = &glyph{}
	g.at(22, 12).circle(12, 12, 10).done()
	g.at(9.09, 9).cubic(10.21, 5.8, 14.93, 6.61, 14.92, 10).cubic(14.92, 12, 11.92, 13, 11.92, 13).done()
	g.dots = []layout.Point{{X: 12, Y: 17}}
	glyphs[wayfinder.NoType] = g
}

// icon draws a ticket type's icon in a size×size square at (x, y).
func icon(img *image.RGBA, typ wayfinder.Type, x, y, size float64, col color.Color) {
	g, ok := glyphs[typ]
	if !ok {
		g = glyphs[wayfinder.NoType]
	}
	s := size / 24
	w := math.Max(1.2, 2*s)
	at := func(p layout.Point) layout.Point { return layout.Point{X: x + p.X*s, Y: y + p.Y*s} }
	p := &path{}
	for _, line := range g.lines {
		pts := make([]layout.Point, len(line))
		for i, q := range line {
			pts[i] = at(q)
		}
		strokeTo(p, pts, w, 0)
	}
	for _, c := range append(g.corners, g.dots...) {
		q := at(c)
		discTo(p, q.X, q.Y, w/2)
	}
	fill(img, p, col)
}
