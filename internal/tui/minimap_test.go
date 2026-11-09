package tui_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/tui"
)

// The mini-map's image ID, 16 with 1 in its top byte, and what deletes it.
const (
	miniID     = "16777232"
	deleteMini = "\x1b_Gq=2,i=" + miniID + ",d=I,a=d\x1b\\"
	cell       = "\U0010EEEE"
)

// sent is an image sent for placeholders in raw output, and the cells it
// spans.
type sent struct {
	img        image.Image
	cols, rows int
}

// transmitted is the mini-maps sent in raw output, decoded.
func transmitted(t *testing.T, raw []string) []sent {
	t.Helper()
	return transmittedAs(t, raw, miniID)
}

// transmittedAs is the images sent with ID id in raw output, decoded.
func transmittedAs(t *testing.T, raw []string, id string) []sent {
	t.Helper()
	transmitHead := regexp.MustCompile(`\x1b_Gf=100,q=2,i=` + id + `,U=1,c=(\d+),r=(\d+),a=T[,;]`)
	var out []sent
	for _, r := range raw {
		m := transmitHead.FindStringSubmatchIndex(r)
		if m == nil {
			continue
		}
		var s sent
		fmt.Sscan(r[m[2]:m[3]], &s.cols)
		fmt.Sscan(r[m[4]:m[5]], &s.rows)
		// Each chunk's payload runs from its ; to its ST.
		var b64 strings.Builder
		for _, c := range strings.Split(r[m[0]:], "\x1b\\") {
			if _, p, ok := strings.Cut(c, ";"); ok {
				b64.WriteString(p)
			}
		}
		data, err := base64.StdEncoding.DecodeString(b64.String())
		if err != nil {
			t.Fatal(err)
		}
		if s.img, err = png.Decode(bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// placeholderRows is the index of each line of the view that shows the
// mini-map, and its width in cells.
func (a *app) placeholderRows() (rows []int, widths []int) {
	for i, l := range a.lines() {
		if n := strings.Count(l, cell); n > 0 {
			rows, widths = append(rows, i), append(widths, n)
		}
	}
	return rows, widths
}

// images is map #214 with inline images on.
func images(t *testing.T, mapFixture string) (*app, *fakeSource) {
	return ask(t, mapFixture, tui.Config{Images: true})
}

// The mini-map is sent once the frame is drawn, then shown as rows of
// placeholder cells where the Blocked by and Blocks lists would be.
func TestTheMiniMapIsShownWithPlaceholders(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.key("enter")
	raw := a.wait(time.Second)
	got := transmitted(t, raw)
	if len(got) != 1 {
		t.Fatalf("sent %d mini-maps, want 1: %q", len(got), raw)
	}
	s := got[0]
	if s.cols < 1 || s.cols > 100 || s.rows < 1 {
		t.Fatalf("the mini-map spans %d×%d cells of a 100-cell pane", s.cols, s.rows)
	}
	// The image fills its cells exactly, at 9×18 pixels a cell.
	if b := s.img.Bounds(); b.Dx() != s.cols*9 || b.Dy() != s.rows*18 {
		t.Errorf("the image is %v for %d×%d cells", b, s.cols, s.rows)
	}
	rows, widths := a.placeholderRows()
	if len(rows) != s.rows {
		t.Fatalf("%d placeholder rows, want %d:\n%s", len(rows), s.rows, a.view())
	}
	for i, r := range rows {
		if r != rows[0]+i || widths[i] != s.cols {
			t.Errorf("placeholder row %d is line %d, %d cells wide; want line %d, %d wide", i, r, widths[i], rows[0]+i, s.cols)
		}
	}
	// Each row's colour is the ID's low byte, and every cell names its row,
	// its column and the ID's top byte.
	v := a.view()
	if !strings.Contains(v, "\x1b[38;5;16m"+cell+"\u0305\u0305\u030D"+cell+"\u0305\u030D\u030D") {
		t.Errorf("the first row doesn't start with cells 0 and 1 of image %s:\n%q", miniID, v)
	}
	if !strings.Contains(v, cell+"\u030D\u0305\u030D") {
		t.Errorf("no cell names row 1:\n%q", v)
	}
	if a.has("Blocked by") || a.has("Blocks") {
		t.Errorf("the text lists show beside the mini-map:\n%s", a.view())
	}
	if placements(raw) != 0 {
		t.Errorf("placed the map over the detail view")
	}
}

var ringYellow = color.RGBA{0xff, 0xd3, 0x3d, 0xff}

// hasRing reports whether img shows the selection's yellow ring anywhere.
func hasRing(img image.Image) bool {
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)) == ringYellow {
				return true
			}
		}
	}
	return false
}

