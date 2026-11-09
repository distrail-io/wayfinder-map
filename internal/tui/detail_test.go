package tui_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/tui"
)

// ask is map #214 in banked/ask-barry with the detail fixtures, sized.
func ask(t *testing.T, mapFixture string, cfg tui.Config) (*app, *fakeSource) {
	t.Helper()
	src := &fakeSource{maps: map[int][]byte{214: fixture(t, mapFixture)}, issues: map[string][]byte{
		"banked/ask-barry#215": fixture(t, "issue-215.json"),
		"banked/ask-barry#222": fixture(t, "issue-222.json"),
		"other/repo#9":         fixture(t, "issue-other-repo-9.json"),
	}}
	cfg.Number = 214
	return startWith(t, src, cfg).sized(), src
}

// issueJSON is issue n in a/b with a title and body, blocking nothing.
func issueJSON(n int, title, body string) []byte {
	ti, _ := json.Marshal(title)
	b, _ := json.Marshal(body)
	return fmt.Appendf(nil, `{"data":{"repository":{"nameWithOwner":"a/b","issue":{"number":%d,"title":%s,"state":"OPEN",
		"body":%s,"labels":{"nodes":[]},"assignees":{"nodes":[]},"subIssuesSummary":{"total":0,"completed":0},
		"subIssues":{"nodes":[]},"blockedBy":{"nodes":[]},"blocking":{"nodes":[]}}}}}`, n, ti, b)
}

// lines is the view without styling, a line each.
func (a *app) lines() []string { return strings.Split(ansi.Strip(a.view()), "\n") }

// has reports whether some line of the view, unstyled, contains s.
func (a *app) has(s string) bool {
	return slices.ContainsFunc(a.lines(), func(l string) bool { return strings.Contains(l, s) })
}

// row is the index of the first line of the view that contains s, or -1.
func (a *app) row(s string) int {
	return slices.IndexFunc(a.lines(), func(l string) bool { return strings.Contains(l, s) })
}

// tabTo moves the detail view's cursor to the related issue whose row
// contains s.
func (a *app) tabTo(s string) {
	a.t.Helper()
	for range 30 {
		a.key("tab")
		if l := a.row("› "); l >= 0 && strings.Contains(a.lines()[l], s) {
			return
		}
	}
	a.t.Fatalf("tab never reached %q:\n%s", s, a.view())
}

func TestEnterAndSpaceOpenTheDetailView(t *testing.T) {
	for _, k := range []string{"enter", "space"} {
		t.Run(k, func(t *testing.T) {
			a, src := ask(t, "map-214.json", tui.Config{})
			raw := a.key(k)
			// The map image would cover the view, so it goes.
			if !slices.Contains(raw, "\x1b_Ga=d,d=A,q=2\x1b\\") {
				t.Errorf("%s didn't clear the map image: %q", k, raw)
			}
			if !a.has("#215 Where does standalone Sandcastle live") || !a.has("Questions") {
				t.Errorf("%s didn't open #215's detail view:\n%s", k, a.view())
			}
			if !slices.Equal(src.issueLoaded, []string{"banked/ask-barry#215"}) {
				t.Errorf("loaded issues %v, want #215 once", src.issueLoaded)
			}
			if raw := a.wait(time.Second); placements(raw) != 0 {
				t.Errorf("placed the map over the detail view")
			}
			if len(a.opened) != 0 {
				t.Errorf("%s opened the browser: %v", k, a.opened)
			}
		})
	}
}

