// Package tui is the Bubble Tea app. A picker lists the repo's open maps; a
// map is shown as a Kitty-graphics image, or as a list grouped by state,
// with the selected ticket's details and a status line below. Both refresh
// on an interval, and the map is redrawn only when it changes.
package tui

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/github"
	"github.com/distrail-io/wayfinder-map/internal/kitty"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

type Config struct {
	Source github.Source
	Number int // 0 opens the picker

	// Interval is how often the map is reloaded. Zero means DefaultInterval.
	Interval time.Duration

	// Look is how the graph draws tickets; c toggles it. List shows the
	// tickets as text instead, for a terminal without Kitty graphics.
	Look draw.Look
	List bool
	// Dir is the graph's direction, unless AutoDir picks the one that draws
	// larger in the pane, again on every resize, until d toggles it.
	Dir     layout.Dir
	AutoDir bool

	// CellSize reports a cell's size in pixels. Nil reads it from stdout.
	CellSize func(cols, rows int) (w, h int)
	// After delivers msg once d has passed. Nil uses the real clock.
	After func(d time.Duration, msg tea.Msg) tea.Cmd
	// Open shows a URL in the browser. Nil runs the platform's opener.
	Open func(url string) error
}

const DefaultInterval = 30 * time.Second

type Model struct {
	cfg Config

	view view

	// The picker.
	list *wayfinder.List // nil until the first listing arrives
	pick int             // the number of the map under the cursor

	number     int // the map shown, or being loaded
	shown      *wayfinder.Map
	look       draw.Look
	layouts    [2]layout.Result // the shown map in the look, by direction
	dir        layout.Dir
	autoDir    bool
	sel        int // the selected ticket's number
	err        error
	loading    bool
	cols, rows int
	cellW      int
	cellH      int
	gen        int // the latest scheduled draw
	pollGen    int // the latest scheduled poll
}

// view is what fills the pane: the picker, a map, or the detail view of
// one issue.
type view int

const (
	pickerView view = iota
	mapView
)

type (
	listedMsg struct {
		l   wayfinder.List
		err error
	}
	loadedMsg struct {
		number int
		m      *wayfinder.Map
		err    error
	}
	openedMsg struct{ err error }
	drawMsg   int
	pollMsg   int
)

// drawDelay lets Bubble Tea paint its frame before the image goes over it.
const drawDelay = 60 * time.Millisecond

// The details area below the graph: a legend, then the selected ticket's
// title, type and state, blockers and what it blocks. Then a row for each
// of the map's warnings, up to maxWarningRows, and the status line last.
const (
	detailRows     = 5
	maxWarningRows = 6
	statusRows     = 1
)

func New(cfg Config) Model {
	if cfg.CellSize == nil {
		cfg.CellSize = stdoutCellSize
	}
	if cfg.Interval == 0 {
		cfg.Interval = DefaultInterval
	}
	if cfg.After == nil {
		cfg.After = func(d time.Duration, msg tea.Msg) tea.Cmd {
			return tea.Tick(d, func(time.Time) tea.Msg { return msg })
		}
	}
	if cfg.Open == nil {
		cfg.Open = openURL
	}
	v := mapView
	if cfg.Number == 0 {
		v = pickerView
	}
	return Model{cfg: cfg, number: cfg.Number, view: v, loading: true,
		look: cfg.Look, dir: cfg.Dir, autoDir: cfg.AutoDir}
}

func stdoutCellSize(cols, rows int) (int, int) {
	if w, h, ok := kitty.CellSize(int(os.Stdout.Fd()), cols, rows); ok {
		return w, h
	}
	return 10, 20
}

