package tui_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/tui"
)

type fakeSource struct {
	maps   map[int][]byte
	list   []byte
	err    error
	loaded []int
	lists  int
}

func (f *fakeSource) LoadMap(_ context.Context, n int) ([]byte, error) {
	f.loaded = append(f.loaded, n)
	if f.err != nil {
		return nil, f.err
	}
	return f.maps[n], nil
}

func (f *fakeSource) ListMaps(context.Context) ([]byte, error) {
	f.lists++
	return f.list, f.err
}

// clock is a fake clock: timers fire only when a test advances it.
type clock struct {
	now    time.Duration
	timers []timer
}

type timer struct {
	at  time.Duration
	msg tea.Msg
}

func (c *clock) After(d time.Duration, msg tea.Msg) tea.Cmd {
	c.timers = append(c.timers, timer{c.now + d, msg})
	return nil
}

// advance moves time on by d, stepping from timer to timer and delivering
// each one's message at its time, so timers set on the way fire too.
func (c *clock) advance(d time.Duration, deliver func(tea.Msg)) {
	end := c.now + d
	for {
		i := -1
		for k, tm := range c.timers {
			if tm.at <= end && (i < 0 || tm.at < c.timers[i].at) {
				i = k
			}
		}
		if i < 0 {
			break
		}
		tm := c.timers[i]
		c.timers = slices.Delete(c.timers, i, i+1)
		c.now = tm.at
		deliver(tm.msg)
	}
	c.now = end
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// mapJSON is a map #1 in a/b whose tickets are blocked as given: blockers[n]
// lists the tickets that ticket n is blocked by.
func mapJSON(titles map[int]string, blockers map[int][]int) []byte {
	var nodes []string
	for n := 1; n <= len(titles); n++ {
		var by []string
		for _, b := range blockers[n] {
			by = append(by, fmt.Sprintf(`{"number":%d,"state":"OPEN","repository":{"nameWithOwner":"a/b"}}`, b))
		}
		nodes = append(nodes, fmt.Sprintf(`{"number":%d,"title":%q,"state":"OPEN","labels":{"nodes":[{"name":"wayfinder:task"}]},
			"assignees":{"nodes":[]},"blockedBy":{"pageInfo":{"hasNextPage":false},"nodes":[%s]}}`, n, titles[n], strings.Join(by, ",")))
	}
	return fmt.Appendf(nil, `{"data":{"repository":{"nameWithOwner":"a/b","issue":{"number":1,"title":"Map","state":"OPEN",
		"subIssuesSummary":{"total":%d},"subIssues":{"pageInfo":{"hasNextPage":false},"nodes":[%s]}}}}}`, len(titles), strings.Join(nodes, ","))
}

// run executes cmd and every command it batches or sequences, returning the
// messages they produce.
func run(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeFor[tea.Cmd]() {
		var out []tea.Msg
		for i := range v.Len() {
			out = append(out, run(v.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

type app struct {
	t      *testing.T
	m      tea.Model
	src    *fakeSource
	clock  *clock
	opened []string
	quit   bool
}

func start(t *testing.T, src *fakeSource, number int) *app {
	t.Helper()
	a := &app{t: t, src: src, clock: &clock{}}
	a.m = tui.New(tui.Config{
		Source:   src,
		Number:   number,
		CellSize: func(int, int) (int, int) { return 9, 18 },
		After:    a.clock.After,
		Open: func(url string) error {
			a.opened = append(a.opened, url)
			return nil
		},
	})
	for _, msg := range run(a.m.Init()) {
		a.send(msg)
	}
	return a
}

// send gives msg to the model, then feeds back every message its commands
// produce except raw terminal output, which it returns.
func (a *app) send(msg tea.Msg) []string {
	a.t.Helper()
	var raw []string
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		var cmd tea.Cmd
		a.m, cmd = a.m.Update(queue[0])
		queue = queue[1:]
		for _, out := range run(cmd) {
			switch out := out.(type) {
			case tea.RawMsg:
				raw = append(raw, out.Msg.(string))
			case tea.QuitMsg:
				a.quit = true
			default:
				queue = append(queue, out)
			}
		}
	}
	return raw
}

func (a *app) key(k string) []string {
	a.t.Helper()
	switch k {
	case "right", "left", "up", "down", "tab", "enter":
		code := map[string]rune{"right": tea.KeyRight, "left": tea.KeyLeft, "up": tea.KeyUp,
			"down": tea.KeyDown, "tab": tea.KeyTab, "enter": tea.KeyEnter}[k]
		return a.send(tea.KeyPressMsg{Code: code})
	}
	return a.send(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
}

// wait advances the clock by d, delivering the timers that fire.
func (a *app) wait(d time.Duration) []string {
	a.t.Helper()
	var raw []string
	a.clock.advance(d, func(msg tea.Msg) { raw = append(raw, a.send(msg)...) })
	return raw
}

func (a *app) view() string { return a.m.View().Content }

func (a *app) sized() *app {
	a.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	a.wait(time.Second)
	return a
}

// placements counts the Kitty images placed in raw output.
func placements(raw []string) int {
	n := 0
	for _, r := range raw {
		n += strings.Count(r, "\x1b_Ga=T,")
	}
	return n
}

func TestShowsTheMapAsAKittyImage(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214)
	if raw := a.send(tea.WindowSizeMsg{Width: 100, Height: 30}); len(raw) != 0 {
		t.Fatalf("placed an image before Bubble Tea drew its frame: %q", raw)
	}
	raw := a.wait(60 * time.Millisecond)
	// 30 rows less 5 for the details area and 1 for the status line; the
	// map has no warnings to add rows.
	if len(raw) != 1 || !strings.Contains(raw[0], "\x1b_Ga=T,f=100,q=2,C=1,c=100,r=24,") {
		t.Fatalf("expected one Kitty placement over the graph area, got %q", raw)
	}
	for _, want := range []string{"#214", "2 frontier", "6 blocked", "Wayfinder: run Sandcastle"} {
		if !strings.Contains(a.view(), want) {
			t.Errorf("status line lacks %q: %q", want, a.view())
		}
	}
}

func TestNoMapNumberOpensTheNewestOpenMap(t *testing.T) {
	src := &fakeSource{
		maps: map[int][]byte{175: fixture(t, "map-175.json")},
		list: []byte(`{"data":{"repository":{"issues":{"nodes":[{"number":175,"title":"x"},{"number":214,"title":"y"}]}}}}`),
	}
	a := start(t, src, 0)
	if !reflect.DeepEqual(src.loaded, []int{175}) {
		t.Errorf("loaded maps %v, want [175]", src.loaded)
	}
	if !strings.Contains(a.view(), "#175") {
		t.Errorf("view doesn't show #175: %q", a.view())
	}
}

func TestNoOpenMaps(t *testing.T) {
	src := &fakeSource{list: []byte(`{"data":{"repository":{"issues":{"nodes":[]}}}}`)}
	a := start(t, src, 0)
	if !strings.Contains(a.view(), "no open wayfinder:map issues") {
		t.Errorf("view = %q", a.view())
	}
}

func TestShowsLoadErrors(t *testing.T) {
	src := &fakeSource{err: errors.New("gh: Could not resolve to an Issue")}
	a := start(t, src, 999)
	a.send(tea.WindowSizeMsg{Width: 100, Height: 30})
	if raw := a.wait(time.Second); len(raw) != 0 {
		t.Errorf("drew an image without a map: %q", raw)
	}
	if !strings.Contains(a.view(), "gh: Could not resolve to an Issue") {
		t.Errorf("view doesn't show the error: %q", a.view())
	}
}

func TestQuitClearsTheImage(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214)
	raw := a.key("q")
	if !a.quit {
		t.Error("q didn't quit")
	}
	if len(raw) != 1 || raw[0] != "\x1b_Ga=d,d=A,q=2\x1b\\" {
		t.Errorf("q didn't clear the image first: %q", raw)
	}
}

func TestDetailsShowTheSelectedTicket(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214).sized()
	// The first frontier ticket starts selected.
	for _, want := range []string{
		"#215 Where does standalone Sandcastle live",
		"grilling · frontier · unassigned",
		"blocks: #220 · #221",
	} {
		if !strings.Contains(a.view(), want) {
			t.Errorf("details lack %q:\n%s", want, a.view())
		}
	}
}

// chain is 1 → 2, with 3 on its own beside 1.
func chain() []byte {
	return mapJSON(map[int]string{1: "One", 2: "Two", 3: "Three"}, map[int][]int{2: {1}})
}

func TestArrowsMoveToTheNearestTicket(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{1: chain()}}
	a := start(t, src, 1).sized()
	if !strings.Contains(a.view(), "#1 One") {
		t.Fatalf("#1 isn't selected first:\n%s", a.view())
	}
	if raw := a.key("right"); len(raw) != 0 {
		t.Errorf("redrew before the frame: %q", raw)
	}
	if raw := a.wait(time.Second); placements(raw) != 1 {
		t.Errorf("moving the selection placed %d images, want 1", placements(raw))
	}
	if !strings.Contains(a.view(), "#2 Two") || !strings.Contains(a.view(), "blocked by: #1 (open)") {
		t.Errorf("right from #1 didn't select #2:\n%s", a.view())
	}
	a.key("left")
	if !strings.Contains(a.view(), "#1 One") {
		t.Errorf("left from #2 didn't select #1:\n%s", a.view())
	}
	// #3 shares #1's layer, so it is either below or above.
	if a.key("down"); strings.Contains(a.view(), "#1 One") {
		a.key("up")
	}
	if !strings.Contains(a.view(), "#3 Three") {
		t.Errorf("neither down nor up from #1 reaches #3:\n%s", a.view())
	}
	// Nothing lies right of #2, so the selection stays.
	a.key("tab")
	for !strings.Contains(a.view(), "#2 Two") {
		a.key("tab")
	}
	a.key("right")
	if !strings.Contains(a.view(), "#2 Two") {
		t.Errorf("right from the last ticket moved the selection:\n%s", a.view())
	}
}

func TestTabCyclesThroughEveryTicket(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214).sized()
	seen := map[int]bool{}
	for range 8 {
		a.key("tab")
		for n := 215; n <= 222; n++ {
			if strings.Contains(a.view(), fmt.Sprintf("\n\x1b[1m#%d ", n)) {
				seen[n] = true
			}
		}
	}
	if len(seen) != 8 {
		t.Errorf("8 tabs selected %v, want all 8 tickets", seen)
	}
	if !strings.Contains(a.view(), "#215 Where does") {
		t.Errorf("8 tabs didn't come back to #215:\n%s", a.view())
	}
}

func TestOpensTheSelectedTicketInTheBrowser(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214).sized()
	a.key("o")
	a.key("tab")
	var next int
	fmt.Sscanf(a.view()[strings.Index(a.view(), "\x1b[1m#")+len("\x1b[1m#"):], "%d", &next)
	a.key("enter")
	want := []string{"https://github.com/banked/ask-barry/issues/215", fmt.Sprint("https://github.com/banked/ask-barry/issues/", next)}
	if next == 215 || !slices.Equal(a.opened, want) {
		t.Errorf("opened %q, want %q", a.opened, want)
	}
}

