// Package tui is the Bubble Tea app: it loads one map and shows it as a
// Kitty-graphics image above a status line.
package tui

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
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
	Number int // 0 opens the repo's most recently updated open map

	// CellSize reports a cell's size in pixels. Nil reads it from stdout.
	CellSize func(cols, rows int) (w, h int)
}

type Model struct {
	cfg        Config
	number     int
	shown      *wayfinder.Map
	res        layout.Result
	err        error
	cols, rows int
	cellW      int
	cellH      int
	gen        int
}

type (
	listedMsg struct {
		maps []wayfinder.Summary
		err  error
	}
	loadedMsg struct {
		m   *wayfinder.Map
		err error
	}
	drawMsg int
)

// drawDelay lets Bubble Tea paint its frame before the image goes over it.
const drawDelay = 60 * time.Millisecond

const statusRows = 1

func New(cfg Config) Model {
	if cfg.CellSize == nil {
		cfg.CellSize = stdoutCellSize
	}
	return Model{cfg: cfg, number: cfg.Number}
}

func stdoutCellSize(cols, rows int) (int, int) {
	if w, h, ok := kitty.CellSize(int(os.Stdout.Fd()), cols, rows); ok {
		return w, h
	}
	return 10, 20
}

func (m Model) Init() tea.Cmd {
	if m.number == 0 {
		return m.list
	}
	return m.load
}

func (m Model) list() tea.Msg {
	raw, err := m.cfg.Source.ListMaps(context.Background())
	if err != nil {
		return listedMsg{err: err}
	}
	maps, err := wayfinder.ParseList(raw)
	return listedMsg{maps, err}
}

func (m Model) load() tea.Msg {
	raw, err := m.cfg.Source.LoadMap(context.Background(), m.number)
	if err != nil {
		return loadedMsg{err: err}
	}
	mp, err := wayfinder.Parse(raw)
	return loadedMsg{mp, err}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case listedMsg:
		switch {
		case msg.err != nil:
			m.err = msg.err
		case len(msg.maps) == 0:
			m.err = fmt.Errorf("no open wayfinder:map issues in this repo")
		default:
			m.number = msg.maps[0].Number
			return m, m.load
		}
	case loadedMsg:
		m.shown, m.err = msg.m, msg.err
		if m.shown != nil {
			m.res = layout.Layered(draw.Graph(m.shown), layout.LR)
		}
		return m.redraw()
	case tea.WindowSizeMsg:
		m.cols, m.rows = msg.Width, msg.Height
		m.cellW, m.cellH = m.cfg.CellSize(m.cols, m.rows)
		return m.redraw()
	case drawMsg:
		if int(msg) == m.gen && m.shown != nil && m.cols > 0 && m.graphRows() > 0 {
			return m, tea.Raw(m.image())
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Sequence(tea.Raw(kitty.Clear), tea.Quit)
		}
	}
	return m, nil
}

// redraw schedules an image for after the next frame; a later redraw
// supersedes it.
func (m Model) redraw() (Model, tea.Cmd) {
	m.gen++
	g := m.gen
	return m, tea.Tick(drawDelay, func(time.Time) tea.Msg { return drawMsg(g) })
}

func (m Model) graphRows() int { return m.rows - statusRows }

func (m Model) image() string {
	maxScale := float64(m.cellH) / 18 * 1.25
	img := draw.Render(m.shown, m.res, m.cols*m.cellW, m.graphRows()*m.cellH, maxScale)
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		return ""
	}
	return kitty.Place(buf.Bytes(), m.cols, m.graphRows())
}

func (m Model) View() tea.View {
	var status string
	switch {
	case m.err != nil:
		status = "error: " + m.err.Error()
	case m.shown == nil && m.number == 0:
		status = "finding the newest open map…"
	case m.shown == nil:
		status = fmt.Sprintf("loading map #%d…", m.number)
	default:
		count := map[wayfinder.State]int{}
		for _, t := range m.shown.Tickets {
			count[t.State]++
		}
		// The counts come first so a narrow pane cuts the title, not them.
		status = fmt.Sprintf("#%d · %d frontier · %d claimed · %d blocked · %d done · q quit · %s",
			m.shown.Number, count[wayfinder.Frontier], count[wayfinder.Claimed],
			count[wayfinder.Blocked], count[wayfinder.Done], m.shown.Title)
	}
	var b strings.Builder
	b.WriteString(strings.Repeat("\n", max(0, m.graphRows())))
	fmt.Fprintf(&b, "\x1b[7m%s\x1b[0m", truncate(status, m.cols))
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func truncate(s string, n int) string {
	r := []rune(s)
	if n < 2 || len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
