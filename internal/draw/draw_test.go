package draw_test

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

var update = flag.Bool("update", false, "rewrite the golden images in testdata")

func load(t *testing.T, name string) *wayfinder.Map {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/map-" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wayfinder.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCardsTakeTheirStateColour(t *testing.T) {
	m := load(t, "edge-cases")
	res := layout.Layered(draw.Graph(m, draw.Cards), layout.LR)
	// At scale 1 the image matches layout units exactly.
	img := draw.Render(m, res, draw.Cards, 0, 0, int(res.W), int(res.H), 1)
	want := map[wayfinder.State]color.RGBA{
		wayfinder.Done:     {0x4d, 0x55, 0x60, 0xff},
		wayfinder.Claimed:  {0x2f, 0x6f, 0xde, 0xff},
		wayfinder.Frontier: {0x23, 0x9a, 0x4a, 0xff},
		wayfinder.Blocked:  {0xb0, 0x3e, 0x4c, 0xff},
	}
	for _, tk := range m.Tickets {
		b := res.Boxes[tk.Number]
		// Just inside the bottom-right corner, clear of the icon and text.
		if got := img.RGBAAt(int(b.X+b.W)-12, int(b.Y+b.H)-4); got != want[tk.State] {
			t.Errorf("#%d (%v) is drawn %v, want %v", tk.Number, tk.State, got, want[tk.State])
		}
	}
	if got := img.RGBAAt(0, 0); got.A != 0 {
		t.Errorf("background is %v, want transparent so the terminal theme shows", got)
	}
}

func TestCompactTicketsAreCirclesInTheirStateColour(t *testing.T) {
	m := load(t, "edge-cases")
	res := layout.Layered(draw.Graph(m, draw.Compact), layout.LR)
	img := draw.Render(m, res, draw.Compact, 0, 0, int(res.W), int(res.H), 1)
	for _, tk := range m.Tickets {
		b := res.Boxes[tk.Number]
		if b.W != b.H || b.W >= 100 {
			t.Fatalf("#%d is %vx%v, want a small square for a circle", tk.Number, b.W, b.H)
		}
		// Just inside the bottom of the circle, below the number.
		if got := img.RGBAAt(int(b.X+b.W/2), int(b.Y+b.H)-3); got != draw.FillOf(tk.State) {
			t.Errorf("#%d (%v) is drawn %v, want %v", tk.Number, tk.State, got, draw.FillOf(tk.State))
		}
		// Its bounding box's corner lies outside the circle.
		if got := img.RGBAAt(int(b.X)+2, int(b.Y)+2); got.A != 0 {
			t.Errorf("#%d: the corner of its box is %v, want transparent", tk.Number, got)
		}
	}
}

func TestSelectedTicketHasAYellowRing(t *testing.T) {
	m := load(t, "214")
	res := layout.Layered(draw.Graph(m, draw.Cards), layout.LR)
	img := draw.Render(m, res, draw.Cards, 218, 0, int(res.W), int(res.H), 1)
	yellow := color.RGBA{0xff, 0xd3, 0x3d, 0xff}
	for _, n := range []int{217, 218} {
		b := res.Boxes[n]
		// Two units left of the card, halfway down: in the ring's band.
		got := img.RGBAAt(int(b.X)-2, int(b.Y+b.H/2))
		if (got == yellow) != (n == 218) {
			t.Errorf("#%d: pixel beside the card is %v; selected is #218", n, got)
		}
	}
}

var indigo = color.RGBA{0x5b, 0x4b, 0xc4, 0xff}

// bannerFillAt is a point inside the banner b, clear of its icon and text:
// near a card's bottom-right corner, or below the middle of a pill.
func bannerFillAt(b layout.Rect, look draw.Look) image.Point {
	if look == draw.Compact {
		return image.Pt(int(b.X+b.W/2), int(b.Y+b.H)-3)
	}
	return image.Pt(int(b.X+b.W)-12, int(b.Y+b.H)-4)
}

