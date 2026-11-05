package draw_test

import (
	"image/color"
	"os"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

func TestBoxesTakeTheirStateColour(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/map-175.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := wayfinder.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	res := layout.Layered(draw.Graph(m), layout.LR)
	// At scale 1 the image matches layout units exactly.
	img := draw.Render(m, res, int(res.W), int(res.H), 1)
	want := map[wayfinder.State]color.RGBA{
		wayfinder.Done:     {0x4d, 0x55, 0x60, 0xff},
		wayfinder.Frontier: {0x23, 0x9a, 0x4a, 0xff},
		wayfinder.Blocked:  {0xb0, 0x3e, 0x4c, 0xff},
	}
	for _, tk := range m.Tickets {
		b := res.Boxes[tk.Number]
		if got := img.RGBAAt(int(b.X)+10, int(b.Y)+4); got != want[tk.State] {
			t.Errorf("#%d (%v) is drawn %v, want %v", tk.Number, tk.State, got, want[tk.State])
		}
	}
	if got := img.RGBAAt(0, 0); got.A != 0 {
		t.Errorf("background is %v, want transparent so the terminal theme shows", got)
	}
}
