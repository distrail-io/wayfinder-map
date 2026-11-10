package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/tui"
)

// text is the view without its styling, its rows joined by spaces, for
// text that wraps.
func (a *app) text() string { return strings.ReplaceAll(ansi.Strip(a.view()), "\n", " ") }

// strip is the three rows from the hints row, between the graph area and
// the map's warnings or the status line.
func (a *app) strip() []string {
	a.t.Helper()
	lines := strings.Split(a.view(), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "c look") && i+3 <= len(lines) {
			return lines[i : i+3]
		}
	}
	a.t.Fatalf("no strip:\n%s", a.view())
	return nil
}

// The selected banner's strip shows the map as a ticket's shows the ticket:
// its number and title, bold, on one row, then its state and progress on
// the next.
func TestTheStripShowsTheSelectedBanner(t *testing.T) {
	for _, c := range []struct{ fixture, facts string }{
		{"map-214.json", "map · open · 0/8 done"},
		{"map-edge-cases.json", "map · closed · 1/8 done"},
	} {
		src := &fakeSource{maps: map[int][]byte{214: fixture(t, c.fixture)}}
		a := start(t, src, 214).sized()
		a.tabToSelect(214)
		s := a.strip()
		if s[1] != "\x1b[1m#214 Wayfinder: run Sandcastle standalone against any Banked repo\x1b[0m" {
			t.Errorf("%s: the first row isn't the map's bold number and title: %q", c.fixture, s[1])
		}
		if s[2] != c.facts {
			t.Errorf("%s: the second row is %q, want %q", c.fixture, s[2], c.facts)
		}
	}
}

// The title takes the first row and the facts the second. A title too long
// for its row is cut with …; the strip stays three rows high, so the graph
// area keeps its size whichever ticket is selected.
func TestTheStripPutsTheTitleAboveTheFactsInAFixedHeight(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 30))
	src := &fakeSource{maps: map[int][]byte{100: mapJSON(map[int]string{1: "Short", 2: long}, map[int][]int{2: {1}})}}
	a := start(t, src, 100)
	a.send(tea.WindowSizeMsg{Width: 40, Height: 30})
	if raw := a.wait(time.Second); placements(raw) != 1 || !strings.Contains(raw[0], ",c=40,r=26,") {
		t.Fatalf("want the image over 30-3-1 = 26 rows, got %q", raw)
	}
	if s := a.strip(); s[1] != "\x1b[1m#1 Short\x1b[0m" || s[2] != "task · frontier · ⛔0 ↦1" {
		t.Errorf("a short title and its facts aren't on rows of their own:\n%s", strings.Join(s, "\n"))
	}
	a.tabToSelect(2)
	s := a.strip()
	if got := ansi.Strip(s[1]); !strings.HasPrefix(got, "#2 word word") || !strings.HasSuffix(got, "…") {
		t.Errorf("a title too long for its row isn't cut with …: %q", got)
	}
	if s[2] != "task · blocked · ⛔1 ↦0" {
		t.Errorf("a long title's facts aren't on the second row: %q", s[2])
	}
	for _, line := range s {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("a strip row is %d cells wide in a 40-cell pane: %q", w, line)
		}
	}
	if raw := a.wait(time.Second); placements(raw) != 1 || !strings.Contains(raw[0], ",c=40,r=26,") {
		t.Errorf("the graph area changed with the selection: %q", raw)
	}
}

// The hints name what c and d set, with their current values.
func TestTheHintsShowTheLookAndDirection(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{100: line()}}
	a := startWith(t, src, tui.Config{Number: 100, AutoDir: true})
	a.send(tea.WindowSizeMsg{Width: 200, Height: 30})
	a.wait(time.Second)
	if s := a.strip(); !strings.Contains(s[0], "c look: cards · d dir: auto (LR)") {
		t.Errorf("hints lack the look and direction:\n%s", s[0])
	}
	a.key("c")
	a.key("d")
	if s := a.strip(); !strings.Contains(s[0], "c look: compact · d dir: TB") {
		t.Errorf("hints don't follow c and d:\n%s", s[0])
	}
}

// In a narrow pane the legend gives way first, and the c and d hints last.
func TestNarrowPanesKeepTheLookAndDirectionHints(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{100: line()}}
	a := startWith(t, src, tui.Config{Number: 100, AutoDir: true})
	for _, c := range []struct {
		width                 int
		names, cd, otherHints bool
	}{
		{200, true, true, true},
		{140, false, true, true},
		{35, false, true, false},
		{20, false, false, false},
	} {
		a.send(tea.WindowSizeMsg{Width: c.width, Height: 30})
		row := a.strip()[0]
		if got := strings.Contains(row, "frontier"); got != c.names {
			t.Errorf("width %d: legend names shown %v, want %v: %q", c.width, got, c.names, row)
		}
		if got := strings.Contains(row, "c look: cards · d dir: auto ("); got != c.cd {
			t.Errorf("width %d: c and d hints shown %v, want %v: %q", c.width, got, c.cd, row)
		}
		if got := strings.Contains(row, "q quit"); got != c.otherHints {
			t.Errorf("width %d: other hints shown %v, want %v: %q", c.width, got, c.otherHints, row)
		}
		if !strings.Contains(row, "c look") {
			t.Errorf("width %d: the c hint is gone: %q", c.width, row)
		}
		if w := ansi.StringWidth(row); w > c.width {
			t.Errorf("width %d: the hints row is %d cells wide", c.width, w)
		}
	}
}
