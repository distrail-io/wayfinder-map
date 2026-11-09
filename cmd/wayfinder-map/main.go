// Command wayfinder-map draws a Wayfinder map from GitHub Issues as a graph
// in the terminal. It reads through the user's gh login and never writes.
//
//	wayfinder-map [MAP] [-R owner/repo] [--look cards|compact] [--dir auto|lr|tb] [--interval 30s] [--list] [--no-mouse] [--no-images]
//
// MAP is 123, #123, owner/repo#123 or an issue URL. Without it, a picker
// lists the repo's open maps.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/github"
	"github.com/distrail-io/wayfinder-map/internal/kitty"
	"github.com/distrail-io/wayfinder-map/internal/layout"
	"github.com/distrail-io/wayfinder-map/internal/tui"
)

// minInterval keeps polling well inside GitHub's rate limit: each load costs
// about 3 of 5,000 points an hour.
const minInterval = 10 * time.Second

// probeTimeout is how long the terminal has to answer the Kitty graphics
// probe. Terminals answer in milliseconds; this only bounds one that doesn't.
const probeTimeout = time.Second

// version is set at release build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Getenv, os.Stdout); err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, "wayfinder-map:", err)
		}
		os.Exit(2)
	}
}

func run(args []string, getenv func(string) string, stdout io.Writer) error {
	o, err := parseOptions(args, getenv)
	if err != nil {
		return err
	}
	if o.version {
		fmt.Fprintln(stdout, "wayfinder-map", version)
		return nil
	}
	src := github.GH{Target: github.Resolve(o.ref, o.repo, getenv)}
	_, err = tea.NewProgram(tui.New(o.config(src, getenv, func() bool { return kitty.Probe(probeTimeout) }))).Run()
	return err
}

// config is the app's config for the options, asking images whether the
// terminal shows Kitty graphics if that decides it.
func (o options) config(src github.Source, getenv func(string) string, images func() bool) tui.Config {
	list := o.showList(images)
	return tui.Config{
		Source:   src,
		Number:   o.ref.Number,
		Interval: o.interval,
		Look:     o.look,
		Dir:      o.dir,
		AutoDir:  o.autoDir,
		List:     list,
		NoMouse:  o.noMouse,
		// WezTerm answers the probe but can't show Unicode placeholders.
		Images: !list && !o.noImages && getenv("TERM_PROGRAM") != "WezTerm",
	}
}

type options struct {
	ref      github.Ref
	repo     string
	look     draw.Look
	dir      layout.Dir
	autoDir  bool // pick the direction that fits the pane
	interval time.Duration
	list     setBool
	noMouse  bool
	noImages bool
	version  bool
}

// showList is whether to list the tickets rather than draw the graph: as
// set, else if the terminal doesn't show Kitty graphics.
func (o options) showList(images func() bool) bool {
	if o.list.set {
		return o.list.v
	}
	return !images()
}

// envVars set the flags they name. A flag beats its variable, which beats
// the default.
var envVars = []struct{ name, flag string }{
	{"WAYFINDER_MAP_LOOK", "look"},
	{"WAYFINDER_MAP_DIR", "dir"},
	{"WAYFINDER_MAP_INTERVAL", "interval"},
	{"WAYFINDER_MAP_LIST", "list"},
	{"WAYFINDER_MAP_NO_MOUSE", "no-mouse"},
	{"WAYFINDER_MAP_NO_IMAGES", "no-images"},
}

