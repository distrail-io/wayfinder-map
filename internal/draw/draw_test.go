package draw_test

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
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
	img := draw.Render(m, res, draw.Cards, 0, int(res.W), int(res.H), 1)
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
	img := draw.Render(m, res, draw.Compact, 0, int(res.W), int(res.H), 1)
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
	img := draw.Render(m, res, draw.Cards, 218, int(res.W), int(res.H), 1)
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
	res := layout.Layered(draw.Graph(m, look), dir)
	sel := 0
	for _, tk := range m.Tickets {
		if tk.State == wayfinder.Frontier {
			sel = tk.Number
			break
		}
	}
	// A 160×36-cell pane of 9×18-pixel cells.
	img := draw.Render(m, res, look, sel, 1440, 648, 1.25)
	golden(t, fmt.Sprintf("testdata/map-%s-%s-%s.png", name, look, dir), img)
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

func diff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}
