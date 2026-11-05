package wayfinder_test

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("../../fixtures/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func numbersIn(m *wayfinder.Map, s wayfinder.State) []int {
	var out []int
	for _, t := range m.Tickets {
		if t.State == s {
			out = append(out, t.Number)
		}
	}
	return out
}

func TestParseFixtures(t *testing.T) {
	tests := []struct {
		fixture  string
		frontier []int
		blocked  []int
		done     []int
	}{
		{"map-214.json", []int{215, 216}, []int{217, 218, 219, 220, 221, 222}, nil},
		{"map-175.json", []int{178, 180, 181, 182, 184, 190, 191, 192, 193}, []int{179, 183, 189}, []int{176, 177}},
		// Closed tickets are done even though they are still assigned.
		{"map-152.json", []int{154, 155, 157, 160}, nil, []int{153, 156, 158, 159}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			m, err := wayfinder.Parse(load(t, tt.fixture))
			if err != nil {
				t.Fatal(err)
			}
			if got := numbersIn(m, wayfinder.Frontier); !slices.Equal(got, tt.frontier) {
				t.Errorf("frontier = %v, want %v", got, tt.frontier)
			}
			if got := numbersIn(m, wayfinder.Blocked); !slices.Equal(got, tt.blocked) {
				t.Errorf("blocked = %v, want %v", got, tt.blocked)
			}
			if got := numbersIn(m, wayfinder.Done); !slices.Equal(got, tt.done) {
				t.Errorf("done = %v, want %v", got, tt.done)
			}
		})
	}
}

// edit applies f to each sub-issue node of a fixture, keyed by number.
func edit(t *testing.T, raw []byte, f func(n int, node map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	issue := doc["data"].(map[string]any)["repository"].(map[string]any)["issue"].(map[string]any)
	for _, n := range issue["subIssues"].(map[string]any)["nodes"].([]any) {
		node := n.(map[string]any)
		f(int(node["number"].(float64)), node)
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseVariants(t *testing.T) {
	raw := edit(t, load(t, "map-214.json"), func(n int, node map[string]any) {
		switch n {
		case 215:
			node["assignees"] = map[string]any{"nodes": []any{map[string]any{"login": "someone"}}}
		case 216:
			node["state"] = "CLOSED"
		case 217, 218, 219:
			for _, b := range node["blockedBy"].(map[string]any)["nodes"].([]any) {
				if b := b.(map[string]any); b["number"] == 216.0 {
					b["state"] = "CLOSED"
				}
			}
		case 222:
			// Its only blocker, #221, is replaced by an open one in another repo.
			node["blockedBy"] = map[string]any{"nodes": []any{map[string]any{
				"number": 9, "state": "OPEN", "repository": map[string]any{"nameWithOwner": "other/repo"},
			}}}
		}
	})
	m, err := wayfinder.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range map[int]wayfinder.State{
		215: wayfinder.Claimed,
		216: wayfinder.Done,
		217: wayfinder.Frontier, // its only blocker, #216, is closed
		219: wayfinder.Blocked,  // #216 is closed but #218 is still open
		222: wayfinder.Blocked,  // by a ticket in another repo
	} {
		if got := m.Ticket(n).State; got != want {
			t.Errorf("#%d is %v, want %v", n, got, want)
		}
	}
	var faded, toCrossRepo []wayfinder.Edge
	for _, e := range m.Edges {
		if e.Faded {
			faded = append(faded, e)
		}
		if e.To == 222 {
			toCrossRepo = append(toCrossRepo, e)
		}
	}
	if want := []wayfinder.Edge{{216, 217, true}, {216, 218, true}, {216, 219, true}}; !slices.Equal(faded, want) {
		t.Errorf("faded edges = %v, want %v", faded, want)
	}
	if len(toCrossRepo) != 0 {
		t.Errorf("a cross-repo blocker drew edges %v", toCrossRepo)
	}
}

func TestParseMissingMap(t *testing.T) {
	_, err := wayfinder.Parse([]byte(`{"data":{"repository":{"nameWithOwner":"a/b","issue":null}}}`))
	if !errors.Is(err, wayfinder.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSanitizeStripsEscapes(t *testing.T) {
	in := "Fix \x1b]8;;https://evil\x1b\\link\x1b]8;;\x1b\\ \u202eabc\u200b\u0085\x9b2J"
	if got, want := wayfinder.Sanitize(in), "Fix ]8;;https://evil\\link]8;;\\ abc2J"; got != want {
		t.Errorf("Sanitize = %q, want %q", got, want)
	}
}

func TestParseList(t *testing.T) {
	raw := []byte(`{"data":{"repository":{"nameWithOwner":"a/b","issues":{"totalCount":2,"nodes":[
		{"number":7,"title":"Newest \u001b[31mmap","updatedAt":"2026-11-05T10:00:00Z"},
		{"number":3,"title":"Older map","updatedAt":"2026-11-01T10:00:00Z"}]}}}}`)
	got, err := wayfinder.ParseList(raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []wayfinder.Summary{{Number: 7, Title: "Newest [31mmap"}, {Number: 3, Title: "Older map"}}
	if !slices.Equal(got, want) {
		t.Errorf("ParseList = %+v, want %+v", got, want)
	}
	if _, err := wayfinder.ParseList([]byte(`{"data":{"repository":null}}`)); !errors.Is(err, wayfinder.ErrNotFound) {
		t.Errorf("missing repo: err = %v, want ErrNotFound", err)
	}
}

func TestTicketTypes(t *testing.T) {
	raw := edit(t, load(t, "map-214.json"), func(n int, node map[string]any) {
		label := func(names ...string) map[string]any {
			var ns []any
			for _, s := range names {
				ns = append(ns, map[string]any{"name": s})
			}
			return map[string]any{"nodes": ns}
		}
		switch n {
		case 215:
			node["labels"] = label("bug")
		case 216:
			node["labels"] = label("wayfinder:map")
		case 217:
			node["labels"] = label("ready-for-agent", "wayfinder:research")
		}
	})
	m, err := wayfinder.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	for n, want := range map[int]wayfinder.Type{215: wayfinder.NoType, 216: wayfinder.NoType, 217: wayfinder.Research, 220: wayfinder.Prototype} {
		if got := m.Ticket(n).Type; got != want {
			t.Errorf("#%d has type %q, want %q", n, got, want)
		}
	}
}
