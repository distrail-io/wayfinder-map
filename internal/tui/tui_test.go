package tui_test

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/tui"
)

type fakeSource struct {
	maps   map[int][]byte
	list   []byte
	err    error
	loaded []int
}

func (f *fakeSource) LoadMap(_ context.Context, n int) ([]byte, error) {
	f.loaded = append(f.loaded, n)
	if f.err != nil {
		return nil, f.err
	}
	return f.maps[n], nil
}

func (f *fakeSource) ListMaps(context.Context) ([]byte, error) { return f.list, f.err }

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
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

// feed sends msg to the model, then feeds back every message its commands
// produce except raw terminal output, which it returns.
func feed(t *testing.T, m tea.Model, msg tea.Msg) (tea.Model, []string, bool) {
	t.Helper()
	var raw []string
	quit := false
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		var cmd tea.Cmd
		m, cmd = m.Update(queue[0])
		queue = queue[1:]
		for _, out := range run(cmd) {
			switch out := out.(type) {
			case tea.RawMsg:
				raw = append(raw, out.Msg.(string))
			case tea.QuitMsg:
				quit = true
			default:
				queue = append(queue, out)
			}
		}
	}
	return m, raw, quit
}

func start(t *testing.T, src *fakeSource, number int) tea.Model {
	t.Helper()
	var m tea.Model = tui.New(tui.Config{
		Source:   src,
		Number:   number,
		CellSize: func(int, int) (int, int) { return 9, 18 },
	})
	for _, msg := range run(m.Init()) {
		m, _, _ = feed(t, m, msg)
	}
	return m
}

func TestShowsTheMapAsAKittyImage(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	m := start(t, src, 214)
	m, raw, _ := feed(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if len(raw) != 1 || !strings.Contains(raw[0], "\x1b_Ga=T,f=100,q=2,C=1,c=100,r=29,") {
		t.Fatalf("expected one Kitty placement over all but the status row, got %q", raw)
	}
	view := m.View().Content
	for _, want := range []string{"#214", "2 frontier", "6 blocked", "Wayfinder: run Sandcastle"} {
		if !strings.Contains(view, want) {
			t.Errorf("status line lacks %q: %q", want, view)
		}
	}
}

func TestNoMapNumberOpensTheNewestOpenMap(t *testing.T) {
	src := &fakeSource{
		maps: map[int][]byte{175: fixture(t, "map-175.json")},
		list: []byte(`{"data":{"repository":{"issues":{"nodes":[{"number":175,"title":"x"},{"number":214,"title":"y"}]}}}}`),
	}
	m := start(t, src, 0)
	if !reflect.DeepEqual(src.loaded, []int{175}) {
		t.Errorf("loaded maps %v, want [175]", src.loaded)
	}
	if view := m.View().Content; !strings.Contains(view, "#175") {
		t.Errorf("view doesn't show #175: %q", view)
	}
}

func TestNoOpenMaps(t *testing.T) {
	src := &fakeSource{list: []byte(`{"data":{"repository":{"issues":{"nodes":[]}}}}`)}
	m := start(t, src, 0)
	if view := m.View().Content; !strings.Contains(view, "no open wayfinder:map issues") {
		t.Errorf("view = %q", view)
	}
}

func TestShowsLoadErrors(t *testing.T) {
	src := &fakeSource{err: errors.New("gh: Could not resolve to an Issue")}
	m := start(t, src, 999)
	m, raw, _ := feed(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if len(raw) != 0 {
		t.Errorf("drew an image without a map: %q", raw)
	}
	if view := m.View().Content; !strings.Contains(view, "gh: Could not resolve to an Issue") {
		t.Errorf("view doesn't show the error: %q", view)
	}
}

func TestQuitClearsTheImage(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, "map-214.json")}}
	m := start(t, src, 214)
	_, raw, quit := feed(t, m, tea.KeyPressMsg{Code: 'q', Text: "q"})
	if !quit {
		t.Error("q didn't quit")
	}
	if len(raw) != 1 || raw[0] != "\x1b_Ga=d,d=A,q=2\x1b\\" {
		t.Errorf("q didn't clear the image first: %q", raw)
	}
}
