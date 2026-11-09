package wayfinder

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

// Issue is one issue as the detail view shows it. It may be a ticket, the
// map itself, or an issue off the map, in the map's repo or another.
type Issue struct {
	Repo      string
	Number    int
	Title     string
	Closed    bool
	Type      Type
	Labels    []string
	Assignees []string

	// Body is the issue's markdown as GitHub returns it, unsanitised: show
	// it only through the markdown renderer, which sanitises as it renders.
	Body string
	// BodyHTML is the body as GitHub renders it, unsanitised. It is read
	// only for its images' signed URLs and is never shown.
	BodyHTML string

	Parent            *Related
	SubIssues         []Related
	SubTotal, SubDone int // over every sub-issue, not just those fetched

	BlockedBy, Blocking []Related
}

// Related is an issue that another links to: its parent, a sub-issue, a
// blocker or an issue it blocks.
type Related struct {
	Repo   string
	Number int
	Title  string
	Closed bool

	// Only a sub-issue's are fetched: its assignees, and the type its
	// labels name. Type is empty for the others.
	Assignees []string
	Type      Type
}

// Ref names the issue as seen from repo: #N, or owner/repo#N when it lives
// in another repo.
func (r Related) Ref(repo string) string {
	return Blocker{Repo: r.Repo, Number: r.Number}.Ref(repo)
}

// State is the issue's state by the same rule as a ticket's.
func (i *Issue) State() State {
	open := slices.ContainsFunc(i.BlockedBy, func(r Related) bool { return !r.Closed })
	return stateOf(i.Closed, len(i.Assignees) > 0, open)
}

// ErrIssueNotFound means the repo or the issue doesn't exist, or isn't
// visible.
var ErrIssueNotFound = errors.New("issue not found")

type rawRelated struct {
	Number     int
	Title      string
	State      string
	Repository struct{ NameWithOwner string }
}

func (r rawRelated) related() Related {
	return Related{Repo: Sanitize(r.Repository.NameWithOwner), Number: r.Number, Title: Sanitize(r.Title), Closed: r.State == "CLOSED"}
}

func relatedOf(rs []rawRelated) []Related {
	var out []Related
	for _, r := range rs {
		out = append(out, r.related())
	}
	return out
}

// rawSubIssue is a sub-issue, which comes with its assignees and labels.
type rawSubIssue struct {
	rawRelated
	Assignees rawLogins
	Labels    rawLabels
}

func subIssuesOf(rs []rawSubIssue) []Related {
	var out []Related
	for _, r := range rs {
		s := r.related()
		s.Assignees, s.Type = r.Assignees.logins(), typeOf(r.Labels.names())
		out = append(out, s)
	}
	return out
}

// ParseIssue reads the response to github.IssueQuery.
func ParseIssue(raw []byte) (*Issue, error) {
	var r struct {
		gqlErrors
		Data struct {
			Repository *struct {
				NameWithOwner string
				Issue         *struct {
					Number           int
					Title            string
					State            string
					Body             string
					BodyHTML         string
					Labels           rawLabels
					Assignees        rawLogins
					Parent           *rawRelated
					SubIssuesSummary struct{ Total, Completed int }
					SubIssues        nodes[rawSubIssue]
					BlockedBy        nodes[rawRelated]
					Blocking         nodes[rawRelated]
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("parse issue: %w", err)
	}
	if err := r.err(); err != nil {
		return nil, err
	}
	repo := r.Data.Repository
	if repo == nil || repo.Issue == nil {
		return nil, ErrIssueNotFound
	}
	is := repo.Issue
	out := &Issue{
		Repo: Sanitize(repo.NameWithOwner), Number: is.Number, Title: Sanitize(is.Title), Closed: is.State == "CLOSED",
		Body: is.Body, BodyHTML: is.BodyHTML, SubTotal: is.SubIssuesSummary.Total, SubDone: is.SubIssuesSummary.Completed,
		Labels: is.Labels.names(), Assignees: is.Assignees.logins(),
		SubIssues: subIssuesOf(is.SubIssues.Nodes), BlockedBy: relatedOf(is.BlockedBy.Nodes), Blocking: relatedOf(is.Blocking.Nodes),
	}
	out.Type = typeOf(out.Labels)
	if is.Parent != nil {
		p := is.Parent.related()
		out.Parent = &p
	}
	return out, nil
}
