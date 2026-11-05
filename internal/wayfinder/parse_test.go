package wayfinder_test

import (
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

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
	if got, want := wayfinder.Sanitize(in), "Fix link abc2J"; got != want {
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
	want := []wayfinder.Summary{{Number: 7, Title: "Newest map"}, {Number: 3, Title: "Older map"}}
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

func TestParseSurfacesGraphQLErrors(t *testing.T) {
	// Partial data must not pass for an empty or smaller map.
	raw := []byte(`{"data":{"repository":{"nameWithOwner":"a/b","issue":null}},
		"errors":[{"type":"FORBIDDEN","message":"Resource not accessible \u001b[31mby integration"},{"message":"second"}]}`)
	_, err := wayfinder.Parse(raw)
	if err == nil || err.Error() != "github: Resource not accessible by integration; second" {
		t.Errorf("Parse err = %v", err)
	}
	if _, err := wayfinder.ParseList([]byte(`{"errors":[{"message":"bad"}]}`)); err == nil || err.Error() != "github: bad" {
		t.Errorf("ParseList err = %v", err)
	}
}

// warnings parses a variant of map-214 and returns its warnings.
func warnings(t *testing.T, f func(issue map[string]any)) []string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(load(t, "map-214.json"), &doc); err != nil {
		t.Fatal(err)
	}
	f(doc["data"].(map[string]any)["repository"].(map[string]any)["issue"].(map[string]any))
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wayfinder.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return m.Warnings
}

func ticketNode(issue map[string]any, n int) map[string]any {
	for _, x := range issue["subIssues"].(map[string]any)["nodes"].([]any) {
		if node := x.(map[string]any); node["number"] == float64(n) {
			return node
		}
	}
	panic(n)
}

func TestWarnings(t *testing.T) {
	blockedBy := func(refs ...[3]any) map[string]any {
		var ns []any
		for _, r := range refs {
			ns = append(ns, map[string]any{"number": r[0], "state": r[1], "repository": map[string]any{"nameWithOwner": r[2]}})
		}
		return map[string]any{"pageInfo": map[string]any{"hasNextPage": false}, "nodes": ns}
	}
	tests := []struct {
		name string
		edit func(issue map[string]any)
		want []string
	}{
		{"none on a clean map", func(map[string]any) {}, nil},
		{"closed map", func(is map[string]any) { is["state"] = "CLOSED" }, []string{"the map is closed"}},
		{"more tickets than fetched", func(is map[string]any) {
			is["subIssuesSummary"] = map[string]any{"total": 130, "completed": 0}
			is["subIssues"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
		}, []string{"truncated: showing 8 of 130 tickets"}},
		{"more blockers than fetched", func(is map[string]any) {
			ticketNode(is, 220)["blockedBy"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
			ticketNode(is, 216)["blockedBy"].(map[string]any)["pageInfo"] = map[string]any{"hasNextPage": true}
		}, []string{"truncated: more blockers than shown on #216, #220"}},
		{"cross-repo blockers", func(is map[string]any) {
			ticketNode(is, 222)["blockedBy"] = blockedBy([3]any{9, "OPEN", "other/repo"}, [3]any{221, "OPEN", "banked/ask-barry"})
			ticketNode(is, 217)["blockedBy"] = blockedBy([3]any{3, "CLOSED", "x/y"})
		}, []string{"blocked from another repo: #217 by x/y#3, #222 by other/repo#9"}},
		{"no type", func(is map[string]any) {
			ticketNode(is, 216)["labels"] = map[string]any{"nodes": []any{}}
			ticketNode(is, 215)["labels"] = map[string]any{"nodes": []any{map[string]any{"name": "bug"}}}
		}, []string{"no wayfinder:* type: #215, #216"}},
		{"cycle", func(is map[string]any) {
			// 216 → 218 → 219 already; 219 → 216 closes the loop. 221 blocks itself.
			ticketNode(is, 216)["blockedBy"] = blockedBy([3]any{219, "OPEN", "banked/ask-barry"})
			ticketNode(is, 221)["blockedBy"] = blockedBy([3]any{221, "OPEN", "banked/ask-barry"}, [3]any{220, "OPEN", "banked/ask-barry"})
		}, []string{"cycle: #216, #218, #219", "cycle: #221"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := warnings(t, tt.edit); !slices.Equal(got, tt.want) {
				t.Errorf("warnings = %q, want %q", got, tt.want)
			}
		})
	}
}

// invisible is what must never reach the terminal from a title: controls
// (so no escape sequence can start), format characters (bidi overrides,
// zero-width and tag characters) and line or paragraph separators.
func invisible(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) || r == utf8.RuneError
}

func subsequence(short, long []rune) bool {
	for _, r := range long {
		if len(short) > 0 && short[0] == r {
			short = short[1:]
		}
	}
	return len(short) == 0
}

func TestSanitizeRemovesWholeEscapeSequences(t *testing.T) {
	for in, want := range map[string]string{
		"a\x1b[1;31mred\x1b[0m b":                         "ared b",
		"\x1b]8;;https://evil\x07link\x1b]8;;\x1b\\ here": "link here",
		"\x1b]0;title\x1b\\ok":                            "ok",
		"\x1bP+q544e\x1b\\dcs \x1b_Ga=d\x1b\\apc":         "dcs apc",
		"\x1b7saved\x1b8 \x1b(Bcharset":                   "saved charset",
		"\u009b2Jc1 \u009d0;t\u009cosc":                   "c1 osc",
		"cut off \x1b[":                                   "cut off ",
		"cut off \x1b]8;;https://evil":                    "cut off ",
		"\x1b\x1b[2Jdouble":                               "double",
		"\x1b[2\u00e9":                                    "\u00e9",
	} {
		if got := wayfinder.Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{
		"Plain title", "Fix \x1b]8;;https://evil\x1b\\link\x1b]8;;\x1b\\", "\x9b2J\u0085",
		"\u202eevil\u2066\u2069", "zero\u200bwidth\u2060\ufeff", "tag\U000E0041\U000E007F", "bad \xff\xfe utf8",
		"line\u2028para\u2029", "Caf\u00e9 \u2705 \u6f22\u5b57",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		out := wayfinder.Sanitize(in)
		if !utf8.ValidString(out) {
			t.Fatalf("Sanitize(%q) = %q is not valid UTF-8", in, out)
		}
		for _, r := range out {
			if invisible(r) {
				t.Fatalf("Sanitize(%q) = %q keeps %U", in, out, r)
			}
		}
		if again := wayfinder.Sanitize(out); again != out {
			t.Fatalf("Sanitize is not idempotent: %q then %q", out, again)
		}
		// What's left is the input's text in order, less only escape
		// sequences when there are any to remove.
		var visible []rune
		for i, r := range in {
			if r == utf8.RuneError {
				if _, size := utf8.DecodeRuneInString(in[i:]); size == 1 {
					continue // an invalid byte
				}
			}
			if !invisible(r) {
				visible = append(visible, r)
			}
		}
		if !strings.ContainsAny(in, "\x1b\u0090\u0098\u009b\u009d\u009e\u009f") {
			if out != string(visible) {
				t.Fatalf("Sanitize(%q) = %q, want %q", in, out, string(visible))
			}
		} else if !subsequence([]rune(out), visible) {
			t.Fatalf("Sanitize(%q) = %q, which isn't drawn from %q", in, out, string(visible))
		}
	})
}

func TestParseEdgeCasesFixture(t *testing.T) {
	m, err := wayfinder.Parse(load(t, "map-edge-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	for s, want := range map[wayfinder.State][]int{
		wayfinder.Claimed:  {215},
		wayfinder.Done:     {216}, // even though it's assigned
		wayfinder.Frontier: {217, 218},
		wayfinder.Blocked:  {219, 220, 221, 222},
	} {
		if got := numbersIn(m, s); !slices.Equal(got, want) {
			t.Errorf("%v = %v, want %v", s, got, want)
		}
	}
	want := []string{
		"the map is closed",
		"truncated: showing 8 of 9 tickets",
		"truncated: more blockers than shown on #220",
		"blocked from another repo: #222 by other/repo#9",
		"no wayfinder:* type: #217",
		"cycle: #221, #222",
	}
	if !slices.Equal(m.Warnings, want) {
		t.Errorf("warnings =\n%q\nwant\n%q", m.Warnings, want)
	}
}