// The parts the map knows show at once; the rest says it is loading until
// the issue arrives.
func TestTheDetailViewPaintsFromTheMapThenFillsIn(t *testing.T) {
	a, _ := ask(t, "map-214.json", tui.Config{})
	var load tea.Cmd
	a.m, load = a.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for _, want := range []string{
		"#215 Where does standalone Sandcastle live",
		"frontier · grilling · unassigned",
		"in map #214 Wayfinder: run Sandcastle standalone",
		"Blocks",
		"#220 Pilot: run Sandcastle against a second Banked repo · blocked",
		"#221 Move Sandcastle into its own home · blocked",
		"labels: loading…",
	} {
		if !a.has(want) {
			t.Errorf("first paint lacks %q:\n%s", want, a.view())
		}
	}
	if strings.Count(ansi.Strip(a.view()), "loading…") < 3 {
		t.Errorf("labels, sub-issues and body should say loading…:\n%s", a.view())
	}
	for _, msg := range run(load) {
		a.send(msg)
	}
	if a.has("loading…") {
		t.Errorf("still loading once the issue arrived:\n%s", a.view())
	}
	if !a.has("labels: wayfinder:grilling, needs-decision") {
		t.Errorf("no labels once loaded:\n%s", a.view())
	}
}

func TestTheBodyIsRenderedFromMarkdown(t *testing.T) {
	a, _ := ask(t, "map-214.json", tui.Config{})
	a.key("enter")
	for _, want := range []string{
		"Questions",
		"☑ Where does the code live?",
		"☐ How is it distributed?",
		"See the Sandcastle notes (https://github.com/banked/ask-barry/wiki/Sandcastle) first.",
		"sandcastle run --repo banked/payments",
		"🖼 pilot diagram",
	} {
		if !a.has(want) {
			t.Errorf("body lacks %q:\n%s", want, a.view())
		}
	}
	if !strings.Contains(a.view(), "\x1b]8;;https://github.com/banked/ask-barry/wiki/Sandcastle\x07") {
		t.Errorf("the link isn't a hyperlink:\n%q", a.view())
	}
	for _, l := range strings.Split(a.view(), "\n") {
		if w := ansi.StringWidth(l); w > 100 {
			t.Errorf("a line is %d cells wide in a 100-cell pane: %q", w, l)
		}
	}
}

// Blockers the map doesn't draw, from another repo or not on the map, are
// listed apart, so a blocked ticket never looks unblocked.
func TestRelationshipsIncludeBlockersTheMapDoesNotDraw(t *testing.T) {
	a, _ := ask(t, "map-edge-cases.json", tui.Config{})
	for a.selected() != 222 {
		a.key("tab")
	}
	var load tea.Cmd
	a.m, load = a.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	// From the map: the blocker in another repo, without its title.
	if !a.has("Also blocked by") || !a.has("other/repo#9") {
		t.Errorf("first paint lacks the blocker from another repo:\n%s", a.view())
	}
	for _, msg := range run(load) {
		a.send(msg)
	}
	v := a.lines()
	at := func(s string) int { return slices.IndexFunc(v, func(l string) bool { return strings.Contains(l, s) }) }
	for _, want := range []string{
		"#222 Switch ask-barry to the standalone Sandcastle",
		"Blocked by",
		"#221 Move Sandcastle into its own home · blocked",
		"Also blocked by",
		"other/repo#9 Publish the sandbox base image · open",
		"#88 Agree the image registry · closed, not on map",
		"Sub-issues · 1/2 done",
		"#301 Remove the vendored copy · closed",
		"#302 Point CI at the standalone image · open",
		"Swap the vendored copy for the standalone one.",
	} {
		if at(want) < 0 {
			t.Errorf("view lacks %q:\n%s", want, a.view())
		}
	}
	if at("Blocked by") > at("#221 Move") || at("#221 Move") > at("Also blocked by") || at("Also blocked by") > at("other/repo#9") {
		t.Errorf("relationships out of order:\n%s", a.view())
	}
}

// The map issue's detail view lists every ticket, from the map, at once.
func TestMOpensTheMapIssue(t *testing.T) {
	a, src := ask(t, "map-214.json", tui.Config{})
	a.key("m")
	if !a.has("#214 Wayfinder: run Sandcastle standalone against any Banked repo") || !a.has("Sub-issues · 0/8 done") {
		t.Errorf("m didn't open the map issue:\n%s", a.view())
	}
	for n := 215; n <= 222; n++ {
		if !a.has(fmt.Sprintf("  #%d ", n)) {
			t.Errorf("the map issue doesn't list #%d:\n%s", n, a.view())
		}
	}
	if !slices.Equal(src.issueLoaded, []string{"banked/ask-barry#214"}) {
		t.Errorf("loaded %v, want the map issue", src.issueLoaded)
	}
}