// The banner stands for the map: a box centred above its tickets, in a
// colour no ticket state uses, with no edges.
func TestTheBannerIsIndigoAndCentredAboveTheTickets(t *testing.T) {
	m := load(t, "214")
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		for _, dir := range []layout.Dir{layout.LR, layout.TB} {
			res := draw.Layout(m, look, dir)
			img := draw.Render(m, res, look, 0, 0, int(math.Ceil(res.W)), int(math.Ceil(res.H)), 1)
			b, ok := res.Boxes[m.Number]
			if !ok {
				t.Fatalf("%v %v: no banner", look, dir)
			}
			left, right := math.Inf(1), math.Inf(-1)
			for _, tk := range m.Tickets {
				tb := res.Boxes[tk.Number]
				if tb.Y < b.Y+b.H {
					t.Errorf("%v %v: #%d at y %v is not below the banner, which ends at %v", look, dir, tk.Number, tb.Y, b.Y+b.H)
				}
				left, right = math.Min(left, tb.X), math.Max(right, tb.X+tb.W)
			}
			if mid := b.X + b.W/2; math.Abs(mid-(left+right)/2) > 1 {
				t.Errorf("%v %v: the banner's middle is at x %v, the tickets' at %v", look, dir, mid, (left+right)/2)
			}
			if len(res.Curves) != len(m.Edges) {
				t.Errorf("%v %v: %d curves for %d edges", look, dir, len(res.Curves), len(m.Edges))
			}
			if p := bannerFillAt(b, look); img.RGBAAt(p.X, p.Y) != indigo {
				t.Errorf("%v %v: the banner is %v at %v, want indigo %v", look, dir, img.RGBAAt(p.X, p.Y), p, indigo)
			}
		}
	}
}

func TestAClosedMapsBannerIsInTheDoneFill(t *testing.T) {
	m := load(t, "edge-cases")
	if !m.Closed {
		t.Fatal("the edge-cases map should be closed")
	}
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		res := draw.Layout(m, look, layout.LR)
		img := draw.Render(m, res, look, 0, 0, int(math.Ceil(res.W)), int(math.Ceil(res.H)), 1)
		p := bannerFillAt(res.Boxes[m.Number], look)
		if got, want := img.RGBAAt(p.X, p.Y), draw.FillOf(wayfinder.Done); got != want {
			t.Errorf("%v: the closed map's banner is %v, want the done fill %v", look, got, want)
		}
	}
}

func TestTheSelectedBannerHasAYellowRing(t *testing.T) {
	m := load(t, "214")
	yellow := color.RGBA{0xff, 0xd3, 0x3d, 0xff}
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		res := draw.Layout(m, look, layout.TB)
		W, H := int(math.Ceil(res.W)), int(math.Ceil(res.H))
		b := res.Boxes[m.Number]
		// Two units left of the banner, halfway down: in the ring's band.
		x, y := int(b.X)-2, int(b.Y+b.H/2)
		if got := draw.Render(m, res, look, m.Number, 0, W, H, 1).RGBAAt(x, y); got != yellow {
			t.Errorf("%v: beside the selected banner is %v, want yellow", look, got)
		}
		if got := draw.Render(m, res, look, 218, 0, W, H, 1).RGBAAt(x, y); got == yellow {
			t.Errorf("%v: the banner has a ring while #218 is selected", look)
		}
	}
}

// The mini-map shows a ticket and its neighbours only: its layout has no
// banner, so none is drawn.
func TestTheMiniMapHasNoBanner(t *testing.T) {
	m := load(t, "214").MiniMap(220)
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		res := layout.Layered(draw.Graph(m, look), layout.LR)
		img := draw.Render(m, res, look, m.Number, 220, int(math.Ceil(res.W)), int(math.Ceil(res.H)), 1)
		for y := range img.Bounds().Dy() {
			for x := range img.Bounds().Dx() {
				if near(img.RGBAAt(x, y), indigo) {
					t.Fatalf("%v: the mini-map has the banner's indigo at (%d, %d)", look, x, y)
				}
			}
		}
	}
}