// Leaving the detail view, or quitting it, deletes the mini-map by ID, as
// clearing doesn't remove an image shown with placeholders.
func TestLeavingOrQuittingDeletesTheMiniMap(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.key("enter")
	a.wait(time.Second)
	raw := a.key("esc")
	if !slices.Contains(raw, deleteMini) {
		t.Errorf("esc to the map didn't delete the mini-map: %q", raw)
	}
	if raw := a.wait(time.Second); placements(raw) != 1 || len(transmitted(t, raw)) != 0 {
		t.Errorf("back on the map: %d placements, %d mini-maps", placements(raw), len(transmitted(t, raw)))
	}
	a.key("enter")
	a.wait(time.Second)
	raw = a.key("q")
	if !a.quit || !slices.Contains(raw, deleteMini) {
		t.Errorf("q in the detail view: quit %v, raw %q", a.quit, raw)
	}
}

// The placeholders are text, so the mini-map scrolls with the view.
func TestTheMiniMapScrollsWithTheView(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.send(tea.WindowSizeMsg{Width: 100, Height: 16})
	a.key("enter")
	a.wait(time.Second)
	before, _ := a.placeholderRows()
	a.key("j")
	after, _ := a.placeholderRows()
	if len(before) == 0 || len(after) != len(before) || after[0] != before[0]-1 {
		t.Errorf("j moved the mini-map from lines %v to %v", before, after)
	}
	if raw := a.wait(time.Second); len(raw) != 0 {
		t.Errorf("scrolling sent %q", raw)
	}
}

// Tab moves the cursor through the parent link, then the mini-map's cards,
// which show the selection's ring, redrawn once the keys stop; Enter
// re-centres on the card.
func TestTabRingsTheMiniMapsCards(t *testing.T) {
	a, src := images(t, "map-214.json")
	a.key("enter")
	if s := transmitted(t, a.wait(time.Second)); len(s) != 1 || hasRing(s[0].img) {
		t.Fatalf("the mini-map opened with a ring, or not at all")
	}
	a.key("tab")
	if l := a.row("› "); l < 0 || !strings.Contains(a.lines()[l], "in map #214") {
		t.Fatalf("tab didn't start at the parent link:\n%s", a.view())
	}
	a.wait(time.Second)
	if raw := a.key("tab"); len(raw) != 0 {
		t.Errorf("tab drew at once, not after the frame: %q", raw)
	}
	if a.row("› ") >= 0 {
		t.Errorf("the cursor's on a card, but a row is marked:\n%s", a.view())
	}
	s := transmitted(t, a.wait(time.Second))
	if len(s) != 1 || !hasRing(s[0].img) {
		t.Fatalf("the card under the cursor has no ring")
	}
	// The first card along the way is #220, as #215 blocks #221 through it.
	a.key("enter")
	if got := src.issueLoaded[len(src.issueLoaded)-1]; got != "banked/ask-barry#220" {
		t.Errorf("enter on the first card opened %s, want #220", got)
	}
	if s := transmitted(t, a.wait(time.Second)); len(s) != 1 || hasRing(s[0].img) {
		t.Errorf("re-centring didn't send a fresh mini-map without a ring")
	}
}

// Tab scrolls the card under the cursor into view, as it does a row.
func TestTabScrollsToTheCard(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.send(tea.WindowSizeMsg{Width: 100, Height: 8})
	a.key("enter")
	a.wait(time.Second)
	a.key("tab")
	if a.row("#215 Where") != 0 {
		t.Fatalf("the parent link scrolled the view:\n%s", a.view())
	}
	a.key("tab")
	if rows, _ := a.placeholderRows(); a.row("#215 Where") >= 0 || len(rows) < 4 {
		t.Errorf("tab to a card didn't scroll the mini-map into view:\n%s", a.view())
	}
}