func TestAFailedFetchShowsAnErrorInPlaceOfTheBody(t *testing.T) {
	a, src := ask(t, "map-214.json", tui.Config{})
	src.issueErr = errors.New("gh: network down")
	a.key("enter")
	if !a.has("error: gh: network down") || a.has("loading…") {
		t.Errorf("no error in place of the body:\n%s", a.view())
	}
	if !a.has("#215 Where does") || !a.has("#220 Pilot") {
		t.Errorf("the parts from the map went:\n%s", a.view())
	}
	// A good refresh clears it.
	src.issueErr = nil
	a.key("r")
	if a.has("network down") || !a.has("Questions") {
		t.Errorf("r didn't recover:\n%s", a.view())
	}
}

func TestTheDetailViewRefreshesOnThePollAndR(t *testing.T) {
	a, src := ask(t, "map-214.json", tui.Config{})
	a.key("enter")
	a.key("r")
	if len(src.issueLoaded) != 2 {
		t.Fatalf("r loaded the issue %d times in all, want 2", len(src.issueLoaded))
	}
	src.issues["banked/ask-barry#215"] = issueJSON(215, "Renamed", "A new body")
	a.wait(30 * time.Second)
	if len(src.issueLoaded) != 3 || !a.has("A new body") {
		t.Errorf("the poll didn't refresh the issue (%d loads):\n%s", len(src.issueLoaded), a.view())
	}
	if raw := a.wait(30 * time.Second); placements(raw) != 0 {
		t.Errorf("a poll placed the map over the detail view")
	}
}

// Tab moves a cursor through the related issues and Enter re-centres the
// view on it, even across repos; Esc and b step back, then to the map.
func TestWalkingRelatedIssuesAndBack(t *testing.T) {
	a, src := ask(t, "map-edge-cases.json", tui.Config{})
	for a.selected() != 222 {
		a.key("tab")
	}
	a.key("enter")
	a.tabTo("other/repo#9")
	a.key("enter")
	if !a.has("other/repo#9 Publish the sandbox base image") || !a.has("assigned to hubot") || !a.has("Push it to the shared registry.") {
		t.Fatalf("enter didn't open other/repo#9:\n%s", a.view())
	}
	a.key("o")
	if want := []string{"https://github.com/other/repo/issues/9"}; !slices.Equal(a.opened, want) {
		t.Errorf("o opened %q, want %q", a.opened, want)
	}
	// Shift+tab goes backwards, round to the last related issue.
	a.key("shift+tab")
	if l := a.row("› "); l < 0 || !strings.Contains(a.lines()[l], "#222") {
		t.Errorf("shift+tab didn't reach #222:\n%s", a.view())
	}
	a.key("esc")
	if !a.has("#222 Switch ask-barry") {
		t.Fatalf("esc didn't step back to #222:\n%s", a.view())
	}
	a.tabTo("#221 Move")
	a.key("enter")
	a.key("b")
	a.key("b")
	if raw := a.wait(time.Second); placements(raw) != 1 || a.selected() != 222 {
		t.Errorf("b didn't return to the map with #222 selected (%d placements):\n%s", placements(raw), a.view())
	}
	// Stepping back loads the issue again, as it may have changed.
	want := []string{"banked/ask-barry#222", "other/repo#9", "banked/ask-barry#222", "banked/ask-barry#221", "banked/ask-barry#222"}
	if !slices.Equal(src.issueLoaded, want) {
		t.Errorf("loaded %v, want %v", src.issueLoaded, want)
	}
	// Esc on the map still goes back to the picker.
	a.key("esc")
	if src.lists != 1 {
		t.Errorf("esc on the map didn't go to the picker")
	}
}