// The focus ticket has a double border in its own colour, a style the map
// never uses, so it isn't mistaken for the selection's yellow ring.
func TestTheFocusTicketHasADoubleBorder(t *testing.T) {
	m := load(t, "214").MiniMap(220)
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		res := layout.Layered(draw.Graph(m, look), layout.LR)
		img := draw.Render(m, res, look, 218, 220, int(res.W), int(res.H), 1)
		for _, tk := range m.Tickets {
			b := res.Boxes[tk.Number]
			y := int(b.Y + b.H/2)
			// Beside the card: the two bands, with a gap between. Boxes
			// needn't sit on whole pixels, so the edges blur a little.
			inner, gap, outer := img.RGBAAt(int(b.X)-2, y), img.RGBAAt(int(b.X)-4, y), img.RGBAAt(int(b.X)-6, y)
			fill := draw.FillOf(tk.State)
			focused := near(inner, fill) && gap.A < tolerance && near(outer, fill)
			if focused != (tk.Number == 220) {
				t.Errorf("%v #%d: beside it %v, %v, %v; focus is #220", look, tk.Number, inner, gap, outer)
			}
		}
		// The selection keeps its ring.
		b := res.Boxes[218]
		if got := img.RGBAAt(int(b.X)-2, int(b.Y+b.H/2)); got != (color.RGBA{0xff, 0xd3, 0x3d, 0xff}) {
			t.Errorf("%v: the selection #218 has no ring: %v", look, got)
		}
	}
}

// tolerance is the most any colour channel may differ from the golden.
// Go fuses multiply-adds on arm64 but not amd64, so anti-aliased edges
// differ slightly between machines.
const tolerance = 16

func TestGoldens(t *testing.T) {
	for _, name := range []string{"152", "175", "214", "edge-cases"} {
		for _, look := range []draw.Look{draw.Cards, draw.Compact} {
			for _, dir := range []layout.Dir{layout.LR, layout.TB} {
				t.Run(fmt.Sprint(name, "-", look, "-", dir), func(t *testing.T) {
					goldenMap(t, name, look, dir)
				})
			}
		}
	}
}

func goldenMap(t *testing.T, name string, look draw.Look, dir layout.Dir) {
	m := load(t, name)
	res := draw.Layout(m, look, dir)
	sel := 0
	for _, tk := range m.Tickets {
		if tk.State == wayfinder.Frontier {
			sel = tk.Number
			break
		}
	}
	// A 160×36-cell pane of 9×18-pixel cells.
	img := draw.Render(m, res, look, sel, 0, 1440, 648, 1.25)
	golden(t, fmt.Sprintf("testdata/map-%s-%s-%s.png", name, look, dir), img)
}

// A mini-map: #220 in focus with its neighbours, and the edge between two
// of its blockers, at the map's full scale, with the cursor on #218.
func TestMiniMapGoldens(t *testing.T) {
	m := load(t, "214").MiniMap(220)
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		t.Run(look.String(), func(t *testing.T) {
			res := layout.Layered(draw.Graph(m, look), layout.LR)
			const s = 1.25
			img := draw.Render(m, res, look, 218, 220, int(math.Ceil(res.W*s)), int(math.Ceil(res.H*s)), s)
			golden(t, fmt.Sprintf("testdata/mini-map-214-220-%s.png", look), img)
		})
	}
}

func golden(t *testing.T, path string, got *image.RGBA) {
	t.Helper()
	if *update {
		var buf bytes.Buffer
		if err := png.Encode(&buf, got); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/draw -update)", err)
	}
	defer f.Close()
	want, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if want.Bounds() != got.Bounds() {
		t.Fatalf("image is %v, golden is %v", got.Bounds(), want.Bounds())
	}
	bad := 0
	var first image.Point
	b := got.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g := got.RGBAAt(x, y)
			w := color.RGBAModel.Convert(want.At(x, y)).(color.RGBA)
			if diff(g.R, w.R) > tolerance || diff(g.G, w.G) > tolerance || diff(g.B, w.B) > tolerance || diff(g.A, w.A) > tolerance {
				if bad == 0 {
					first = image.Pt(x, y)
				}
				bad++
			}
		}
	}
	if bad > 0 {
		// Kept after the test, for a look at what changed.
		out := filepath.Join(os.TempDir(), "wayfinder-map-"+filepath.Base(path))
		if f, err := os.Create(out); err == nil {
			png.Encode(f, got)
			f.Close()
		}
		t.Errorf("%d pixels differ from %s, first at %v; got %s", bad, path, first, out)
	}
}

// near reports whether a is b to within the golden tolerance.
func near(a, b color.RGBA) bool {
	return diff(a.R, b.R) <= tolerance && diff(a.G, b.G) <= tolerance && diff(a.B, b.B) <= tolerance && diff(a.A, b.A) <= tolerance
}

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
