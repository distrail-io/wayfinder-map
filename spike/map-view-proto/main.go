// PROTOTYPE, throwaway: wayfinder-map#3 and #6. Looks of the map graph and layout engines, switchable live:
//
//	[ / ]  look: cards | compact | lanes
//	l      layout engine: dot (Graphviz binary) | sugiyama (#6 option B, ours) | layered (#3 naive)
//	v      versus: dot on the left, sugiyama on the right
//	d      direction: LR | TB (cards and compact)
//	m      next fixture map
//	←↑↓→ / tab   select ticket     o  open in browser     q  quit
//
// Run: cd spike/map-view-proto && go run .
// Dump PNGs instead of running the TUI: go run . -png out.png -look cards -layout dot -map 175
// Side-by-side PNGs for every fixture, look and direction: go run . -compare out/
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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

var engines = []string{"dot", "sugiyama", "layered"}

type model struct {
	maps       []*Map
	mi         int
	look       int
	engine     int
	versus     bool
	lay2       *Layout // sugiyama, for versus
	layTook    time.Duration
	tb         bool
	sel        int
	cols, rows int
	cellW      int
	cellH      int
	lay        *Layout
	scale      float64
	note       string
	gen        int
	drawTook   time.Duration
}

type drawMsg int

const detailRows = 8

func (m *model) cur() *Map { return m.maps[m.mi] }

func (m *model) graphRows() int { return max(4, m.rows-detailRows) }