// openURL runs the platform's opener with the URL as its only argument.
func openURL(url string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	cmd := exec.Command(opener, url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func (m Model) Init() tea.Cmd {
	_, cmd := m.fetch()
	return cmd
}

func (m Model) listMaps() tea.Msg {
	raw, err := m.cfg.Source.ListMaps(context.Background())
	if err != nil {
		return listedMsg{err: err}
	}
	l, err := wayfinder.ParseList(raw)
	return listedMsg{l, err}
}

func (m Model) load() tea.Msg {
	raw, err := m.cfg.Source.LoadMap(context.Background(), m.number)
	if err != nil {
		return loadedMsg{number: m.number, err: err}
	}
	mp, err := wayfinder.Parse(raw)
	return loadedMsg{m.number, mp, err}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case listedMsg:
		// A listing that arrives after a map was picked is dropped.
		if m.view == pickerView {
			return m.listed(msg)
		}
	case loadedMsg:
		// So is a map that arrives after going back to the picker.
		if m.view != pickerView && msg.number == m.number {
			return m.loaded(msg)
		}
	case pollMsg:
		if int(msg) == m.pollGen && !m.loading {
			return m.fetch()
		}
	case openedMsg:
		if msg.err != nil {
			m.err = fmt.Errorf("open: %w", msg.err)
		}
	case tea.WindowSizeMsg:
		m.cols, m.rows = msg.Width, msg.Height
		m.cellW, m.cellH = m.cfg.CellSize(m.cols, m.rows)
		return m.fit().redraw()
	case drawMsg:
		if int(msg) != m.gen || m.cfg.List || m.shown == nil {
			break
		}
		if m.cols <= 0 || m.graphRows() <= 0 {
			// No room for the graph: don't leave the old image over the text.
			return m, tea.Raw(kitty.Clear)
		}
		return m, tea.Raw(m.image())
	case tea.KeyPressMsg:
		return m.key(msg.String())
	}
	return m, nil
}

// listed takes a (re)loaded listing, keeping the cursor on the same map if
// it is still open, and schedules the next poll.
func (m Model) listed(msg listedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m, poll := m.schedulePoll()
	if msg.err != nil {
		m.err = msg.err
		return m, poll
	}
	m.err, m.list = nil, &msg.l
	if m.pickIndex() < 0 {
		m.pick = 0
		if len(msg.l.Maps) > 0 {
			m.pick = msg.l.Maps[0].Number
		}
	}
	return m, poll
}

func (m Model) pickIndex() int {
	if m.list == nil {
		return -1
	}
	return slices.IndexFunc(m.list.Maps, func(s wayfinder.Summary) bool { return s.Number == m.pick })
}

// loaded takes a (re)loaded map. A failed refresh keeps the map on screen,
// and an unchanged one isn't drawn again. Either way the next poll is
// scheduled from now.
func (m Model) loaded(msg loadedMsg) (tea.Model, tea.Cmd) {
	m.loading = false
	m, poll := m.schedulePoll()
	if msg.err != nil {
		m.err = msg.err
		return m, poll
	}
	m.err = nil
	if reflect.DeepEqual(m.shown, msg.m) {
		return m, poll
	}
	m.shown = msg.m
	m = m.relayout()
	if m.shown.Ticket(m.sel) == nil {
		m.sel = firstSelection(m.shown)
	}
	m, redraw := m.redraw()
	return m, tea.Batch(poll, redraw)
}

// relayout lays the shown map out both ways in the look, then fits it.
// The list needs no layout.
func (m Model) relayout() Model {
	if m.cfg.List {
		return m
	}
	g := draw.Graph(m.shown, m.look)
	m.layouts = [2]layout.Result{layout.LR: layout.Layered(g, layout.LR), layout.TB: layout.Layered(g, layout.TB)}
	return m.fit()
}

// fit picks the direction that draws larger in the graph area, while it is
// automatic.
func (m Model) fit() Model {
	if m.autoDir && m.shown != nil && !m.cfg.List {
		w, h := float64(m.cols*m.cellW), float64(m.graphRows()*m.cellH)
		m.dir = layout.BestFit(m.layouts[layout.LR], m.layouts[layout.TB], w, h, m.maxScale())
	}
	return m
}

// res is the layout drawn.
func (m Model) res() layout.Result { return m.layouts[m.dir] }

// maxScale stops a small map from being drawn larger than the text around
// it.
func (m Model) maxScale() float64 { return float64(m.cellH) / 18 * 1.25 }

// schedulePoll fetches again after the interval, superseding any poll
// already scheduled.
func (m Model) schedulePoll() (Model, tea.Cmd) {
	m.pollGen++
	return m, m.cfg.After(m.cfg.Interval, pollMsg(m.pollGen))
}

// fetch lists the maps in the picker, else loads the map.
func (m Model) fetch() (Model, tea.Cmd) {
	m.loading = true
	if m.view == pickerView {
		return m, m.listMaps
	}
	return m, m.load
}

// openMap leaves the picker for map n. Polls scheduled for the picker are
// superseded.
func (m Model) openMap(n int) (tea.Model, tea.Cmd) {
	m.view, m.number, m.shown, m.sel, m.err = mapView, n, nil, 0, nil
	m.pollGen++
	return m.fetch()
}

// back leaves the map for the picker, with its cursor on the map, and
// clears the image.
func (m Model) back() (tea.Model, tea.Cmd) {
	m.view, m.pick, m.number, m.shown, m.sel, m.err = pickerView, m.number, 0, nil, 0, nil
	m.pollGen++
	m.gen++
	m, fetch := m.fetch()
	return m, tea.Batch(m.clear(), fetch)
}

// clear deletes the image, if there may be one; a terminal without Kitty
// graphics might print the escape.
func (m Model) clear() tea.Cmd {
	if m.cfg.List {
		return nil
	}
	return tea.Raw(kitty.Clear)
}

// firstSelection is the first frontier ticket, else the first ticket.
func firstSelection(mp *wayfinder.Map) int {
	for _, t := range mp.Tickets {
		if t.State == wayfinder.Frontier {
			return t.Number
		}
	}
	if len(mp.Tickets) > 0 {
		return mp.Tickets[0].Number
	}
	return 0
}

func (m Model) key(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "q", "ctrl+c":
		return m, tea.Sequence(m.clear(), tea.Quit)
	case "r":
		if m.loading {
			return m, nil
		}
		return m.fetch()
	}
	if m.view == pickerView {
		return m.pickerKey(k)
	}
	if k == "b" || k == "esc" {
		return m.back()
	}
	if m.shown == nil || m.sel == 0 {
		return m, nil
	}
	if k == "o" || k == "enter" {
		return m, m.open(m.shown.Repo, m.sel)
	}
	if m.cfg.List {
		return m.listKey(k)
	}
	sel := m.sel
	switch k {
	case "c":
		m.look = map[draw.Look]draw.Look{draw.Cards: draw.Compact, draw.Compact: draw.Cards}[m.look]
		return m.relayout().redraw()
	case "d":
		m.dir, m.autoDir = map[layout.Dir]layout.Dir{layout.LR: layout.TB, layout.TB: layout.LR}[m.dir], false
		return m.redraw()
	case "tab", "shift+tab":
		sel = m.cycle(k == "tab")
	case "left", "right", "up", "down":
		sel = m.nearest(k)
	}
	if sel == m.sel {
		return m, nil
	}
	m.sel = sel
	return m.redraw()
}