// A click on a mini-map card re-centres on it; a click on the focus or
// beside the cards does nothing.
func TestClickingAMiniMapCard(t *testing.T) {
	a, src := images(t, "map-214.json")
	a.key("enter")
	s := transmitted(t, a.wait(time.Second))[0]
	rows, _ := a.placeholderRows()
	// Left to right: #215 in focus, #220, then #221 that both block.
	a.click(0, rows[0])
	a.click(s.cols/6, rows[len(rows)/2])
	if len(src.issueLoaded) != 1 {
		t.Fatalf("clicks off the cards or on the focus loaded %v", src.issueLoaded)
	}
	for y := rows[0]; y <= rows[len(rows)-1] && len(src.issueLoaded) == 1; y++ {
		a.click(s.cols/2, y)
	}
	if len(src.issueLoaded) != 2 || src.issueLoaded[1] != "banked/ask-barry#220" {
		t.Errorf("clicks down the middle of the mini-map loaded %v, want #220", src.issueLoaded)
	}
}

// d flips the mini-map top to bottom, and leaves the map's direction alone;
// it starts left to right on every open.
func TestDFlipsTheMiniMapNotTheMap(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.key("d") // the map: top to bottom
	a.wait(time.Second)
	a.key("enter")
	lr := transmitted(t, a.wait(time.Second))[0]
	if !a.has("d dir: LR") {
		t.Errorf("the hints don't show the mini-map's direction:\n%s", a.view())
	}
	a.key("d")
	tb := transmitted(t, a.wait(time.Second))
	if len(tb) != 1 || tb[0].rows <= lr.rows || tb[0].cols >= lr.cols || !a.has("d dir: TB") {
		t.Fatalf("d didn't flip the mini-map: %d×%d, then %v", lr.cols, lr.rows, tb)
	}
	if rows, _ := a.placeholderRows(); len(rows) != tb[0].rows {
		t.Errorf("%d placeholder rows for a mini-map %d rows high", len(rows), tb[0].rows)
	}
	a.key("esc")
	a.wait(time.Second)
	if !strings.Contains(a.strip()[0], "d dir: TB") {
		t.Errorf("the map's direction changed: %q", a.strip()[0])
	}
	a.key("enter")
	if again := transmitted(t, a.wait(time.Second)); len(again) != 1 || again[0].cols != lr.cols || !a.has("d dir: LR") {
		t.Errorf("the mini-map didn't open left to right again")
	}
}

// The compact look's circles hide the titles, so they are listed under the
// mini-map, where the cursor shows on them too.
func TestTheCompactMiniMapListsItsNeighbours(t *testing.T) {
	a, _ := images(t, "map-214.json")
	a.key("enter")
	a.wait(time.Second)
	if a.has("#220 Pilot") {
		t.Errorf("the cards look lists titles the cards show:\n%s", a.view())
	}
	a.key("c")
	if s := transmitted(t, a.wait(time.Second)); len(s) != 1 {
		t.Fatalf("c sent %d mini-maps, want 1", len(s))
	}
	rows, _ := a.placeholderRows()
	last := rows[len(rows)-1]
	if a.row("#220 Pilot: run Sandcastle against a second Banked repo · blocked") != last+1 ||
		a.row("#221 Move Sandcastle into its own home · blocked") != last+2 {
		t.Errorf("the neighbours aren't listed under the mini-map:\n%s", a.view())
	}
	a.tabTo("#220 Pilot")
	if s := transmitted(t, a.wait(time.Second)); len(s) != 1 || !hasRing(s[0].img) {
		t.Errorf("the circle under the cursor has no ring")
	}
}

