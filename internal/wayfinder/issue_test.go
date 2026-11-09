package wayfinder_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

func TestParseIssue(t *testing.T) {
	is, err := wayfinder.ParseIssue(load(t, "issue-222.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := &wayfinder.Issue{
		Repo: "banked/ask-barry", Number: 222, Title: "Switch ask-barry to the standalone Sandcastle",
		Type: wayfinder.Task, Labels: []string{"wayfinder:task"},
		Body:     "Swap the vendored copy for the standalone one.",
		BodyHTML: `<p dir="auto">Swap the vendored copy for the standalone one.</p>`,
		Parent:   &wayfinder.Related{Repo: "banked/ask-barry", Number: 214, Title: "Wayfinder: run Sandcastle standalone against any Banked repo"},
		SubIssues: []wayfinder.Related{
			{Repo: "banked/ask-barry", Number: 301, Title: "Remove the vendored copy", Closed: true},
			{Repo: "banked/ask-barry", Number: 302, Title: "Point CI at the standalone image"},
		},
		SubTotal: 2, SubDone: 1,
		BlockedBy: []wayfinder.Related{
			{Repo: "banked/ask-barry", Number: 221, Title: "Move Sandcastle into its own home"},
			{Repo: "other/repo", Number: 9, Title: "Publish the sandbox base image"},
			{Repo: "banked/ask-barry", Number: 88, Title: "Agree the image registry", Closed: true},
		},
	}
	if !reflect.DeepEqual(is, want) {
		t.Errorf("ParseIssue =\n%+v\nwant\n%+v", is, want)
	}
	if is.State() != wayfinder.Blocked {
		t.Errorf("state = %v, want blocked", is.State())
	}
}

// Every string but the body is sanitised; the body is left for the
// markdown renderer, which sanitises it as it renders.
func TestParseIssueSanitizes(t *testing.T) {
	raw := []byte(`{"data":{"repository":{"nameWithOwner":"a/b","issue":{"number":1,"title":"x\u001b[2Jy","state":"OPEN",
		"body":"b\u001b[31m","labels":{"nodes":[{"name":"l‮abel"}]},"assignees":{"nodes":[{"login":"oc\u0007to"}]},
		"parent":{"number":2,"title":"p\u001b]0;t\u0007q","repository":{"nameWithOwner":"a/b"}},
		"blockedBy":{"nodes":[{"number":3,"title":"\u009b2Jz","state":"OPEN","repository":{"nameWithOwner":"a/b"}}]}}}}}`)
	is, err := wayfinder.ParseIssue(raw)
	if err != nil {
		t.Fatal(err)
	}
	if is.Title != "xy" || is.Labels[0] != "label" || is.Assignees[0] != "octo" || is.Parent.Title != "pq" || is.BlockedBy[0].Title != "z" {
		t.Errorf("unsanitised: %+v", is)
	}
	if is.Body != "b\x1b[31m" {
		t.Errorf("the raw body changed: %q", is.Body)
	}
}

func TestParseIssueErrors(t *testing.T) {
	if _, err := wayfinder.ParseIssue([]byte(`{"data":{"repository":{"issue":null}}}`)); !errors.Is(err, wayfinder.ErrIssueNotFound) {
		t.Errorf("a missing issue: err = %v", err)
	}
	if _, err := wayfinder.ParseIssue([]byte(`{"errors":[{"message":"bad"}]}`)); err == nil || err.Error() != "github: bad" {
		t.Errorf("GraphQL errors: err = %v", err)
	}
}
