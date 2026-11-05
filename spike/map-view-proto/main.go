// PROTOTYPE, throwaway: wayfinder-map#3. Three looks of the map graph, switchable live:
//
//	[ / ]  look: cards | compact | lanes
//	l      layout engine: dot (Graphviz positions+splines) | layered (hand-rolled)
//	d      direction: LR | TB (cards and compact)
//	m      next fixture map
//	←↑↓→ / tab   select ticket     o  open in browser     q  quit
//
// Run: cd spike/map-view-proto && go run .
// Dump PNGs instead of running the TUI: go run . -png out.png -look cards -layout dot -map 175
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sys/unix"
)

var looks = []string{"cards", "compact", "lanes"}

type model struct {
	maps        []*Map
	mi          int
	look        int
	useDot      bool
	tb          bool
	sel         int
	cols, rows  int
	cellW       int
	cellH       int
	lay         *Layout
	scale       float64
	note        string
	gen         int
	drawTook    time.Duration
}

type drawMsg int

const detailRows = 8

func (m *model) cur() *Map { return m.maps[m.mi] }

func (m *model) graphRows() int { return max(4, m.rows-detailRows) }

func (m *model) relayout() {
	mp := m.cur()
	look := looks[m.look]
	m.note = ""
	switch {
	case look == "lanes":
		m.lay = lanesLayout(mp)
		m.note = "lanes always use the layered x-axis"
	case m.useDot:
		lay, err := dotLayout(mp, look, m.tb)
		if err != nil {
			m.note = err.Error() + ", fell back to layered"
			lay = layeredLayout(mp, look, m.tb)
		}
		m.lay = lay
	default:
		m.lay = layeredLayout(mp, look, m.tb)
	}
	if _, ok := mp.Tickets[m.sel]; !ok {
		m.sel = mp.Order[0]
		for _, n := range mp.Order {
			if mp.Tickets[n].State == Frontier {
				m.sel = n
				break
			}
		}
	}
}

func (m *model) maxScale() float64 { return float64(m.cellH) / 18 * 1.25 }

func (m *model) kitty() string {
	W, H := m.cols*m.cellW, m.graphRows()*m.cellH
	t0 := time.Now()
	img, s := render(m.cur(), m.lay, looks[m.look], m.sel, W, H, m.maxScale())
	m.scale = s / (float64(m.cellH) / 18)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	enc.Encode(&buf, img)
	m.drawTook = time.Since(t0)
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	var b strings.Builder
	b.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\") // delete previous placements
	b.WriteString("\x1b7\x1b[1;1H")         // save cursor, go home
	for i := 0; i < len(data); i += 4096 {
		end, more := min(i+4096, len(data)), 1
		if end == len(data) {
			more = 0
		}
		if i == 0 {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,q=2,C=1,c=%d,r=%d,m=%d;%s\x1b\\", m.cols, m.graphRows(), more, data[i:end])
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, data[i:end])
		}
	}
	b.WriteString("\x1b8")
	return b.String()
}

// redraw debounces image output until after Bubble Tea has painted its frame.
func (m *model) redraw() tea.Cmd {
	m.gen++
	g := m.gen
	return tea.Tick(60*time.Millisecond, func(time.Time) tea.Msg { return drawMsg(g) })
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.cols, m.rows = msg.Width, msg.Height
		m.cellW, m.cellH = cellSize(m.cols, m.rows)
		return m, m.redraw()
	case drawMsg:
		if int(msg) != m.gen || m.lay == nil {
			return m, nil
		}
		return m, tea.Raw(m.kitty())
	case tea.KeyPressMsg:
		switch k := msg.String(); k {
		case "q", "ctrl+c":
			return m, tea.Sequence(tea.Raw("\x1b_Ga=d,d=A,q=2\x1b\\"), tea.Quit)
		case "]":
			m.look = (m.look + 1) % len(looks)
		case "[":
			m.look = (m.look + len(looks) - 1) % len(looks)
		case "l":
			m.useDot = !m.useDot
		case "d":
			m.tb = !m.tb
		case "m":
			m.mi = (m.mi + 1) % len(m.maps)
			m.sel = 0
		case "o", "enter":
			exec.Command("open", fmt.Sprintf("https://github.com/%s/issues/%d", m.cur().Repo, m.sel)).Start()
			return m, nil
		case "tab", "shift+tab":
			m.sel = m.cycle(k == "tab")
			return m, m.redraw()
		case "left", "right", "up", "down", "h", "j", "k":
			m.sel = m.nearest(k)
			return m, m.redraw()
		default:
			return m, nil
		}
		m.relayout()
		return m, m.redraw()
	}
	return m, nil
}

func (m *model) cycle(fwd bool) int {
	ns := append([]int(nil), m.cur().Order...)
	sort.Slice(ns, func(i, j int) bool {
		a, b := m.lay.Boxes[ns[i]], m.lay.Boxes[ns[j]]
		if m.tb || looks[m.look] == "lanes" {
			return a.Y < b.Y || a.Y == b.Y && a.X < b.X
		}
		return a.X < b.X || a.X == b.X && a.Y < b.Y
	})
	for i, n := range ns {
		if n == m.sel {
			if fwd {
				return ns[(i+1)%len(ns)]
			}
			return ns[(i+len(ns)-1)%len(ns)]
		}
	}
	return ns[0]
}

