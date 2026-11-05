package github_test

import (
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/github"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in   string
		want github.Ref
	}{
		{"214", github.Ref{Number: 214}},
		{"#214", github.Ref{Number: 214}},
		{"banked/ask-barry#214", github.Ref{Repo: "banked/ask-barry", Number: 214}},
	}
	for _, tt := range tests {
		got, err := github.ParseRef(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseRef(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "#", "abc", "0", "-3", "a/b", "a#3", "/b#3", "a/#3", "a/b/c#3"} {
		if _, err := github.ParseRef(bad); err == nil {
			t.Errorf("ParseRef(%q) accepted a bad reference", bad)
		}
	}
}

const herdrContext = `{
	"workspace_id": "w3C",
	"worktree": {"repo_root": "/git/app", "checkout_path": "/wt/app/feature", "is_linked_worktree": true},
	"focused_pane_cwd": "/somewhere/else"
}`

func TestResolveFirstMatchWins(t *testing.T) {
	tests := []struct {
		name string
		ref  github.Ref
		flag string
		env  map[string]string
		want github.Target
	}{
		{"repo in MAP beats -R", github.Ref{Repo: "a/map", Number: 1}, "a/flag",
			map[string]string{"GH_REPO": "a/env"}, github.Target{Repo: "a/map"}},
		{"-R beats GH_REPO", github.Ref{Number: 1}, "a/flag",
			map[string]string{"GH_REPO": "a/env"}, github.Target{Repo: "a/flag"}},
		{"GH_REPO beats Herdr", github.Ref{Number: 1}, "",
			map[string]string{"GH_REPO": "a/env", "HERDR_PLUGIN_CONTEXT_JSON": herdrContext}, github.Target{Repo: "a/env"}},
		{"Herdr worktree checkout", github.Ref{Number: 1}, "",
			map[string]string{"HERDR_PLUGIN_CONTEXT_JSON": herdrContext}, github.Target{Dir: "/wt/app/feature"}},
		{"Herdr focused pane without a worktree", github.Ref{Number: 1}, "",
			map[string]string{"HERDR_PLUGIN_CONTEXT_JSON": `{"worktree": null, "focused_pane_cwd": "/git/app/sub"}`},
			github.Target{Dir: "/git/app/sub"}},
		{"unreadable Herdr context falls through to cwd", github.Ref{Number: 1}, "",
			map[string]string{"HERDR_PLUGIN_CONTEXT_JSON": `not json`}, github.Target{}},
		{"cwd", github.Ref{Number: 1}, "", nil, github.Target{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := github.Resolve(tt.ref, tt.flag, func(k string) string { return tt.env[k] })
			if got != tt.want {
				t.Errorf("Resolve = %+v, want %+v", got, tt.want)
			}
		})
	}
}
