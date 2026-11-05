// Package github is the only code that runs gh. It runs two fixed GraphQL
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

// Source loads raw query responses. Tests use a fixture-backed fake.
type Source interface {
	LoadMap(ctx context.Context, number int) ([]byte, error)
	ListMaps(ctx context.Context) ([]byte, error)
}

// GH runs the queries with `gh api graphql` against one target.
type GH struct{ Target Target }

func (g GH) LoadMap(ctx context.Context, number int) ([]byte, error) {
	return g.run(ctx, MapQuery, "-F", "number="+strconv.Itoa(number))
}

func (g GH) ListMaps(ctx context.Context) ([]byte, error) {
	return g.run(ctx, ListQuery)
}

func (g GH) run(ctx context.Context, query string, args ...string) ([]byte, error) {
	argv := []string{"api", "graphql", "-f", "query=" + query}
	if g.Target.Repo != "" {
		owner, name, _ := strings.Cut(g.Target.Repo, "/")
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
	if g.Target.Repo == "" {
		return nil, fmt.Errorf("gh: %s (outside a GitHub checkout, pass -R owner/repo)", msg)
	}
	return nil, fmt.Errorf("gh: %s", msg)
}
