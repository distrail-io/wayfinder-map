package github

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Ref is a map reference from the command line.
type Ref struct {
	Repo   string // owner/repo, or "" when not given
	Number int
}

// ParseRef reads 123, #123, owner/repo#123 or an issue URL,
// https://github.com/owner/repo/issues/123.
func ParseRef(s string) (Ref, error) {
	var repo, num string
	if strings.Contains(s, "://") || strings.HasPrefix(s, "github.com/") {
		repo, num = issueURL(s)
	} else if r, n, hasRepo := strings.Cut(s, "#"); hasRepo {
		repo, num = r, n
	} else {
		num = s
	}
	n, err := strconv.Atoi(num)
	if err != nil || n <= 0 || repo != "" && !ValidRepo(repo) {
		return Ref{}, fmt.Errorf("bad map reference %q: want 123, #123, owner/repo#123 or an issue URL", s)
	}
	return Ref{Repo: repo, Number: n}, nil
}

// issueURL splits a github.com issue URL into owner/repo and the number,
// ignoring any query or fragment. Anything else gives "", "".
func issueURL(s string) (repo, num string) {
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
		return "", ""
	}
	parts := strings.Split(strings.TrimSuffix(u.Path, "/"), "/")
	if len(parts) != 5 || parts[0] != "" || parts[3] != "issues" {
		return "", ""
	}
	return parts[1] + "/" + parts[2], parts[4]
}

// ValidRepo reports whether s looks like owner/repo.
func ValidRepo(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}

// Target is where gh looks for the map: a named repo, or else the repo checked
// out in Dir ("" for the process's cwd).
type Target struct {
	Repo string
	Dir  string
}

// Resolve picks the repo; the first match wins:
//  1. owner/repo in the map reference
//  2. the -R flag
//  3. GH_REPO
//  4. the host's idea of the current checkout (Herdr)
//  5. the process's cwd
func Resolve(ref Ref, flagRepo string, getenv func(string) string) Target {
	for _, repo := range []string{ref.Repo, flagRepo, getenv("GH_REPO")} {
		if repo != "" {
			return Target{Repo: repo}
		}
	}
	return Target{Dir: herdrDir(getenv("HERDR_PLUGIN_CONTEXT_JSON"))}
}

// herdrDir reads the checkout a Herdr plugin pane was opened for: the
// workspace's worktree, else the focused pane's cwd.
func herdrDir(contextJSON string) string {
	if contextJSON == "" {
		return ""
	}
	var c struct {
		Worktree *struct {
			CheckoutPath string `json:"checkout_path"`
		} `json:"worktree"`
		FocusedPaneCwd string `json:"focused_pane_cwd"`
	}
	if json.Unmarshal([]byte(contextJSON), &c) != nil {
		return ""
	}
	if c.Worktree != nil && c.Worktree.CheckoutPath != "" {
		return c.Worktree.CheckoutPath
	}
	return c.FocusedPaneCwd
}
