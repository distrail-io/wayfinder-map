// Package github is the only code that runs gh. It runs three fixed GraphQL
// queries through the user's existing gh login and returns the raw JSON.
package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

// MapQuery fetches one map and all its tickets in a single call.
// wayfinder.Parse reads its response.
const MapQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    issue(number: $number) {
      number title state
      labels(first: 20) { nodes { name } }
      subIssuesSummary { total completed }
      subIssues(first: 100) {
        pageInfo { hasNextPage }
        nodes {
          number title state stateReason
          labels(first: 20) { nodes { name } }
          assignees(first: 5) { nodes { login } }
          issueDependenciesSummary { blockedBy totalBlockedBy blocking totalBlocking }
          blockedBy(first: 50) { pageInfo { hasNextPage } nodes { number state repository { nameWithOwner } } }
        }
      }
    }
  }
}`

// ListQuery fetches a repo's open maps, most recently updated first.
const ListQuery = `query($owner: String!, $name: String!) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    issues(labels: ["wayfinder:map"], states: OPEN, first: 50, orderBy: {field: UPDATED_AT, direction: DESC}) {
      totalCount
      nodes {
        number title updatedAt
        subIssuesSummary { total completed }
        subIssues(first: 100) { nodes { state assignees { totalCount } issueDependenciesSummary { blockedBy } } }
      }
    }
  }
}`

// IssueQuery fetches one issue, on the map or not, for the detail view.
// wayfinder.ParseIssue reads its response.
const IssueQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    nameWithOwner
    issue(number: $number) {
      number title state body bodyHTML
      labels(first: 20) { nodes { name } }
      assignees(first: 10) { nodes { login } }
      parent { number title state repository { nameWithOwner } }
      subIssuesSummary { total completed }
      subIssues(first: 100) { nodes { number title state repository { nameWithOwner } } }
      blockedBy(first: 50) { nodes { number title state repository { nameWithOwner } } }
      blocking(first: 50) { nodes { number title state repository { nameWithOwner } } }
    }
  }
}`

// Source loads raw query responses. Tests use a fixture-backed fake.
type Source interface {
	LoadMap(ctx context.Context, number int) ([]byte, error)
	ListMaps(ctx context.Context) ([]byte, error)
	// LoadIssue loads issue number in repo, given as owner/name.
	LoadIssue(ctx context.Context, repo string, number int) ([]byte, error)
}

// GH runs the queries with `gh api graphql` against one target.
type GH struct{ Target Target }

func (g GH) LoadMap(ctx context.Context, number int) ([]byte, error) {
	return g.run(ctx, MapQuery, g.Target.Repo, "-F", "number="+strconv.Itoa(number))
}

func (g GH) ListMaps(ctx context.Context) ([]byte, error) {
	return g.run(ctx, ListQuery, g.Target.Repo)
}

// LoadIssue names its repo, as the issue may live outside the target.
func (g GH) LoadIssue(ctx context.Context, repo string, number int) ([]byte, error) {
	if !ValidRepo(repo) {
		return nil, fmt.Errorf("bad repo %q", repo)
	}
	return g.run(ctx, IssueQuery, repo, "-F", "number="+strconv.Itoa(number))
}

// run runs query against repo, or if that is empty the repo checked out in
// the target's directory.
func (g GH) run(ctx context.Context, query, repo string, args ...string) ([]byte, error) {
	argv := []string{"api", "graphql", "-f", "query=" + query}
	if repo != "" {
		owner, name, _ := strings.Cut(repo, "/")
		argv = append(argv, "-f", "owner="+owner, "-f", "name="+name)
	} else {
		// gh fills these from the repo checked out in the working directory.
		argv = append(argv, "-F", "owner={owner}", "-F", "name={repo}")
	}
	cmd := exec.CommandContext(ctx, "gh", append(argv, args...)...)
	cmd.Dir = g.Target.Dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, errors.New("gh not found: install the GitHub CLI and run gh auth login")
	}
	msg := wayfinder.Sanitize(strings.Join(strings.Fields(stderr.String()), " "))
	msg = strings.TrimPrefix(msg, "gh: ")
	if msg == "" {
		msg = err.Error()
	}
	if repo == "" {
		return nil, fmt.Errorf("gh: %s (outside a GitHub checkout, pass -R owner/repo)", msg)
	}
	return nil, fmt.Errorf("gh: %s", msg)
}
