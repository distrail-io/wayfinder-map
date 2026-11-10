package tui_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/tui"
)

func (a *app) click(x, y int) []string {
	a.t.Helper()
	return a.send(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func (a *app) wheel(down bool) []string {
	a.t.Helper()
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	return a.send(tea.MouseWheelMsg{X: 10, Y: 5, Button: b})
}

// lone is map #1 in a/b with one ticket, #7. In a 100×30 pane the two are
// drawn in the middle of the graph area: the banner over cell (50, 9) and
// #7 over (50, 16).
func lone() []byte {
	return []byte(`{"data":{"repository":{"nameWithOwner":"a/b","issue":{"number":1,"title":"Map","state":"OPEN",
		"subIssuesSummary":{"total":1},"subIssues":{"pageInfo":{"hasNextPage":false},"nodes":[{"number":7,"title":"Seven",
		"state":"OPEN","labels":{"nodes":[{"name":"wayfinder:task"}]},"assignees":{"nodes":[]},
		"blockedBy":{"pageInfo":{"hasNextPage":false},"nodes":[]}}]}}}}}`)
}

func TestMouseReportingIsOnUnlessTurnedOff(t *testing.T) {
	for _, off := range []bool{false, true} {
		want := tea.MouseModeCellMotion
		if off {
			want = tea.MouseModeNone
		}
		src := &fakeSource{list: fixture(t, "list-ask-barry.json"), maps: map[int][]byte{214: fixture(t, "map-214.json")}}
		a := startWith(t, src, tui.Config{NoMouse: off}).sized()
		if got := a.m.View().MouseMode; got != want {
			t.Errorf("NoMouse %v: the picker's mouse mode is %v, want %v", off, got, want)
		}
		a.key("down")
		a.key("enter")
		if got := a.m.View().MouseMode; got != want {
			t.Errorf("NoMouse %v: the map's mouse mode is %v, want %v", off, got, want)
		}
		a.key("enter")
		if got := a.m.View().MouseMode; got != want {
			t.Errorf("NoMouse %v: the detail view's mouse mode is %v, want %v", off, got, want)
		}
	}
}

// A click on a ticket opens it; a click beside it does nothing. In the
// compact look, only the circle counts, not the square around it.
func TestClickingATicketOpensIt(t *testing.T) {
	for _, c := range []struct {
		look       draw.Look
		corner     [2]int // a cell near the top-left corner of #7's box
		nearCorner bool   // does a click there open #7?
	}{
		{draw.Cards, [2]int{35, 15}, true},
		{draw.Compact, [2]int{46, 13}, false},
	} {
		t.Run(c.look.String(), func(t *testing.T) {
			src := &fakeSource{maps: map[int][]byte{1: lone()}}
			a := startWith(t, src, tui.Config{Number: 1, Look: c.look}).sized()
			// Outside the ticket, and on the strip and status line.
			for _, at := range [][2]int{{0, 0}, {99, 25}, {50, 27}, {50, 29}} {
				a.click(at[0], at[1])
			}
			// Right-clicks are the terminal's.
			a.send(tea.MouseClickMsg{X: 50, Y: 13, Button: tea.MouseRight})
			if len(src.issueLoaded) != 0 || strings.Contains(a.view(), "tab select · enter open") {
				t.Fatalf("a click off the ticket opened %v:\n%s", src.issueLoaded, a.view())
			}
			a.click(c.corner[0], c.corner[1])
			if got := slices.Equal(src.issueLoaded, []string{"a/b#7"}); got != c.nearCorner {
				t.Errorf("a click near the corner of #7's box opened it: %v, want %v; opened %v", got, c.nearCorner, src.issueLoaded)
			}
			if c.nearCorner {
				a.key("esc")
			}
			a.click(50, 16)
			if src.issueLoaded[len(src.issueLoaded)-1] != "a/b#7" || !a.has("#7 Seven") {
				t.Errorf("a click on #7 didn't open it: %v\n%s", src.issueLoaded, a.view())
			}
		})
	}
}

// A click on the banner selects it and opens the map's detail view, as a
// click on a ticket does; leaving comes back to the banner.
func TestClickingTheBannerOpensTheMap(t *testing.T) {
	for _, look := range []draw.Look{draw.Cards, draw.Compact} {
		t.Run(look.String(), func(t *testing.T) {
			src := &fakeSource{maps: map[int][]byte{1: lone()}}
			a := startWith(t, src, tui.Config{Number: 1, Look: look}).sized()
			a.click(50, 9)
			if !slices.Equal(src.issueLoaded, []string{"a/b#1"}) {
				t.Fatalf("a click on the banner opened %v, want the map a/b#1", src.issueLoaded)
			}
			a.key("esc")
			if a.selected() != 1 {
				t.Errorf("back on the map, #%d is selected, want the banner #1", a.selected())
			}
		})
	}
}

// The compact banner is a pill: a click just outside its rounded end, in
// the corner of the box around it, misses it, as a click beside a compact
// ticket's circle misses that; a click inside the end opens the map. In a
// 100×30 pane the pill spans cells 41 to 58 across and 8 to 11 down.
func TestClickingBesideTheCompactBannersRoundEndMissesIt(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{1: lone()}}
	a := startWith(t, src, tui.Config{Number: 1, Look: draw.Compact}).sized()
	a.click(41, 8)
	if len(src.issueLoaded) != 0 {
		t.Fatalf("a click beside the pill's rounded end opened %v", src.issueLoaded)
	}
	a.click(42, 9)
	if !slices.Equal(src.issueLoaded, []string{"a/b#1"}) {
		t.Errorf("a click inside the pill's rounded end opened %v, want the map a/b#1", src.issueLoaded)
	}
}

func TestClickingAListRowOpensIt(t *testing.T) {
	a, src := ask(t, "map-edge-cases.json", tui.Config{List: true})
	a.click(10, a.row("blocked (4)"))
	if len(src.issueLoaded) != 0 {
		t.Errorf("a click on a heading opened %v", src.issueLoaded)
	}
	a.click(10, a.row("#219 task"))
	if !slices.Equal(src.issueLoaded, []string{"banked/ask-barry#219"}) {
		t.Errorf("a click on #219's row opened %v", src.issueLoaded)
	}
	a.key("esc")
	if a.selected() != 219 {
		t.Errorf("back on the list, #%d is selected, want #219", a.selected())
	}
}

func TestClickingARelatedIssueRecentres(t *testing.T) {
	a, src := ask(t, "map-214.json", tui.Config{})
	a.key("enter")
	a.click(10, a.row("Questions"))
	a.click(10, a.row("Blocks"))
	if len(src.issueLoaded) != 1 {
		t.Errorf("a click off the related issues opened %v", src.issueLoaded[1:])
	}
	a.click(10, a.row("#221 Move Sandcastle"))
	if !slices.Equal(src.issueLoaded, []string{"banked/ask-barry#215", "banked/ask-barry#221"}) || !a.has("#221 Move Sandcastle into its own home") {
		t.Errorf("a click on #221 opened %v:\n%s", src.issueLoaded, a.view())
	}
	a.click(10, a.row("in map #214"))
	if !a.has("map · open") {
		t.Errorf("a click on the parent didn't open the map issue:\n%s", a.view())
	}
}

func TestTheWheelScrollsTheDetailViewAndThePicker(t *testing.T) {
	src := &fakeSource{list: fixture(t, "list-ask-barry.json"), maps: map[int][]byte{214: fixture(t, "map-214.json")},
		issues: map[string][]byte{"banked/ask-barry#215": fixture(t, "issue-215.json")}}
	a := start(t, src, 0).sized()
	a.wheel(true)
	a.wheel(true)
	a.wheel(false)
	if !strings.Contains(a.view(), "› #214 · ") {
		t.Fatalf("the wheel didn't move the picker's cursor to #214:\n%s", a.view())
	}
	a.key("enter")
	a.wait(time.Second)
	a.key("enter")
	a.send(tea.WindowSizeMsg{Width: 100, Height: 10})
	a.wheel(true)
	if a.row("#215 Where does") == 0 {
		t.Errorf("the wheel didn't scroll the detail view down:\n%s", a.view())
	}
	a.wheel(false)
	if a.row("#215 Where does") != 0 {
		t.Errorf("the wheel didn't scroll the detail view back up:\n%s", a.view())
	}
}