func (m *model) nearest(dir string) int {
	dx, dy := map[string][2]float64{"left": {-1, 0}, "h": {-1, 0}, "right": {1, 0}, "up": {0, -1}, "k": {0, -1}, "down": {0, 1}, "j": {0, 1}}[dir][0],
		map[string][2]float64{"left": {-1, 0}, "h": {-1, 0}, "right": {1, 0}, "up": {0, -1}, "k": {0, -1}, "down": {0, 1}, "j": {0, 1}}[dir][1]
	from := m.lay.Boxes[m.sel].C()
	best, bestScore := m.sel, math.Inf(1)
	for n, b := range m.lay.Boxes {
		c := b.C()
		along := (c.X-from.X)*dx + (c.Y-from.Y)*dy
		if n == m.sel || along <= 1 {
			continue
		}
		perp := math.Abs((c.X-from.X)*dy - (c.Y-from.Y)*dx)
		if score := along + 2*perp; score < bestScore {
			best, bestScore = n, score
		}
	}
	return best
}

func sw(st State) string {
	c := fillOf[st]
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm  \x1b[0m %s", c.R, c.G, c.B, stateNames[st])
}

func (m model) View() tea.View {
	if m.lay == nil {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	mp := m.cur()
	t := mp.Tickets[m.sel]
	var b strings.Builder
	b.WriteString(strings.Repeat("\n", m.graphRows()))
	fmt.Fprintf(&b, "%s  %s  %s  %s   ┄ blocker closed   ⟐ selected\n", sw(Done), sw(Claimed), sw(Frontier), sw(Blocked))
	fmt.Fprintf(&b, "\x1b[1m#%d %s\x1b[0m\n", t.Num, trunc(t.Title, m.cols-8))
	who := "unassigned"
	if t.Assignee != "" {
		who = "claimed by " + t.Assignee
	}
	fmt.Fprintf(&b, "%s · %s · %s\n", t.Type, stateNames[t.State], who)
	var by []string
	for _, r := range t.Blockers {
		mark := "open"
		if r.Closed {
			mark = "closed"
		}
		if bt, ok := mp.inMap(r); ok {
			by = append(by, fmt.Sprintf("%s (%s)", bt.Title, mark))
		} else {
			by = append(by, fmt.Sprintf("%s#%d (%s, other repo)", r.Repo, r.Num, mark))
		}
	}
	fmt.Fprintf(&b, "blocked by: %s\n", trunc(orNone(by), m.cols-13))
	var bl []string
	for _, n := range t.Blocks {
		bl = append(bl, mp.Tickets[n].Title)
	}
	fmt.Fprintf(&b, "blocks:     %s\n", trunc(orNone(bl), m.cols-13))
	eng := "layered"
	if m.useDot && looks[m.look] != "lanes" {
		eng = "dot"
	}
	dir := "LR"
	if m.tb {
		dir = "TB"
	}
	status := fmt.Sprintf(" PROTOTYPE  look %s [ ]  layout %s (l)  dir %s (d)  map #%d (m)  scale %.0f%%  draw %dms  cell %dx%d  · ←↑↓→/tab o q",
		looks[m.look], eng, dir, mp.Num, m.scale*100, m.drawTook.Milliseconds(), m.cellW, m.cellH)
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", trunc(status, m.cols))
	if m.note != "" {
		fmt.Fprintf(&b, "\n\x1b[2m%s\x1b[0m", trunc(m.note, m.cols))
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func orNone(xs []string) string {
	if len(xs) == 0 {
		return "—"
	}
	return strings.Join(xs, " · ")
}

func trunc(s string, n int) string {
	r := []rune(s)
	if n < 2 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// cellSize reads the pixel size from TIOCGWINSZ; WFP_CELL=WxH overrides.
func cellSize(cols, rows int) (int, int) {
	var w, h int
	if _, err := fmt.Sscanf(os.Getenv("WFP_CELL"), "%dx%d", &w, &h); err == nil {
		return w, h
	}
	if ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil && ws.Xpixel > 0 && cols > 0 {
		return int(ws.Xpixel) / cols, int(ws.Ypixel) / rows
	}
	return 10, 20
}

func main() {
	pngOut := flag.String("png", "", "write one PNG and exit")
	look := flag.String("look", "cards", "cards|compact|lanes")
	layout := flag.String("layout", "dot", "dot|layered")
	tb := flag.Bool("tb", false, "top-to-bottom")
	mapNum := flag.Int("map", 214, "fixture map number")
	cols := flag.Int("cols", 160, "png: columns")
	rows := flag.Int("rows", 40, "png: rows")
	sel := flag.Int("sel", 0, "png: selected ticket")
	flag.Parse()

	files, _ := filepath.Glob("../../fixtures/map-*.json")
	if len(files) == 0 {
		files, _ = filepath.Glob("fixtures/map-*.json")
	}
	m := model{useDot: *layout == "dot", tb: *tb, sel: *sel}
	for _, f := range files {
		mp, err := loadMap(f, "banked/ask-barry", true)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if mp.Num == *mapNum {
			m.mi = len(m.maps)
		}
		m.maps = append(m.maps, mp)
	}
	for i, l := range looks {
		if l == *look {
			m.look = i
		}
	}
	m.relayout()

	if *pngOut != "" {
		m.cols, m.rows, m.cellW, m.cellH = *cols, *rows, 18, 38
		img, s := render(m.cur(), m.lay, looks[m.look], m.sel, m.cols*m.cellW, m.graphRows()*m.cellH, m.maxScale())
		f, _ := os.Create(*pngOut)
		png.Encode(f, img)
		f.Close()
		fmt.Printf("%s: %dx%d scale %.0f%% %s\n", *pngOut, m.cols*m.cellW, m.graphRows()*m.cellH, s/(float64(m.cellH)/18)*100, m.note)
		return
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
