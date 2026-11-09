//go:build contract

package github

// The contract check catches GitHub's GraphQL schema drifting under the
// queries, which carry no version. It runs them for real against this repo's
// own map, so it needs a gh login or GH_TOKEN:
//
//	go test -tags contract -run TestContract -count=1 ./internal/github

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

const (
	contractRepo = "distrail-io/wayfinder-map"
	contractMap  = 1 // "wayfinder-map v1: terminal graph viewer"

	// The detail view's spec ticket: a sub-issue of #25 that blocks #27.
	contractIssue       = 26
	contractIssueParent = 25
)

func TestContract(t *testing.T) {
	ctx := context.Background()
	g := GH{Target: Target{Repo: contractRepo}}

	t.Run("ListQuery runs and parses", func(t *testing.T) {
		raw, err := g.ListMaps(ctx)
		if err != nil {
			t.Fatal(err)
		}
		l, err := wayfinder.ParseList(raw)
		if err != nil {
			t.Fatal(err)
		}
		if l.Repo != contractRepo {
			t.Errorf("listed repo %q, want %q", l.Repo, contractRepo)
		}
	})

	t.Run("MapQuery runs and parses", func(t *testing.T) {
		raw, err := g.LoadMap(ctx, contractMap)
		if err != nil {
			t.Fatal(err)
		}
		m, err := wayfinder.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		// Fields that vanished would parse as zeros, so check what the map
		// is known to hold.
		if m.Repo != contractRepo || m.Number != contractMap || m.Title == "" {
			t.Errorf("map = %s#%d %q", m.Repo, m.Number, m.Title)
		}
		if len(m.Tickets) == 0 || len(m.Edges) == 0 {
			t.Errorf("map #%d has %d tickets and %d edges, want some of each", contractMap, len(m.Tickets), len(m.Edges))
		}
	})

	t.Run("IssueQuery runs and parses", func(t *testing.T) {
		raw, err := g.LoadIssue(ctx, contractRepo, contractIssue)
		if err != nil {
			t.Fatal(err)
		}
		is, err := wayfinder.ParseIssue(raw)
		if err != nil {
			t.Fatal(err)
		}
		if is.Repo != contractRepo || is.Number != contractIssue || is.Title == "" || is.Body == "" || is.BodyHTML == "" {
			t.Errorf("issue = %s#%d %q, body %d bytes, body HTML %d bytes", is.Repo, is.Number, is.Title, len(is.Body), len(is.BodyHTML))
		}
		if is.Parent == nil || is.Parent.Number != contractIssueParent || is.Parent.Title == "" {
			t.Errorf("issue #%d's parent = %+v, want #%d", contractIssue, is.Parent, contractIssueParent)
		}
		if len(is.Blocking) == 0 || is.Blocking[0].Title == "" || is.Blocking[0].Repo != contractRepo {
			t.Errorf("issue #%d blocks %+v, want some with titles", contractIssue, is.Blocking)
		}
		// The map issue has sub-issues.
		raw, err = g.LoadIssue(ctx, contractRepo, contractMap)
		if err != nil {
			t.Fatal(err)
		}
		if m, err := wayfinder.ParseIssue(raw); err != nil || len(m.SubIssues) == 0 || m.SubTotal == 0 {
			t.Errorf("map issue #%d: %d sub-issues of %d, err %v", contractMap, len(m.SubIssues), m.SubTotal, err)
		}
	})

	for name, query := range map[string]string{"ListQuery": ListQuery, "MapQuery": MapQuery, "IssueQuery": IssueQuery} {
		t.Run(name+" uses no deprecated field", func(t *testing.T) {
			s := &schema{t: t, types: map[string]map[string]field{}}
			w := &walker{t: t, toks: tokens(query), s: s}
			for w.toks[w.i] != "{" {
				w.skip()
			}
			w.selectionSet("Query")
			if w.fields == 0 {
				t.Fatal("found no fields")
			}
		})
	}
}