// listKey moves the selection through the list, stopping at its ends.
func (m Model) listKey(k string) (tea.Model, tea.Cmd) {
	ts := listOrder(m.shown)
	i := slices.IndexFunc(ts, func(t *wayfinder.Ticket) bool { return t.Number == m.sel })
	switch k {
	case "up", "k", "shift+tab":
		i = max(i-1, 0)
	case "down", "j", "tab":
		i = min(i+1, len(ts)-1)
	}
	m.sel = ts[i].Number
	return m, nil
}

// listStates are the list's groups, in order: what can be taken first, what
// is done last.
var listStates = []wayfinder.State{wayfinder.Frontier, wayfinder.Claimed, wayfinder.Blocked, wayfinder.Done}

// listOrder is the tickets as the list shows them: grouped by state, by
// number within each.
func listOrder(mp *wayfinder.Map) []*wayfinder.Ticket {
	var ts []*wayfinder.Ticket
	for _, s := range listStates {
		for _, t := range mp.Tickets {
			if t.State == s {
				ts = append(ts, t)
			}
		}
	}
	return ts
}

func (m Model) pickerKey(k string) (tea.Model, tea.Cmd) {
	i := m.pickIndex()
	if i < 0 {
		return m, nil
	}
	switch k {
	case "enter":
		return m.openMap(m.pick)
	case "up", "k":
		i = max(i-1, 0)
	case "down", "j":
		i = min(i+1, len(m.list.Maps)-1)
	}
	m.pick = m.list.Maps[i].Number
	return m, nil
}

func (m Model) open(repo string, n int) tea.Cmd {
	open := m.cfg.Open
	return func() tea.Msg {
		if !github.ValidRepo(repo) {
			return openedMsg{errors.New("bad repo " + repo)}
		}
		return openedMsg{open(fmt.Sprintf("https://github.com/%s/issues/%d", repo, n))}
	}
}

func centre(r layout.Rect) layout.Point { return layout.Point{X: r.X + r.W/2, Y: r.Y + r.H/2} }

// cycle steps through the tickets in reading order along the main axis.
func (m Model) cycle(forward bool) int {
	ns := make([]int, 0, len(m.shown.Tickets))
	for _, t := range m.shown.Tickets {
		ns = append(ns, t.Number)
	}
	slices.SortStableFunc(ns, func(a, b int) int {
		pa, pb := m.res().Boxes[a], m.res().Boxes[b]
		if pa.X != pb.X {
			return cmp.Compare(pa.X, pb.X)
		}
		return cmp.Compare(pa.Y, pb.Y)
	})
	i := slices.Index(ns, m.sel)
	if forward {
		return ns[(i+1)%len(ns)]
	}
	return ns[(i+len(ns)-1)%len(ns)]
}