func (m *model) relayout() {
	mp := m.cur()
	look := looks[m.look]
	m.note = ""
	t0 := time.Now()
	if m.versus && look != "lanes" {
		m.lay, m.note = layoutWith(mp, look, "dot", m.tb)
		t0 = time.Now()
		m.lay2, _ = layoutWith(mp, look, "sugiyama", m.tb)
	} else {
		m.lay, m.note = layoutWith(mp, look, engines[m.engine], m.tb)
		m.lay2 = nil
	}
	m.layTook = time.Since(t0)
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

func layoutWith(mp *Map, look, engine string, tb bool) (*Layout, string) {
	switch {
	case look == "lanes":
		return lanesLayout(mp), "lanes always use the layered x-axis"
	case engine == "dot":
		lay, err := dotLayout(mp, look, tb)
		if err != nil {
			return sugiyamaLayout(mp, look, tb), err.Error() + ", fell back to sugiyama"
		}
		return lay, ""
	case engine == "sugiyama":
		return sugiyamaLayout(mp, look, tb), ""
	}
	return layeredLayout(mp, look, tb), ""
}

// renderView draws one layout, or dot and sugiyama side by side in versus mode.
func renderView(mp *Map, lay, lay2 *Layout, look string, sel, W, H int, maxScale float64) (*image.RGBA, float64) {
	if lay2 == nil {
		return render(mp, lay, look, sel, W, H, maxScale)
	}
	half := (W - 8) / 2
	a, s := render(mp, lay, look, sel, half, H, maxScale)
	b, _ := render(mp, lay2, look, sel, half, H, maxScale)
	img := image.NewRGBA(image.Rect(0, 0, W, H))
	draw.Draw(img, a.Bounds(), a, image.Point{}, draw.Src)
	draw.Draw(img, b.Bounds().Add(image.Pt(W-half, 0)), b, image.Point{}, draw.Src)
	fill(img, roundRect(float64(half)+3, 0, 2, float64(H), 0), fadedCol)
	fs := float64(H) / 40
	text(img, 8, fs+6, "dot (Graphviz)", fs, true, fadedCol)
	text(img, float64(W-half)+8, fs+6, "sugiyama (ours)", fs, true, fadedCol)
	return img, s
}

func (m *model) maxScale() float64 { return float64(m.cellH) / 18 * 1.25 }

func (m *model) kitty() string {
	W, H := m.cols*m.cellW, m.graphRows()*m.cellH
	t0 := time.Now()
	img, s := renderView(m.cur(), m.lay, m.lay2, looks[m.look], m.sel, W, H, m.maxScale())
	m.scale = s / (float64(m.cellH) / 18)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	enc.Encode(&buf, img)
	m.drawTook = time.Since(t0)
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	var b strings.Builder
	b.WriteString("\x1b_Ga=d,d=A,q=2\x1b\\") // delete previous placements
	b.WriteString("\x1b7\x1b[1;1H")          // save cursor, go home
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
			m.engine = (m.engine + 1) % len(engines)
			m.versus = false
		case "v":
			m.versus = !m.versus
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
	eng := engines[m.engine]
	switch {
	case looks[m.look] == "lanes":
		eng = "layered"
	case m.versus:
		eng = "dot|sugiyama"
	}
	dir := "LR"
	if m.tb {
		dir = "TB"
	}
	status := fmt.Sprintf(" PROTOTYPE  look %s [ ]  layout %s (l, v)  %dµs  dir %s (d)  map #%d (m)  scale %.0f%%  draw %dms  cell %dx%d  · ←↑↓→/tab o q",
		looks[m.look], eng, m.layTook.Microseconds(), dir, mp.Num, m.scale*100, m.drawTook.Milliseconds(), m.cellW, m.cellH)
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
	layout := flag.String("layout", "dot", "dot|sugiyama|layered")
	bench := flag.Int("bench", 0, "time dot and sugiyama on a random map of this many tickets and exit")
	compare := flag.String("compare", "", "write dot-vs-sugiyama PNGs for every fixture into this dir and exit")
	tb := flag.Bool("tb", false, "top-to-bottom")
	mapNum := flag.Int("map", 214, "fixture map number")
	cols := flag.Int("cols", 160, "png: columns")
	rows := flag.Int("rows", 40, "png: rows")
	sel := flag.Int("sel", 0, "png: selected ticket")
	flag.Parse()

	if *bench > 0 {
		benchLayouts(*bench)
		return
	}
	files, _ := filepath.Glob("../../fixtures/map-*.json")
	if len(files) == 0 {
		files, _ = filepath.Glob("fixtures/map-*.json")
	}
	m := model{tb: *tb, sel: *sel}
	for i, e := range engines {
		if e == *layout {
			m.engine = i
		}
	}
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
	syn := synthMap(40, 7) // bigger than any fixture; "m" cycles to it
	syn.Num = 40
	m.maps = append(m.maps, syn)
	if *mapNum == 40 {
		m.mi = len(m.maps) - 1
	}
	for i, l := range looks {
		if l == *look {
			m.look = i
		}
	}
	m.relayout()

	if *compare != "" {
		os.MkdirAll(*compare, 0o755)
		m.cols, m.rows, m.cellW, m.cellH = *cols, *rows, 18, 38
		m.versus = true
		for mi := range m.maps {
			m.mi = mi
			for _, lk := range []int{0, 1} {
				for _, tb := range []bool{false, true} {
					m.look, m.tb, m.sel = lk, tb, 0
					m.relayout()
					img, _ := renderView(m.cur(), m.lay, m.lay2, looks[lk], m.sel, m.cols*m.cellW, m.graphRows()*m.cellH, m.maxScale())
					dir := "lr"
					if tb {
						dir = "tb"
					}
					out := filepath.Join(*compare, fmt.Sprintf("map-%d-%s-%s.png", m.cur().Num, looks[lk], dir))
					bg := image.NewRGBA(img.Bounds()) // dark terminal-ish background so it reads in an image viewer
					draw.Draw(bg, bg.Bounds(), image.NewUniform(color.RGBA{30, 30, 36, 255}), image.Point{}, draw.Src)
					draw.Draw(bg, bg.Bounds(), img, image.Point{}, draw.Over)
					f, _ := os.Create(out)
					png.Encode(f, bg)
					f.Close()
					fmt.Println(out, m.note)
				}
			}
		}
		return
	}
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
