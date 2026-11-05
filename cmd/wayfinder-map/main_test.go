package main

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/distrail-io/wayfinder-map/internal/draw"
	"github.com/distrail-io/wayfinder-map/internal/github"
	"github.com/distrail-io/wayfinder-map/internal/layout"
)

func env(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }

func TestBadIntervalFailsAtStartup(t *testing.T) {
	for _, arg := range []string{"5s", "9.9s", "-1m", "soon"} {
		err := run([]string{"214", "--interval", arg}, env(nil), io.Discard)
		if err == nil || !strings.Contains(err.Error(), "interval") {
			t.Errorf("--interval %s: err = %v, want one naming the flag", arg, err)
		}
	}
}

func TestDefaults(t *testing.T) {
	got, err := parseOptions(nil, env(nil))
	if err != nil {
		t.Fatal(err)
	}
	want := options{look: draw.Cards, autoDir: true, interval: 30 * time.Second}
	if got != want {
		t.Errorf("parseOptions() = %+v, want %+v", got, want)
	}
}

func TestEnvBeatsDefault(t *testing.T) {
	got, err := parseOptions(nil, env(map[string]string{
		"WAYFINDER_MAP_LOOK":     "compact",
		"WAYFINDER_MAP_DIR":      "tb",
		"WAYFINDER_MAP_INTERVAL": "45s",
		"WAYFINDER_MAP_LIST":     "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := options{look: draw.Compact, dir: layout.TB, interval: 45 * time.Second, list: setBool{v: true, set: true}}
	if got != want {
		t.Errorf("parseOptions = %+v, want %+v", got, want)
	}
}

func TestFlagBeatsEnv(t *testing.T) {
	got, err := parseOptions([]string{"--look", "cards", "--dir", "lr", "--interval", "20s", "--list=false"}, env(map[string]string{
		"WAYFINDER_MAP_LOOK":     "compact",
		"WAYFINDER_MAP_DIR":      "tb",
		"WAYFINDER_MAP_INTERVAL": "45s",
		"WAYFINDER_MAP_LIST":     "1",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want := options{look: draw.Cards, dir: layout.LR, interval: 20 * time.Second, list: setBool{set: true}}
	if got != want {
		t.Errorf("parseOptions = %+v, want %+v", got, want)
	}
}

func TestDirAutoFlagBeatsEnv(t *testing.T) {
	got, err := parseOptions([]string{"--dir", "auto"}, env(map[string]string{"WAYFINDER_MAP_DIR": "tb"}))
	if err != nil || !got.autoDir {
		t.Errorf("--dir auto over WAYFINDER_MAP_DIR=tb: %+v, %v; want auto", got, err)
	}
}

func TestBadEnvNamesTheVariable(t *testing.T) {
	for name, v := range map[string]string{
		"WAYFINDER_MAP_LOOK":     "fancy",
		"WAYFINDER_MAP_DIR":      "up",
		"WAYFINDER_MAP_INTERVAL": "5s",
		"WAYFINDER_MAP_LIST":     "maybe",
	} {
		_, err := parseOptions(nil, env(map[string]string{name: v}))
		if err == nil || !strings.Contains(err.Error(), name) || !strings.Contains(err.Error(), "want") {
			t.Errorf("%s=%s: err = %v, want one naming the variable and what it wants", name, v, err)
		}
	}
	// A flag doesn't hide a bad variable.
	if _, err := parseOptions([]string{"--interval", "1m"}, env(map[string]string{"WAYFINDER_MAP_INTERVAL": "soon"})); err == nil {
		t.Error("a bad WAYFINDER_MAP_INTERVAL passed because --interval was set")
	}
	if _, err := parseOptions(nil, env(map[string]string{"WAYFINDER_MAP_INTERVAL": "9s"})); err == nil || !strings.Contains(err.Error(), "10s") {
		t.Errorf("WAYFINDER_MAP_INTERVAL=9s: err = %v, want the 10s minimum", err)
	}
}

func TestBadFlagNamesTheFlag(t *testing.T) {
	for _, args := range [][]string{{"--look", "fancy"}, {"--dir", "up"}, {"-R", "nope"}} {
		_, err := parseOptions(args, env(nil))
		if err == nil || !strings.Contains(err.Error(), strings.TrimLeft(args[0], "-")) {
			t.Errorf("%q: err = %v, want one naming the flag", args, err)
		}
	}
}

func TestMapForms(t *testing.T) {
	tests := []struct {
		args []string
		want github.Ref
	}{
		{nil, github.Ref{}},
		{[]string{"214"}, github.Ref{Number: 214}},
		{[]string{"#214"}, github.Ref{Number: 214}},
		{[]string{"banked/ask-barry#214"}, github.Ref{Repo: "banked/ask-barry", Number: 214}},
		{[]string{"https://github.com/banked/ask-barry/issues/214"}, github.Ref{Repo: "banked/ask-barry", Number: 214}},
		{[]string{"214", "-R", "a/b"}, github.Ref{Number: 214}},
	}
	for _, tt := range tests {
		got, err := parseOptions(tt.args, env(nil))
		if err != nil || got.ref != tt.want {
			t.Errorf("parseOptions(%q).ref = %+v, %v; want %+v", tt.args, got.ref, err, tt.want)
		}
	}
	if _, err := parseOptions([]string{"1", "2"}, env(nil)); err == nil {
		t.Error("two MAPs were accepted")
	}
	if _, err := parseOptions([]string{"https://github.com/a/b/pull/3"}, env(nil)); err == nil {
		t.Error("a pull request URL was accepted")
	}
}

// Without --list or its variable, the terminal is asked whether it shows
// Kitty graphics; set either way, it isn't.
func TestListUnlessTheTerminalShowsImages(t *testing.T) {
	for _, c := range []struct {
		args   []string
		env    map[string]string
		images bool // what the probe finds
		want   bool
		probed bool
	}{
		{nil, nil, true, false, true},
		{nil, nil, false, true, true},
		{[]string{"--list"}, nil, true, true, false},
		{[]string{"--list=false"}, nil, false, false, false},
		{nil, map[string]string{"WAYFINDER_MAP_LIST": "0"}, false, false, false},
	} {
		o, err := parseOptions(c.args, env(c.env))
		if err != nil {
			t.Fatal(err)
		}
		probed := false
		got := o.showList(func() bool { probed = true; return c.images })
		if got != c.want || probed != c.probed {
			t.Errorf("%q %v, images %v: list %v, probed %v; want %v, %v", c.args, c.env, c.images, got, probed, c.want, c.probed)
		}
	}
}