func TestRefreshesOnRAndEveryInterval(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214).sized()
	a.key("r")
	if len(src.loaded) != 2 {
		t.Fatalf("r loaded %d times in all, want 2", len(src.loaded))
	}
	// The poll counts from the last load, by default every 30s.
	a.wait(29 * time.Second)
	if len(src.loaded) != 2 {
		t.Errorf("polled after 29s")
	}
	a.wait(time.Second)
	if len(src.loaded) != 3 {
		t.Errorf("didn't poll after 30s: %d loads", len(src.loaded))
	}
	a.wait(30 * time.Second)
	if len(src.loaded) != 4 {
		t.Errorf("didn't poll again: %d loads", len(src.loaded))
	}
}

func TestRedrawsOnlyOnChange(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214).sized()
	// Each poll comes 30s after the last load; a redraw a frame later.
	if raw := a.wait(31 * time.Second); placements(raw) != 0 || len(src.loaded) != 2 {
		t.Errorf("an unchanged map was placed again, after %d loads", len(src.loaded))
	}
	src.maps[214] = fixture(t, "map-edge-cases.json")
	if raw := a.wait(31 * time.Second); placements(raw) != 1 {
		t.Errorf("a changed map placed %d images, want 1", placements(raw))
	}
	// #215 is still selected, and is now claimed.
	if !strings.Contains(a.view(), "grilling · claimed by octocat") {
		t.Errorf("details don't follow the change:\n%s", a.view())
	}
}