func parseOptions(args []string, getenv func(string) string) (options, error) {
	o := options{look: draw.Cards, autoDir: true, interval: tui.DefaultInterval}
	fs := flag.NewFlagSet("wayfinder-map", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), usage)
		fs.PrintDefaults()
	}
	fs.StringVar(&o.repo, "R", "", "the repo to read, as owner/repo")
	fs.StringVar(&o.repo, "repo", "", "the same as -R")
	fs.Var((*lookValue)(&o.look), "look", "how tickets are drawn: cards or compact (default cards)")
	fs.Var(dirValue{&o.dir, &o.autoDir}, "dir", "the graph's direction: auto, lr or tb (default auto)")
	fs.Var((*intervalValue)(&o.interval), "interval", "how often to reload the map, at least "+minInterval.String())
	fs.Var(&o.list, "list", "list the tickets as text instead of drawing the graph (default: if the terminal lacks Kitty graphics)")
	fs.BoolVar(&o.noMouse, "no-mouse", false, "leave the mouse to the terminal, for its own text selection")
	fs.BoolVar(&o.noImages, "no-images", false, "list the detail view's relationships as text instead of drawing a mini-map")
	fs.BoolVar(&o.version, "version", false, "print the version and exit")
	for _, e := range envVars {
		if v := getenv(e.name); v != "" {
			if err := fs.Lookup(e.flag).Value.Set(v); err != nil {
				return options{}, fmt.Errorf("bad %s=%q: %v", e.name, v, err)
			}
		}
	}
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return options{}, err
	}
	if o.repo != "" && !github.ValidRepo(o.repo) {
		return options{}, fmt.Errorf("bad -R %q: want owner/repo", o.repo)
	}
	switch len(pos) {
	case 0:
	case 1:
		if o.ref, err = github.ParseRef(pos[0]); err != nil {
			return options{}, err
		}
	default:
		return options{}, fmt.Errorf("one MAP at most, got %q", pos)
	}
	return o, nil
}

const usage = `usage: wayfinder-map [MAP] [-R owner/repo] [--look cards|compact] [--dir auto|lr|tb] [--interval 30s] [--list] [--no-mouse] [--no-images]

MAP is 123, #123, owner/repo#123 or an issue URL; without it, a picker lists
the repo's open maps. WAYFINDER_MAP_LOOK, WAYFINDER_MAP_DIR,
WAYFINDER_MAP_INTERVAL, WAYFINDER_MAP_LIST, WAYFINDER_MAP_NO_MOUSE and
WAYFINDER_MAP_NO_IMAGES (1 or 0) set the flags of those names; a flag beats
its variable. Unless --list is set either way, the tickets are listed if the
terminal doesn't say it shows Kitty graphics. The detail view's mini-map is
drawn wherever the map is, except in WezTerm, which can't show it.
`

type lookValue draw.Look

func (v *lookValue) String() string { return draw.Look(*v).String() }

func (v *lookValue) Set(s string) error {
	switch s {
	case "cards":
		*v = lookValue(draw.Cards)
	case "compact":
		*v = lookValue(draw.Compact)
	default:
		return errors.New("want cards or compact")
	}
	return nil
}

type dirValue struct {
	dir  *layout.Dir
	auto *bool
}

func (v dirValue) String() string {
	switch {
	case v.auto == nil || *v.auto:
		return "auto"
	case *v.dir == layout.TB:
		return "tb"
	}
	return "lr"
}

func (v dirValue) Set(s string) error {
	switch s {
	case "auto":
		*v.dir, *v.auto = layout.LR, true
	case "lr":
		*v.dir, *v.auto = layout.LR, false
	case "tb":
		*v.dir, *v.auto = layout.TB, false
	default:
		return errors.New("want auto, lr or tb")
	}
	return nil
}

type intervalValue time.Duration

func (v *intervalValue) String() string { return time.Duration(*v).String() }

func (v *intervalValue) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil || d < minInterval {
		return fmt.Errorf("want a duration of at least %v", minInterval)
	}
	*v = intervalValue(d)
	return nil
}

// setBool is a bool flag that knows whether it was set.
type setBool struct{ v, set bool }

func (b *setBool) String() string   { return strconv.FormatBool(b.v) }
func (b *setBool) IsBoolFlag() bool { return true }

func (b *setBool) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return errors.New("want 1 or 0, or true or false")
	}
	b.v, b.set = v, true
	return nil
}

// parseInterleaved lets flags follow positional arguments, as in
// `wayfinder-map 214 -R owner/repo`, which the flag package alone stops at.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos, args = append(pos, args[0]), args[1:]
	}
}