// A ticket without blockers or blocked tickets says so, the map issue has
// no mini-map, and neither sends an image.
func TestNoMiniMapWithoutNeighboursOrForTheMapIssue(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{1: lone()}}
	a := startWith(t, src, tui.Config{Number: 1, Images: true}).sized()
	a.key("enter")
	raw := a.wait(time.Second)
	if !a.has("no dependencies") || len(transmitted(t, raw)) != 0 || strings.Contains(a.view(), cell) {
		t.Errorf("a lone ticket's detail view:\n%s\nraw %q", a.view(), raw)
	}
	b, _ := images(t, "map-214.json")
	b.key("m")
	raw = b.wait(time.Second)
	if len(transmitted(t, raw)) != 0 || strings.Contains(b.view(), cell) || b.has("no dependencies") {
		t.Errorf("the map issue's detail view:\n%s\nraw %q", b.view(), raw)
	}
}

// Blockers the mini-map can't draw are listed right under it.
func TestAlsoBlockedBySitsUnderTheMiniMap(t *testing.T) {
	a, _ := images(t, "map-edge-cases.json")
	for a.selected() != 222 {
		a.key("tab")
	}
	a.key("enter")
	a.wait(time.Second)
	rows, _ := a.placeholderRows()
	if len(rows) == 0 || a.row("Also blocked by") != rows[len(rows)-1]+2 || a.row("other/repo#9") != rows[len(rows)-1]+3 {
		t.Errorf("Also blocked by isn't under the mini-map:\n%s", a.view())
	}
	a.tabTo("other/repo#9")
}

// A ticket blocked only by issues the mini-map can't draw has no mini-map,
// but it doesn't say it has no dependencies, as a blocked ticket must never
// look unblocked.
func TestBlockedOnlyOffTheMapIsNotNoDependencies(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{1: offMap("other/repo", "")}}
	a := startWith(t, src, tui.Config{Number: 1, Images: true}).sized()
	a.key("enter")
	raw := a.wait(time.Second)
	if a.has("no dependencies") || !a.has("Also blocked by") || !a.has("other/repo#9 · open") {
		t.Errorf("a ticket blocked off the map:\n%s", a.view())
	}
	if len(transmitted(t, raw)) != 0 || strings.Contains(a.view(), cell) {
		t.Errorf("sent a mini-map of the lone ticket: %q", raw)
	}
}

// Without images, or in list mode, the detail view lists the relationships
// as text and writes no placeholders.
func TestWithoutImagesTheDetailViewListsRelationships(t *testing.T) {
	for _, c := range []struct {
		name string
		cfg  tui.Config
	}{
		{"images off", tui.Config{}},
		{"list mode", tui.Config{List: true, Images: true}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _ := ask(t, "map-214.json", c.cfg)
			var raw []string
			for _, k := range []string{"enter", "tab", "tab", "d", "c", "esc"} {
				raw = append(raw, a.key(k)...)
				raw = append(raw, a.wait(time.Second)...)
				if k == "enter" && (!a.has("Blocks") || !a.has("#220 Pilot") || strings.Contains(a.view(), cell) || a.has("d dir")) {
					t.Errorf("the detail view isn't the text lists:\n%s", a.view())
				}
			}
			if len(transmitted(t, raw)) != 0 || slices.Contains(raw, deleteMini) {
				t.Errorf("wrote placeholder escapes: %q", raw)
			}
			if c.cfg.List && strings.Contains(strings.Join(raw, ""), "\x1b_G") {
				t.Errorf("list mode wrote Kitty escapes: %q", raw)
			}
		})
	}
}

// The text lists stand in for the mini-map: what blocks the focus ticket,
// the focus ticket, then what it blocks.
func TestTheTextListsStackTheFocusBetweenItsBlockersAndWhatItBlocks(t *testing.T) {
	a, _ := ask(t, "map-214.json", tui.Config{})
	a.key("enter")
	a.tabTo("#220 Pilot")
	a.key("enter")
	order := []string{"Blocked by", "  #215 Where does standalone Sandcastle live", "Focus",
		"  #220 Pilot: run Sandcastle against a second Banked repo · blocked", "Blocks", "  #221 Move Sandcastle"}
	last := -1
	for _, s := range order {
		r := a.row(s)
		if r <= last {
			t.Fatalf("%q isn't after %q:\n%s", s, order[max(0, slices.Index(order, s)-1)], a.view())
		}
		last = r
	}
}