// nearest finds the closest ticket in an arrow's direction, favouring ones
// in line with the selection. It stays put if there is none.
func (m Model) nearest(arrow string) int {
	d := map[string]layout.Point{"left": {X: -1}, "right": {X: 1}, "up": {Y: -1}, "down": {Y: 1}}[arrow]
	from := centre(m.res().Boxes[m.sel])
	best, bestScore := m.sel, math.Inf(1)
	for _, t := range m.shown.Tickets {
		c := centre(m.res().Boxes[t.Number])
		along := (c.X-from.X)*d.X + (c.Y-from.Y)*d.Y
		if t.Number == m.sel || along <= 1 {
			continue
		}
		perp := math.Abs((c.X-from.X)*d.Y - (c.Y-from.Y)*d.X)
		if score := along + 2*perp; score < bestScore {
			best, bestScore = t.Number, score
		}
	}
	return best
}

// redraw schedules an image for after the next frame; a later redraw
// supersedes it.
func (m Model) redraw() (Model, tea.Cmd) {
	m.gen++
	return m, m.cfg.After(drawDelay, drawMsg(m.gen))
}

func (m Model) graphRows() int { return m.rows - detailRows - len(m.warnings()) - statusRows }

// warnings returns a row for each of the map's warnings, folding any beyond
// maxWarningRows into the last.
func (m Model) warnings() []string {
	if m.shown == nil {
		return nil
	}
	ws := m.shown.Warnings
	if len(ws) > maxWarningRows {
		more := fmt.Sprintf("and %d more: %s", len(ws)-maxWarningRows+1, strings.Join(ws[maxWarningRows-1:], " · "))
		ws = append(slices.Clip(ws[:maxWarningRows-1]), more)
	}
	rows := make([]string, len(ws))
	for i, w := range ws {
		rows[i] = "\x1b[33m" + truncate("⚠ "+w, m.cols) + "\x1b[0m"
	}
	return rows
}

func (m Model) image() string {
	img := draw.Render(m.shown, m.res(), m.look, m.sel, m.cols*m.cellW, m.graphRows()*m.cellH, m.maxScale())
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return ""
	}
	return kitty.Place(buf.Bytes(), m.cols, m.graphRows())
}

