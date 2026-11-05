// Command wayfinder-map draws a Wayfinder map from GitHub Issues as a graph
// in the terminal. It reads through the user's gh login and never writes.
//
//	wayfinder-map [MAP] [-R owner/repo]
//
// MAP is 123, #123 or owner/repo#123. Without it, the repo's most recently
// updated open map opens.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/distrail-io/wayfinder-map/internal/github"
	"github.com/distrail-io/wayfinder-map/internal/tui"
)

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
	fs := flag.NewFlagSet("wayfinder-map", flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: wayfinder-map [MAP] [-R owner/repo]\n\nMAP is 123, #123 or owner/repo#123; without it, the newest open map opens.")
		fs.PrintDefaults()
	}
	var repo string
	fs.StringVar(&repo, "R", "", "the repo to read, as owner/repo")
	fs.StringVar(&repo, "repo", "", "the same as -R")
	showVersion := fs.Bool("version", false, "print the version and exit")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, "wayfinder-map", version)
		return nil
	}
	if repo != "" && !github.ValidRepo(repo) {
		return fmt.Errorf("bad -R %q: want owner/repo", repo)
	}
	var ref github.Ref
	switch len(pos) {
	case 0:
	case 1:
		if ref, err = github.ParseRef(pos[0]); err != nil {
			return err
		}
	default:
		return fmt.Errorf("one MAP at most, got %q", pos)
	}

	src := github.GH{Target: github.Resolve(ref, repo, getenv)}
	_, err = tea.NewProgram(tui.New(tui.Config{Source: src, Number: ref.Number})).Run()
	return err
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