// An issue that arrives after moving on is dropped.
func TestALateIssueIsDropped(t *testing.T) {
	a, _ := ask(t, "map-214.json", tui.Config{})
	var load tea.Cmd
	a.m, load = a.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.tabTo("#220")
	a.key("enter")
	for _, msg := range run(load) {
		a.send(msg)
	}
	if a.has("Questions") || !a.has("#220 Pilot") {
		t.Errorf("#215's late reply was shown over #220:\n%s", a.view())
	}
}

func TestScrollingTheDetailView(t *testing.T) {
	var body []string
	for i := 1; i <= 100; i++ {
		body = append(body, fmt.Sprintf("Paragraph %d.", i))
	}
	src := &fakeSource{maps: map[int][]byte{1: chain()}, issues: map[string][]byte{"a/b#2": issueJSON(2, "Two", strings.Join(body, "\n\n"))}}
	a := start(t, src, 1)
	a.send(tea.WindowSizeMsg{Width: 60, Height: 20})
	a.key("right")
	a.key("enter")
	if a.row("#2 Two") != 0 || a.has("Paragraph 50.") {
		t.Fatalf("the view doesn't start at the top:\n%s", a.view())
	}
	a.key("j")
	a.key("down")
	if a.row("#2 Two") >= 0 {
		t.Errorf("j and down didn't scroll:\n%s", a.view())
	}
	a.key("k")
	a.key("up")
	if a.row("#2 Two") != 0 {
		t.Errorf("k and up didn't scroll back:\n%s", a.view())
	}
	a.key("G")
	if !a.has("Paragraph 100.") || a.row("Paragraph 100.") != 18 {
		t.Errorf("G didn't show the end on the last row before the status line:\n%s", a.view())
	}
	a.key("j")
	if a.row("Paragraph 100.") != 18 {
		t.Errorf("scrolled past the end:\n%s", a.view())
	}
	a.key("g")
	if a.row("#2 Two") != 0 {
		t.Errorf("g didn't go to the top:\n%s", a.view())
	}
	// A page is the view's height, less a line kept for context.
	a.key("pgdown")
	afterPage := a.view()
	a.key("pgup")
	if a.row("#2 Two") != 0 {
		t.Errorf("pgup didn't go back a page:\n%s", a.view())
	}
	a.key("space")
	if a.view() != afterPage || a.row("#2 Two") >= 0 {
		t.Errorf("pgdown and space don't scroll the same page:\n%s", a.view())
	}
	if lines := len(a.lines()); lines != 20 {
		t.Errorf("the detail view is %d lines high, want 20", lines)
	}
}

// c switches the look shared by the map and the detail view.
func TestCInTheDetailViewSwitchesTheLook(t *testing.T) {
	a, _ := ask(t, "map-214.json", tui.Config{})
	a.key("enter")
	a.key("c")
	if !a.has("c look: compact") {
		t.Errorf("the detail view's hints don't show the look:\n%s", a.view())
	}
	a.key("esc")
	if raw := a.wait(time.Second); placements(raw) != 1 || !strings.Contains(a.strip()[0], "c look: compact") {
		t.Errorf("the map didn't come back compact:\n%s", a.view())
	}
}

// List mode has the same detail view and writes no Kitty escapes.
func TestTheDetailViewInListMode(t *testing.T) {
	a, src := ask(t, "map-edge-cases.json", tui.Config{List: true})
	var raw []string
	for _, k := range []string{"down", "enter", "tab", "enter", "esc", "esc", "m", "r", "esc"} {
		raw = append(raw, a.key(k)...)
		raw = append(raw, a.wait(time.Second)...)
	}
	if strings.Contains(strings.Join(raw, ""), "\x1b_G") {
		t.Errorf("wrote Kitty escapes: %q", raw)
	}
	// #218, its parent the map issue, #218 again on the way back, then the
	// map issue by m and again by r.
	want := []string{"banked/ask-barry#218", "banked/ask-barry#214", "banked/ask-barry#218", "banked/ask-barry#214", "banked/ask-barry#214"}
	if !slices.Equal(src.issueLoaded, want) {
		t.Errorf("loaded %v, want %v", src.issueLoaded, want)
	}
	if a.selected() != 218 {
		t.Errorf("back on the list, #%d is selected, want #218", a.selected())
	}
}

