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

// strip is the rows between the graph area and the status line.
func (a *app) strip() []string {
	a.t.Helper()
	lines := strings.Split(a.view(), "\n")
	if len(lines) < 4 {
		a.t.Fatalf("no strip:\n%s", a.view())
	}
	return lines[len(lines)-4 : len(lines)-1]
}

// The selected banner's strip shows the map as a ticket's shows the ticket:
// its number and title, bold, then its state and progress.
func TestTheStripShowsTheSelectedBanner(t *testing.T) {
	for _, c := range []struct{ fixture, want string }{
		{"map-214.json", "#214 Wayfinder: run Sandcastle standalone against any Banked repo · map · open · 0/8 done"},
		{"map-edge-cases.json", "#214 Wayfinder: run Sandcastle standalone against any Banked repo · map · closed · 1/8 done"},
	} {
		src := &fakeSource{maps: map[int][]byte{214: fixture(t, c.fixture)}}
		a := start(t, src, 214).sized()
		for range 9 {
			if a.selected() == 214 {
				break
			}
			a.key("tab")
		}
		if !strings.Contains(a.text(), c.want) {
			t.Errorf("%s: the strip lacks %q:\n%s", c.fixture, c.want, a.view())
		}
		if !strings.Contains(a.view(), "\x1b[1m#214 Wayfinder: run Sandcastle standalone against any Banked repo\x1b[0m · map") {
			t.Errorf("%s: the map's number and title aren't bold, or its facts are:\n%q", c.fixture, a.view())
		}
	}
}

// A long title wraps over the strip's two title rows and is cut only when
// even two rows can't hold it; the strip stays three rows high, so the
// graph area keeps its size whichever ticket is selected.
func TestTheStripWrapsLongTitlesInAFixedHeight(t *testing.T) {
	long := strings.TrimSpace(strings.Repeat("word ", 30))
	src := &fakeSource{maps: map[int][]byte{100: mapJSON(map[int]string{
		1: "Short", 2: "A title too long for one row but fits two", 3: long}, nil)}}
	a := start(t, src, 100)
	a.send(tea.WindowSizeMsg{Width: 40, Height: 30})
	if raw := a.wait(time.Second); placements(raw) != 1 || !strings.Contains(raw[0], ",c=40,r=26,") {
		t.Fatalf("want the image over 30-3-1 = 26 rows, got %q", raw)
	}
	if s := a.strip(); ansi.Strip(s[1]) != "#1 Short · task · frontier · ⛔0 ↦0" || ansi.Strip(s[2]) != "" {
		t.Errorf("a short title doesn't fit one row:\n%s", strings.Join(s, "\n"))
	}
	for a.selected() != 2 {
		a.key("tab")
	}
	s := a.strip()
	if got := ansi.Strip(s[1] + " " + s[2]); got != "#2 A title too long for one row but fits two · task · frontier · ⛔0 ↦0" {
		t.Errorf("a two-row title isn't wrapped whole: %q", got)
	}
	for a.selected() != 3 {
		a.key("tab")
	}
	s = a.strip()
	if !strings.HasPrefix(ansi.Strip(s[1]), "#3 word word") || !strings.HasSuffix(ansi.Strip(s[2]), "… · task · frontier · ⛔0 ↦0") {
		t.Errorf("a title too long for two rows isn't cut with …:\n%s", strings.Join(s, "\n"))
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
