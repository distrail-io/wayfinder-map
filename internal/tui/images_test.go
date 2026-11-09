package tui_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/distrail-io/wayfinder-map/internal/tui"
)

// The body's images' IDs, from 17 with 1 in the top byte, and their cells'
// colours.
const (
	firstImage  = "16777233"
	secondImage = "16777234"
	thirdImage  = "16777235"
)

// signed is the signed URL of #215's image in its body HTML.
const signed = "https://private-user-images.githubusercontent.com/1001/1-pilot.png?jwt=eyJhbGciOiJIUzI1NiJ9.e30.c2ln"

func deleteImage(id string) string { return "\x1b_Gq=2,i=" + id + ",d=I,a=d\x1b\\" }

// pic is a w×h PNG.
func pic(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

// imageRows is the index of each line of the view that shows the body
// image in colour c, and its width in cells.
func (a *app) imageRows(c int) (rows []int, widths []int) {
	for i, l := range strings.Split(a.view(), "\n") {
		if strings.Contains(l, fmt.Sprintf("\x1b[38;5;%dm%s", c, cell)) {
			rows, widths = append(rows, i), append(widths, strings.Count(l, cell))
		}
	}
	return rows, widths
}

// bodied is map #214 with #215's body and body HTML replaced, the images in
// pics served, and #215, the first selection, ready to open.
func bodied(t *testing.T, cfg tui.Config, body, html string, pics map[string][]byte) (*app, *fakeSource) {
	t.Helper()
	a, src := ask(t, "map-214.json", cfg)
	src.issues["banked/ask-barry#215"] = withBody(t, fixture(t, "issue-215.json"), body, html)
	a.pics = pics
	return a, src
}

// withBody is an issue's JSON with its body and body HTML replaced.
func withBody(t *testing.T, raw []byte, body, html string) []byte {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	is := v["data"].(map[string]any)["repository"].(map[string]any)["issue"].(map[string]any)
	is["body"], is["bodyHTML"] = body, html
	out, _ := json.Marshal(v)
	return out
}

// An image in the body is fetched from its signed URL, sent once, and shown
// with placeholders where it is in the text, at the size its tag asks for;
// the placeholders are text, so it scrolls with the view.
func TestBodyImagesShowInlineAndScroll(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.pics = map[string][]byte{signed: pic(1200, 400)}
	a.key("enter")
	raw := a.wait(time.Second)
	if !slices.Equal(a.fetches, []string{signed}) {
		t.Fatalf("fetched %q, want the signed URL", a.fetches)
	}
	got := transmittedAs(t, raw, firstImage)
	// <img width="600">: 600×200 pixels, 67×12 cells of 9×18.
	if len(got) != 1 || got[0].cols != 67 || got[0].rows != 12 {
		t.Fatalf("sent %+v, want one image over 67×12 cells", got)
	}
	if b := got[0].img.Bounds(); b.Dx() != 67*9 || b.Dy() != 12*18 {
		t.Errorf("the image is %v for 67×12 cells", b)
	}
	// It ends the body, at the bottom.
	a.key("G")
	rows, widths := a.imageRows(17)
	if len(rows) != 12 || rows[0] != a.row("sandcastle run --repo")+2 {
		t.Fatalf("the image is on lines %v, not under the code:\n%s", rows, a.view())
	}
	for i, w := range widths {
		if w != 67 || rows[i] != rows[0]+i {
			t.Errorf("image row %d is line %d, %d cells wide", i, rows[i], w)
		}
	}
	if a.has("🖼") {
		t.Errorf("the image is also a placeholder:\n%s", a.view())
	}
	a.key("k")
	if after, _ := a.imageRows(17); len(after) != 11 || after[0] != rows[0]+1 {
		t.Errorf("k moved the image from line %d to %v", rows[0], after)
	}
	if raw := a.wait(time.Second); len(transmittedAs(t, raw, firstImage)) != 0 {
		t.Errorf("scrolling sent the image again")
	}
}

// Until an image arrives, the rows its tag's width and height ask for are
// kept for it, saying it is loading, so the text doesn't move when it
// comes.
func TestSpaceIsKeptForAnImageWhileItLoads(t *testing.T) {
	url := "https://camo.githubusercontent.com/chart"
	a, _ := bodied(t, tui.Config{Images: true}, "before\n\n"+
		`<img width="450" height="180" alt="chart" src="https://github.com/user-attachments/assets/2">`+"\n\nafter",
		`<p>before</p><p><img width="450" height="180" alt="chart" src="`+url+`"></p><p>after</p>`,
		map[string][]byte{url: pic(900, 360)})
	a.holding = true
	a.key("enter")
	raw := a.wait(time.Second)
	at := a.row("loading image…")
	// 450×180 pixels: 50×10 cells, then a blank line.
	if at < 0 || a.row("after") != at+11 || len(transmittedAs(t, raw, firstImage)) != 0 {
		t.Fatalf("no room kept for the image while it loads:\n%s", a.view())
	}
	a.release()
	got := transmittedAs(t, a.wait(time.Second), firstImage)
	if len(got) != 1 || got[0].cols != 50 || got[0].rows != 10 {
		t.Fatalf("sent %+v, want one image over 50×10 cells", got)
	}
	if rows, _ := a.imageRows(17); len(rows) != 10 || rows[0] != at || a.row("after") != at+11 || a.has("loading image") {
		t.Errorf("the image moved the text, or isn't where it was kept:\n%s", a.view())
	}
}

// An image is scaled down to the view's width and to at most 60% of the
// pane's height, keeping its shape.
func TestImagesFitTheWidthAndMostOfTheHeight(t *testing.T) {
	tall, wide := "https://camo.githubusercontent.com/tall", "https://camo.githubusercontent.com/wide"
	a, _ := bodied(t, tui.Config{Images: true}, "![tall](t.png)\n\n![wide](w.png)",
		`<p><img src="`+tall+`"></p><p><img src="`+wide+`"></p>`,
		map[string][]byte{tall: pic(1000, 1000), wide: pic(2000, 100)})
	a.key("enter")
	raw := a.wait(time.Second)
	// 29 rows above the status line, so 17 rows at most: 306 pixels high,
	// so 306 wide, 34 cells.
	if got := transmittedAs(t, raw, firstImage); len(got) != 1 || got[0].cols != 34 || got[0].rows != 17 {
		t.Errorf("the tall image was sent as %+v, want 34×17 cells", got)
	}
	// 100 cells is 900 pixels wide, so 45 high: 3 rows.
	if got := transmittedAs(t, raw, secondImage); len(got) != 1 || got[0].cols != 100 || got[0].rows != 3 {
		t.Errorf("the wide image was sent as %+v, want 100×3 cells", got)
	}
}

// Only GitHub's image hosts are fetched from, over https; an image
// anywhere else is a placeholder.
func TestOnlyGitHubsImageHostsAreFetched(t *testing.T) {
	ok := "https://camo.githubusercontent.com/c"
	srcs := []string{"https://example.com/a.png", "http://camo.githubusercontent.com/b", ok}
	var html string
	pics := map[string][]byte{}
	for _, s := range srcs {
		html += `<p><img src="` + s + `"></p>`
		pics[s] = pic(90, 36)
	}
	a, _ := bodied(t, tui.Config{Images: true}, "![a](a.png)\n\n![b](b.png)\n\n![c](c.png)", html, pics)
	a.key("enter")
	raw := a.wait(time.Second)
	if !slices.Equal(a.fetches, []string{ok}) {
		t.Errorf("fetched %q, want only %s", a.fetches, ok)
	}
	if !a.has("🖼 a") || !a.has("🖼 b") || a.has("🖼 c") || len(transmittedAs(t, raw, thirdImage)) != 1 {
		t.Errorf("want placeholders for a and b and the image c:\n%s", a.view())
	}
}

// Images too large, too many pixels, not images, SVGs or that fail to
// load are placeholders, and aren't sent.
func TestImagesThatCantBeShownArePlaceholders(t *testing.T) {
	cases := []struct {
		alt  string
		data []byte
	}{
		{"too big", append(pic(10, 10), make([]byte, 10<<20)...)},
		{"too many pixels", pic(5000, 10)},
		{"a page", []byte("<html>sign in</html>")},
		{"a drawing", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"/>`)},
		{"gone", nil},
	}
	var body, html string
	pics := map[string][]byte{}
	for i, c := range cases {
		url := fmt.Sprintf("https://camo.githubusercontent.com/%d", i)
		body += "![" + c.alt + "](x.png)\n\n"
		html += `<p><img src="` + url + `"></p>`
		if c.data != nil {
			pics[url] = c.data
		}
	}
	a, _ := bodied(t, tui.Config{Images: true}, body, html, pics)
	a.key("enter")
	raw := a.wait(time.Second)
	for _, c := range cases {
		if !a.has("🖼 " + c.alt) {
			t.Errorf("no placeholder for %s:\n%s", c.alt, a.view())
		}
	}
	if len(a.fetches) != len(cases) {
		t.Errorf("fetched %q, want each once", a.fetches)
	}
	for n := range cases {
		if id := fmt.Sprint(16777233 + n); len(transmittedAs(t, raw, id)) != 0 {
			t.Errorf("sent image %s", cases[n].alt)
		}
	}
}

// If the body and its HTML have different numbers of images, which is
// which is unknown, so every image is a placeholder.
func TestEveryImageIsAPlaceholderIfTheCountsDiffer(t *testing.T) {
	url := "https://camo.githubusercontent.com/one"
	a, _ := bodied(t, tui.Config{Images: true}, "![one](1.png) ![two](2.png)",
		`<p><img src="`+url+`"></p>`, map[string][]byte{url: pic(90, 36)})
	a.key("enter")
	a.wait(time.Second)
	if !a.has("🖼 one") || !a.has("🖼 two") || len(a.fetches) != 0 {
		t.Errorf("fetched %q; view:\n%s", a.fetches, a.view())
	}
}

// Refreshing, on the poll or r, signs the image's URL afresh, but the
// image is the same, so it isn't fetched or sent again.
func TestRefreshingDoesntFetchImagesAgain(t *testing.T) {
	a, src := images(t, "map-214.json")
	resigned := strings.Replace(signed, "jwt=", "jwt=2", 1)
	a.pics = map[string][]byte{signed: pic(1200, 400), resigned: pic(1200, 400)}
	a.key("enter")
	a.wait(time.Second)
	src.issues["banked/ask-barry#215"] = bytes.ReplaceAll(fixture(t, "issue-215.json"), []byte(signed), []byte(resigned))
	raw := a.wait(tui.DefaultInterval)
	raw = append(raw, a.key("r")...)
	raw = append(raw, a.wait(time.Second)...)
	if len(src.issueLoaded) != 3 {
		t.Fatalf("loaded #215 %d times, want 3", len(src.issueLoaded))
	}
	if len(a.fetches) != 1 || len(transmittedAs(t, raw, firstImage)) != 0 {
		t.Errorf("refreshing fetched %q and sent the image %d times", a.fetches, len(transmittedAs(t, raw, firstImage)))
	}
	a.key("G")
	if rows, _ := a.imageRows(17); len(rows) != 12 {
		t.Errorf("the image went on refresh:\n%s", a.view())
	}
}

// Leaving the detail view, quitting, or moving to another issue deletes
// the body's images by ID. Coming back shows them again without fetching
// them again.
func TestLeavingDeletesBodyImages(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.pics = map[string][]byte{signed: pic(1200, 400)}
	a.key("enter")
	a.wait(time.Second)
	if raw := a.key("esc"); !slices.Contains(raw, deleteImage(firstImage)) || !slices.Contains(raw, deleteMini) {
		t.Errorf("esc to the map didn't delete the images: %q", raw)
	}
	a.key("enter")
	if got := transmittedAs(t, a.wait(time.Second), firstImage); len(got) != 1 || len(a.fetches) != 1 {
		t.Errorf("coming back sent the image %d times and fetched %d", len(got), len(a.fetches))
	}
	a.tabTo("in map #214")
	if raw := a.key("enter"); !slices.Contains(raw, deleteImage(firstImage)) {
		t.Errorf("moving to the map issue didn't delete the image: %q", raw)
	}
	a.key("esc")
	a.wait(time.Second)
	if raw := a.key("q"); !a.quit || !slices.Contains(raw, deleteImage(firstImage)) {
		t.Errorf("q in the detail view: quit %v, raw %q", a.quit, raw)
	}
}

// Where the mini-map isn't drawn (WezTerm, iTerm2, --no-images or list
// mode) images are placeholders, and nothing is fetched or sent.
func TestImagesArePlaceholdersWithoutInlineImages(t *testing.T) {
	for name, cfg := range map[string]tui.Config{"images off": {}, "list mode": {List: true, Images: true}} {
		t.Run(name, func(t *testing.T) {
			a, _ := ask(t, "map-214.json", cfg)
			a.pics = map[string][]byte{signed: pic(1200, 400)}
			a.key("enter")
			raw := a.wait(time.Second)
			if !a.has("🖼 pilot diagram") || len(a.fetches) != 0 || strings.Contains(strings.Join(raw, ""), "i="+firstImage) {
				t.Errorf("fetched %q; view:\n%s", a.fetches, a.view())
			}
			if raw := a.key("esc"); slices.Contains(raw, deleteImage(firstImage)) {
				t.Errorf("deleted an image never sent: %q", raw)
			}
		})
	}
}