// No string from GitHub reaches the terminal with its escape sequences.
func TestTheDetailViewIsSanitised(t *testing.T) {
	src := &fakeSource{maps: map[int][]byte{1: chain()}, issues: map[string][]byte{"a/b#2": issueJSON(2,
		"T\x1b]52;c;ZXZpbA==\x07wo", "body \x1b[2J here\n\n`code \x1b]0;title\x07`\n\n[li\x1b[5mnk](https://example.com)")}}
	a := start(t, src, 1).sized()
	a.key("right")
	a.key("enter")
	v := a.view()
	for _, bad := range []string{"\x1b]52", "\x1b[2J", "\x1b]0;", "\x1b[5m"} {
		if strings.Contains(v, bad) {
			t.Errorf("the view holds %q:\n%q", bad, v)
		}
	}
	for _, want := range []string{"#2 Two", "body here", "code", "link (https://example.com)"} {
		if !a.has(want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

// A sub-issue off the map shows whether it is claimed, and its type, as
// the issue's query brings its assignees and labels.
func TestSubIssuesOffTheMapShowClaimedAndTheirType(t *testing.T) {
	a, src := ask(t, "map-edge-cases.json", tui.Config{})
	src.issues["banked/ask-barry#222"] = []byte(strings.Replace(string(fixture(t, "issue-222.json")),
		`"title":"Point CI at the standalone image","state":"OPEN","repository":{"nameWithOwner":"banked/ask-barry"},"assignees":{"nodes":[]},"labels":{"nodes":[]}`,
		`"title":"Point CI at the standalone image","state":"OPEN","repository":{"nameWithOwner":"banked/ask-barry"},"assignees":{"nodes":[{"login":"hubot"}]},"labels":{"nodes":[{"name":"wayfinder:task"}]}`, 1))
	for a.selected() != 222 {
		a.key("tab")
	}
	a.key("enter")
	if !a.has("#302 Point CI at the standalone image · claimed · task") || !a.has("#301 Remove the vendored copy · closed") {
		t.Errorf("the sub-issues don't show their state and type:\n%s", a.view())
	}
}

// A ticket closed as not planned, or as a duplicate, says so once the
// issue arrives; one closed as completed is just done.
func TestTheHeaderSaysWhyATicketWasClosed(t *testing.T) {
	for reason, want := range map[string]string{
		"NOT_PLANNED": "done · not planned · task · assigned to hubot",
		"DUPLICATE":   "done · duplicate · task · assigned to hubot",
		"COMPLETED":   "done · task · assigned to hubot",
	} {
		t.Run(reason, func(t *testing.T) {
			a, src := ask(t, "map-edge-cases.json", tui.Config{})
			src.issues["banked/ask-barry#216"] = fmt.Appendf(nil, `{"data":{"repository":{"nameWithOwner":"banked/ask-barry","issue":{
				"number":216,"title":"Sandcastle","state":"CLOSED","stateReason":%q,"body":"","labels":{"nodes":[{"name":"wayfinder:task"}]},
				"assignees":{"nodes":[{"login":"hubot"}]},"subIssuesSummary":{"total":0,"completed":0},"subIssues":{"nodes":[]},
				"blockedBy":{"nodes":[]},"blocking":{"nodes":[]}}}}}`, reason)
			for a.selected() != 216 {
				a.key("tab")
			}
			a.key("enter")
			if a.lines()[1] != want {
				t.Errorf("the header's facts are %q, want %q", a.lines()[1], want)
			}
		})
	}
}

// The status line drops hints a pane has no room for, the least needed
// first, but always says how to go back and quit.
func TestTheDetailStatusLineFitsThePane(t *testing.T) {
	a, _ := images(t, "map-edge-cases.json")
	for a.selected() != 222 {
		a.key("tab")
	}
	a.key("enter")
	a.tabTo("other/repo#9")
	a.key("enter")
	a.key("c")
	status := func() string { l := a.lines(); return l[len(l)-1] }
	for _, cols := range []int{140, 100, 60, 40} {
		a.send(tea.WindowSizeMsg{Width: cols, Height: 30})
		s := status()
		if w := ansi.StringWidth(s); w > cols || strings.Contains(s, "…") {
			t.Errorf("at %d columns the status line is %d wide, or cut: %q", cols, w, s)
		}
		for _, want := range []string{"other/repo#9", "b back", "q quit"} {
			if !strings.Contains(s, want) {
				t.Errorf("at %d columns the status line lacks %q: %q", cols, want, s)
			}
		}
		if cols == 140 && !strings.Contains(s, "tab select · enter open · b back · o browser · c look: compact · d dir: LR · r refresh · q quit") {
			t.Errorf("a wide pane dropped hints: %q", s)
		}
	}
}

// offMap is map #1 in a/b with one ticket, #7, assigned to who, if anyone,
// and blocked only by #9 in repo, which isn't on the map.
func offMap(repo, who string) []byte {
	r, _ := json.Marshal(repo)
	assignees := ""
	if who != "" {
		w, _ := json.Marshal(who)
		assignees = fmt.Sprintf(`{"login":%s}`, w)
	}
	return fmt.Appendf(nil, `{"data":{"repository":{"nameWithOwner":"a/b","issue":{"number":1,"title":"Map","state":"OPEN",
		"subIssuesSummary":{"total":1},"subIssues":{"pageInfo":{"hasNextPage":false},"nodes":[{"number":7,"title":"Seven",
		"state":"OPEN","labels":{"nodes":[{"name":"wayfinder:task"}]},"assignees":{"nodes":[%s]},
		"blockedBy":{"pageInfo":{"hasNextPage":false},"nodes":[{"number":9,"state":"OPEN","repository":{"nameWithOwner":%s}}]}}]}}}}}`,
		assignees, r)
}

// Repo names and logins come from GitHub too, so they are sanitised like
// titles: from the map before the issue loads, and from the issue after.
func TestRepoNamesAndLoginsAreSanitised(t *testing.T) {
	const evil, who = "oth\x1b]0;x\x07er/repo", "oct\x1b[2Jocat"
	src := &fakeSource{maps: map[int][]byte{1: offMap(evil, who)}, issues: map[string][]byte{}}
	a := start(t, src, 1).sized()
	var load tea.Cmd
	a.m, load = a.m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	check := func(when string, want ...string) {
		t.Helper()
		v := a.view()
		for _, bad := range []string{"\x1b]0;", "\x1b[2J"} {
			if strings.Contains(v, bad) {
				t.Errorf("%s, the view holds %q:\n%q", when, bad, v)
			}
		}
		for _, w := range want {
			if !a.has(w) {
				t.Errorf("%s, the view lacks %q:\n%s", when, w, v)
			}
		}
	}
	check("before the issue loads", "assigned to octocat", "other/repo#9")
	r, _ := json.Marshal(evil)
	w, _ := json.Marshal(who)
	related := fmt.Sprintf(`{"number":9,"title":"Nine","state":"OPEN","repository":{"nameWithOwner":%s}}`, r)
	src.issues["a/b#7"] = fmt.Appendf(nil, `{"data":{"repository":{"nameWithOwner":%s,"issue":{"number":7,"title":"Seven","state":"OPEN",
		"body":"","labels":{"nodes":[]},"assignees":{"nodes":[{"login":%s}]},"parent":%s,"subIssuesSummary":{"total":1,"completed":0},
		"subIssues":{"nodes":[%s]},"blockedBy":{"nodes":[%s]},"blocking":{"nodes":[%s]}}}}}`, r, w, related, related, related, related)
	for _, msg := range run(load) {
		a.send(msg)
	}
	check("once the issue loads", "assigned to octocat", "in other/repo#9 Nine", "other/repo#9 Nine · open")
}
