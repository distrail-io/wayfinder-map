// Package tui is the Bubble Tea app. A picker lists the repo's open maps; a
// map is shown as a Kitty-graphics image, with the selected ticket's details
// and a status line below. Both refresh on an interval, and the map is
// redrawn only when it changes.
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

	// Look and List are how the map is shown. Only cards are drawn so far,
	// and List is not yet honoured.
	Look draw.Look
	List bool
	// Dir is the graph's direction, unless AutoDir picks the one that fits
	// best, which is LR for now.
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

	// The picker, shown while picking.
	picking bool
	list    *wayfinder.List // nil until the first listing arrives
	pick    int             // the number of the map under the cursor

	number     int // the map shown, or being loaded
	shown      *wayfinder.Map
	res        layout.Result
	sel        int // the selected ticket's number
	err        error
	loading    bool
	cols, rows int
	cellW      int
	cellH      int
	gen        int // the latest scheduled draw
	pollGen    int // the latest scheduled poll
}

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
	return Model{cfg: cfg, number: cfg.Number, picking: cfg.Number == 0, loading: true}
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
		if m.picking {
			return m.listed(msg)
		}
	case loadedMsg:
		// So is a map that arrives after going back to the picker.
		if !m.picking && msg.number == m.number {
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
		return m.redraw()
	case drawMsg:
		if int(msg) == m.gen && m.shown != nil && m.cols > 0 && m.graphRows() > 0 {
			return m, tea.Raw(m.image())
		}
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
	m.res = layout.Layered(draw.Graph(m.shown), m.dir())
	if m.shown.Ticket(m.sel) == nil {
		m.sel = firstSelection(m.shown)
	}
	m, redraw := m.redraw()
	return m, tea.Batch(poll, redraw)
}

func (m Model) dir() layout.Dir {
	if m.cfg.AutoDir {
		return layout.LR
	}
	return m.cfg.Dir
}

// schedulePoll fetches again after the interval, superseding any poll
// already scheduled.
func (m Model) schedulePoll() (Model, tea.Cmd) {
	m.pollGen++
	return m, m.cfg.After(m.cfg.Interval, pollMsg(m.pollGen))
}

// fetch lists the maps while picking, else loads the map.
func (m Model) fetch() (Model, tea.Cmd) {
	m.loading = true
	if m.picking {
		return m, m.listMaps
	}
	return m, m.load
}

// openMap leaves the picker for map n. Polls scheduled for the picker are
// superseded.
func (m Model) openMap(n int) (tea.Model, tea.Cmd) {
	m.picking, m.number, m.shown, m.sel, m.err = false, n, nil, 0, nil
	m.pollGen++
	return m.fetch()
}

// back leaves the map for the picker, with its cursor on the map, and
// clears the image.
func (m Model) back() (tea.Model, tea.Cmd) {
	m.picking, m.pick, m.number, m.shown, m.sel, m.err = true, m.number, 0, nil, 0, nil
	m.pollGen++
	m.gen++
	m, fetch := m.fetch()
	return m, tea.Batch(tea.Raw(kitty.Clear), fetch)
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
		return m, tea.Sequence(tea.Raw(kitty.Clear), tea.Quit)
	case "r":
		if m.loading {
			return m, nil
		}
		return m.fetch()
	}
	if m.picking {
		return m.pickerKey(k)
	}
	if k == "b" || k == "esc" {
		return m.back()
	}
	if m.shown == nil || m.sel == 0 {
		return m, nil
	}
	sel := m.sel
	switch k {
	case "o", "enter":
		return m, m.open(m.shown.Repo, m.sel)
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
		pa, pb := m.res.Boxes[a], m.res.Boxes[b]
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
	from := centre(m.res.Boxes[m.sel])
	best, bestScore := m.sel, math.Inf(1)
	for _, t := range m.shown.Tickets {
		c := centre(m.res.Boxes[t.Number])
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
	maxScale := float64(m.cellH) / 18 * 1.25
	img := draw.Render(m.shown, m.res, m.sel, m.cols*m.cellW, m.graphRows()*m.cellH, maxScale)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return ""
	}
	return kitty.Place(buf.Bytes(), m.cols, m.graphRows())
}

func (m Model) View() tea.View {
	if m.picking {
		return m.pickerView()
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("\n", max(0, m.graphRows())))
	for _, line := range append(m.details(), m.warnings()...) {
		b.WriteString(line + "\n")
	}
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", truncate(m.status(), m.cols))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// pickerView is a header, a row per map with the cursor's in bold, a hint
// if there are more maps than listed, and the status line at the bottom.
func (m Model) pickerView() tea.View {
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
		c := draw.FillOf(s)
		name := s.String()
		if !add(fmt.Sprintf("\x1b[48;2;%d;%d;%dm  \x1b[0m %s  ", c.R, c.G, c.B, name), 2+1+len([]rune(name))+2) {
			return b.String()
		}
	}
	for _, s := range []string{"┄ closed blocker  ", "←↑↓→ tab select · o open · b back · r refresh · q quit"} {
		if !add(s, len([]rune(s))) {
			break
		}
	}
	return b.String()
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