func (m Model) View() tea.View {
	if m.view == pickerView {
		return m.picker()
	}
	var b strings.Builder
	if m.cfg.List && m.shown != nil {
		for _, line := range m.listRows() {
			b.WriteString(line + "\n")
		}
	} else {
		b.WriteString(strings.Repeat("\n", max(0, m.graphRows())))
	}
	for _, line := range append(m.details(), m.warnings()...) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", truncate(m.status(), m.cols))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// listRows fills the graph area with the tickets grouped by state, a
// heading over each group, scrolled so the selection is in view.
func (m Model) listRows() []string {
	count := map[wayfinder.State]int{}
	for _, t := range m.shown.Tickets {
		count[t.State]++
	}
	var rows []string
	at := 0
	ts := listOrder(m.shown)
	for i, t := range ts {
		if i == 0 || t.State != ts[i-1].State {
			heading := truncate(fmt.Sprintf("%s (%d)", t.State, count[t.State]), m.cols-3)
			rows = append(rows, swatch(t.State)+" \x1b[1m"+heading+"\x1b[0m")
		}
		row := fmt.Sprintf("#%d %s · %s", t.Number, t.Type, t.Title)
		if t.Number == m.sel {
			at = len(rows)
			rows = append(rows, "\x1b[1m"+truncate("› "+row, m.cols)+"\x1b[0m")
		} else {
			rows = append(rows, truncate("  "+row, m.cols))
		}
	}
	fit := max(0, m.graphRows())
	from := max(0, at-fit+1)
	rows = rows[from:min(len(rows), from+fit)]
	return append(rows, make([]string, fit-len(rows))...)
}

// picker is a header, a row per map with the cursor's in bold, a hint
// if there are more maps than listed, and the status line at the bottom.
func (m Model) picker() tea.View {
	var rows, foot []string
	header := "Open maps"
	if m.list != nil {
		header += " in " + m.list.Repo
		for _, s := range m.list.Maps {
			row := fmt.Sprintf("#%d · %d/%d done · %d ready · %d claimed · %d blocked · %s",
				s.Number, s.Done, s.Total, s.Frontier, s.Claimed, s.Blocked, s.Title)
			if s.Number == m.pick {
				rows = append(rows, "\x1b[1m"+truncate("› "+row, m.cols)+"\x1b[0m")
			} else {
				rows = append(rows, truncate("  "+row, m.cols))
			}
		}
		if m.list.More {
			foot = append(foot, "\x1b[33m"+truncate("⚠ more maps exist; open one directly with `wayfinder-map <number>`", m.cols)+"\x1b[0m")
		}
	}
	// Scroll so the cursor's row is in view.
	fit := max(0, m.rows-1-len(foot)-statusRows)
	from := max(0, m.pickIndex()-fit+1)
	rows = rows[from:min(len(rows), from+fit)]
	var b strings.Builder
	b.WriteString("\x1b[1m" + truncate(header, m.cols) + "\x1b[0m\n")
	for _, line := range rows {
		b.WriteString(line + "\n")
	}
	b.WriteString(strings.Repeat("\n", max(0, fit-len(rows))))
	for _, line := range foot {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", truncate(m.pickerStatus(), m.cols))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m Model) pickerStatus() string {
	switch {
	case m.err != nil:
		return "error: " + m.err.Error()
	case m.list == nil:
		return "finding open maps…"
	case len(m.list.Maps) == 0:
		return "no open wayfinder:map issues in this repo · r refresh · q quit"
	}
	n := fmt.Sprintf("%d open maps", len(m.list.Maps))
	if len(m.list.Maps) == 1 {
		n = "1 open map"
	}
	return n + " · ↑↓ select · enter open · r refresh · q quit"
}

func (m Model) status() string {
	switch {
	case m.err != nil:
		return "error: " + m.err.Error()
	case m.shown == nil:
		return fmt.Sprintf("loading map #%d…", m.number)
	}
	count := map[wayfinder.State]int{}
	for _, t := range m.shown.Tickets {
		count[t.State]++
	}
	// The counts come first so a narrow pane cuts the title, not them.
	return fmt.Sprintf("#%d · %d frontier · %d claimed · %d blocked · %d done · %s",
		m.shown.Number, count[wayfinder.Frontier], count[wayfinder.Claimed],
		count[wayfinder.Blocked], count[wayfinder.Done], m.shown.Title)
}

// details returns the detailRows lines below the graph.
func (m Model) details() []string {
	lines := make([]string, detailRows)
	if m.shown == nil {
		return lines
	}
	lines[0] = m.legend()
	if t := m.shown.Ticket(m.sel); t != nil {
		lines[1] = "\x1b[1m" + truncate(fmt.Sprintf("#%d %s", t.Number, t.Title), m.cols) + "\x1b[0m"
		meta := fmt.Sprintf("%s · %s · unassigned", t.Type, t.State)
		switch who := strings.Join(t.Assignees, ", "); {
		case t.State == wayfinder.Claimed:
			meta = fmt.Sprintf("%s · claimed by %s", t.Type, who)
		case who != "":
			meta = fmt.Sprintf("%s · %s · assigned to %s", t.Type, t.State, who)
		}
		lines[2] = truncate(meta, m.cols)
		var by []string
		for _, b := range t.Blockers {
			state := "open"
			if b.Closed {
				state = "closed"
			}
			by = append(by, fmt.Sprintf("%s (%s)", b.Ref(m.shown.Repo), state))
		}
		lines[3] = truncate("blocked by: "+orNone(by), m.cols)
		var blocks []string
		for _, n := range m.shown.Blocks(t.Number) {
			blocks = append(blocks, fmt.Sprintf("#%d", n))
		}
		lines[4] = truncate("blocks: "+orNone(blocks), m.cols)
	}
	return lines
}

// legend shows what the colours and lines mean, then the keys, as far as
// the pane is wide.
func (m Model) legend() string {
	var b strings.Builder
	width := 0
	add := func(styled string, w int) bool {
		if width+w > m.cols {
			return false
		}
		b.WriteString(styled)
		width += w
		return true
	}
	for _, s := range []wayfinder.State{wayfinder.Done, wayfinder.Claimed, wayfinder.Frontier, wayfinder.Blocked} {
		name := s.String()
		if !add(swatch(s)+" "+name+"  ", 2+1+len([]rune(name))+2) {
			return b.String()
		}
	}
	keys := []string{"┄ closed blocker  ", "←↑↓→ tab select · c look · d direction · o open · b back · r refresh · q quit"}
	if m.cfg.List {
		keys = []string{"↑↓ select · o open · b back · r refresh · q quit"}
	}
	for _, s := range keys {
		if !add(s, len([]rune(s))) {
			break
		}
	}
	return b.String()
}

// swatch is two cells in the state's colour.
func swatch(s wayfinder.State) string {
	c := draw.FillOf(s)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm  \x1b[0m", c.R, c.G, c.B)
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "—"
	}
	return strings.Join(xs, " · ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n < 2 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