func TestAFailedRefreshKeepsTheMap(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	a := start(t, src, 214).sized()
	src.err = errors.New("gh: network down")
	raw := a.wait(30 * time.Second)
	if strings.Contains(strings.Join(raw, ""), "a=d") {
		t.Errorf("a failed refresh cleared the image")
	}
	if !strings.Contains(a.view(), "gh: network down") || !strings.Contains(a.view(), "#215 Where does") {
		t.Errorf("view should show the error and the old map:\n%s", a.view())
	}
	src.err = nil
	a.wait(30 * time.Second)
	if strings.Contains(a.view(), "network down") {
		t.Errorf("the error outlived a good refresh:\n%s", a.view())
	}
}

func TestShowsWarnings(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-edge-cases.json")}}
	a := start(t, src, 214)
	a.send(tea.WindowSizeMsg{Width: 80, Height: 30})
	// Each warning gets its own row, so a narrow pane cuts none of them.
	for _, want := range []string{
		"the map is closed",
		"truncated: showing 8 of 9 tickets",
		"truncated: more blockers than shown on #220",
		"blocked from another repo: #222 by other/repo#9",
		"no wayfinder:* type: #217",
		"cycle: #221, #222",
	} {
		if !strings.Contains(a.view(), "⚠ "+want+"\x1b[0m\n") {
			t.Errorf("view lacks warning %q:\n%s", want, a.view())
		}
	}
	// The graph gives up a row for each.
	if raw := a.wait(time.Second); len(raw) != 1 || !strings.Contains(raw[0], ",c=80,r=18,") {
		t.Errorf("want the image over 30-5-6-1 = 18 rows, got %q", raw)
	}
	for !strings.Contains(a.view(), "\x1b[1m#222 ") {
		a.key("tab")
	}
	if !strings.Contains(a.view(), "blocked by: #221 (open) · other/repo#9 (open)") {
		t.Errorf("#222's cross-repo blocker isn't shown as owner/repo#N:\n%s", a.view())
	}
}

func TestAFailedListingIsRetried(t *testing.T) {
	src := &fakeSource{err: errors.New("gh: network down"), maps: map[int][]byte{175: fixture(t, "map-175.json")}}
	a := start(t, src, 0)
	src.err = nil
	src.list = []byte(`{"data":{"repository":{"issues":{"nodes":[{"number":175,"title":"x"}]}}}}`)
	a.wait(30 * time.Second)
	if src.lists != 2 || !strings.Contains(a.view(), "#175") {
		t.Errorf("listed %d times; view:\n%s", src.lists, a.view())
	}
}