type field struct {
	Name              string
	IsDeprecated      bool
	DeprecationReason string
	Type              typeRef
}

type typeRef struct {
	Name   string
	OfType *typeRef
}

// named unwraps lists and non-nulls to the named type.
func (r typeRef) named() string {
	for r.Name == "" && r.OfType != nil {
		r = *r.OfType
	}
	return r.Name
}

const typeQuery = `query($type: String!) {
  __type(name: $type) {
    fields(includeDeprecated: true) {
      name isDeprecated deprecationReason
      type { name ofType { name ofType { name ofType { name } } } }
    }
  }
}`

// schema introspects types as the walk reaches them.
type schema struct {
	t     *testing.T
	types map[string]map[string]field
}

func (s *schema) fields(typ string) map[string]field {
	if fs, ok := s.types[typ]; ok {
		return fs
	}
	out, err := exec.Command("gh", "api", "graphql", "-f", "query="+typeQuery, "-f", "type="+typ).Output()
	if ee, ok := err.(*exec.ExitError); ok {
		s.t.Fatalf("introspect %s: %v: %s", typ, err, ee.Stderr)
	} else if err != nil {
		s.t.Fatalf("introspect %s: %v", typ, err)
	}
	var r struct {
		Data struct {
			Type *struct{ Fields []field } `json:"__type"`
		}
	}
	if err := json.Unmarshal(out, &r); err != nil || r.Data.Type == nil {
		s.t.Fatalf("introspect %s: %v %s", typ, err, out)
	}
	fs := map[string]field{}
	for _, f := range r.Data.Type.Fields {
		fs[f.Name] = f
	}
	s.types[typ] = fs
	return fs
}

// walker reads a query's selection sets, checking each field against the
// type it is selected on.
type walker struct {
	t      *testing.T
	toks   []string
	i      int
	s      *schema
	fields int
}

func (w *walker) next() string {
	if w.i == len(w.toks) {
		w.t.Fatal("query ended early")
	}
	w.i++
	return w.toks[w.i-1]
}

func (w *walker) peek() string {
	if w.i == len(w.toks) {
		return ""
	}
	return w.toks[w.i]
}

// skip passes one token, or a whole bracketed group.
func (w *walker) skip() {
	open := w.next()
	if open != "(" && open != "{" {
		return
	}
	for depth := 1; depth > 0; {
		switch w.next() {
		case "(", "{":
			depth++
		case ")", "}":
			depth--
		}
	}
}

func (w *walker) selectionSet(typ string) {
	if w.next() != "{" {
		w.t.Fatalf("want { after %s", typ)
	}
	for w.peek() != "}" {
		name := w.next()
		if w.peek() == ":" { // an alias
			w.next()
			name = w.next()
		}
		f, ok := w.s.fields(typ)[name]
		if !ok {
			w.t.Errorf("%s.%s doesn't exist", typ, name)
		} else if f.IsDeprecated {
			w.t.Errorf("%s.%s is deprecated: %s", typ, name, f.DeprecationReason)
		}
		w.fields++
		if w.peek() == "(" {
			w.skip()
		}
		if w.peek() == "{" {
			if !ok {
				w.skip()
				continue
			}
			w.selectionSet(f.Type.named())
		}
	}
	w.next()
}

// tokens splits a query into names, strings and the punctuation the walk
// needs; it drops the rest.
func tokens(q string) []string {
	var out []string
	isName := func(c byte) bool {
		return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
	}
	for i := 0; i < len(q); {
		switch c := q[i]; {
		case c == '"':
			j := i + 1 + strings.IndexByte(q[i+1:], '"')
			out = append(out, q[i:j+1])
			i = j + 1
		case isName(c):
			j := i
			for j < len(q) && isName(q[j]) {
				j++
			}
			out = append(out, q[i:j])
			i = j
		case strings.IndexByte("{}():", c) >= 0:
			out = append(out, string(c))
			i++
		default:
			i++
		}
	}
	return out
}
