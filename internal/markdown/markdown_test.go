package markdown_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/markdown"
)

// plain is the rendered lines without their styling.
func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func check(t *testing.T, src string, width int, want ...string) []string {
	t.Helper()
	lines := markdown.Render(src, width)
	if got := plain(lines); !slices.Equal(got, want) {
		t.Errorf("Render(%q, %d) =\n%s\nwant\n%s", src, width, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("Render(%q, %d): a line is %d cells wide: %q", src, width, w, l)
		}
	}
	return lines
}

func TestParagraphsWrapToTheWidth(t *testing.T) {
	check(t, "one two three four five\nsix", 10, "one two", "three four", "five six")
	check(t, "first\n\nsecond", 20, "first", "", "second")
	// A hard break keeps the line; a word wider than the width is broken.
	check(t, "line one  \nline two", 20, "line one", "line two")
	check(t, "abcdefghijkl", 5, "abcde", "fghij", "kl")
}

func TestHeadingsAreBold(t *testing.T) {
	lines := check(t, "# Title\n\nText\n\n### Small", 20, "Title", "", "Text", "", "Small")
	if !strings.Contains(lines[0], "\x1b[1m") || !strings.Contains(lines[4], "\x1b[1m") {
		t.Errorf("headings aren't bold: %q", lines)
	}
}

func TestInlineStyles(t *testing.T) {
	lines := check(t, "*em* **strong** ~~gone~~ `code`", 40, "em strong gone code")
	for _, want := range []string{"\x1b[3mem", "\x1b[1mstrong", "\x1b[9mgone", "\x1b[36mcode"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("lacks %q: %q", want, lines[0])
		}
	}
}

// Wrapping keeps a style on every line it covers.
func TestStylesSurviveWrapping(t *testing.T) {
	lines := check(t, "**bold words that wrap**", 10, "bold words", "that wrap")
	for _, l := range lines {
		if !strings.HasPrefix(l, "\x1b[1m") || !strings.HasSuffix(l, "\x1b[0m") {
			t.Errorf("a wrapped line isn't bold on its own: %q", l)
		}
	}
}

func TestEntitiesAndEscapesAreResolved(t *testing.T) {
	check(t, `a &amp; b &lt;c&gt; \*not em\*`, 40, "a & b <c> *not em*")
}

func TestFencedCodeKeepsItsLines(t *testing.T) {
	check(t, "```go\nfunc main() {\n\tx := 1\n}\n```", 40, "func main() {", "    x := 1", "}")
	// Code too wide for the pane is broken, not cut.
	check(t, "```\nabcdefgh\n```", 5, "abcde", "fgh")
	check(t, "    indented code", 40, "indented code")
}

func TestLists(t *testing.T) {
	check(t, "- a\n- b\n  - c\n\n1. one\n2. two", 20, "• a", "• b", "  • c", "", "1. one", "2. two")
	// A wrapped item lines up under its text.
	check(t, "- one two three", 10, "• one two", "  three")
	check(t, "3. three\n4. four", 20, "3. three", "4. four")
}

func TestTaskLists(t *testing.T) {
	check(t, "- [ ] todo\n- [x] done", 20, "☐ todo", "☑ done")
}

func TestQuotes(t *testing.T) {
	check(t, "> quoted words here", 12, "│ quoted", "│ words here")
}

func TestLinksShowTheirTextAndURL(t *testing.T) {
	lines := check(t, "see [the docs](https://example.com/docs) now", 60, "see the docs (https://example.com/docs) now")
	if !strings.Contains(lines[0], "\x1b]8;;https://example.com/docs\x07the docs") {
		t.Errorf("the link isn't an OSC 8 hyperlink: %q", lines[0])
	}
	// An autolink, or a link whose text is its URL, shows the URL once.
	check(t, "go to https://example.com now", 60, "go to https://example.com now")
	check(t, "<https://example.com>", 60, "https://example.com")
	// Only web and mail links become hyperlinks.
	lines = check(t, "[click](javascript:alert(1))", 60, "click (javascript:alert(1))")
	if strings.Contains(lines[0], "\x1b]8") {
		t.Errorf("a javascript: link became a hyperlink: %q", lines[0])
	}
}

func TestTables(t *testing.T) {
	check(t, "| a | bb |\n|---|---|\n| ccc | d |", 40, "a   │ bb", "────┼───", "ccc │ d")
}

func TestThematicBreaks(t *testing.T) {
	check(t, "above\n\n---\n\nbelow", 5, "above", "", "─────", "", "below")
}

func TestRawHTMLIsReducedToText(t *testing.T) {
	check(t, "<details><summary>More</summary>\n\nHidden text\n</details>", 40, "More", "", "Hidden text")
	check(t, "before <b>bold</b> after<br>next", 40, "before bold after", "next")
	check(t, "<!-- a comment\nover lines -->\n\nText", 40, "Text")
}

func TestImagesArePlaceholders(t *testing.T) {
	check(t, "![a screenshot](https://example.com/a.png)", 40, "🖼 a screenshot")
	check(t, `<img width="200" alt="a diagram" src="https://example.com/b.png">`, 40, "🖼 a diagram")
	check(t, `text <img src="x.png"> more`, 40, "text 🖼 image more")
}

// Nothing in a body, a link or a code block reaches the terminal as an
// escape sequence.
func TestSanitisesEveryString(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"text", "red\x1b[31m text\x1b[0m", "red text"},
		{"osc", "a\x1b]52;c;ZXZpbA==\x07b", "ab"},
		{"c1", "a\u009b31mb", "ab"},
		{"bidi", "a‮b", "ab"},
		// The ESC an entity makes is removed, but the parser has already
		// split the sequence's tail off as text.
		{"numeric reference", "a&#27;[31mb", "a[31mb"},
		{"code span", "`a\x1b[2Jb`", "ab"},
		{"link text", "[x\x1b[31my](https://example.com)", "xy (https://example.com)"},
		{"heading", "# a\x1b]0;title\x07b", "ab"},
	} {
		t.Run(c.name, func(t *testing.T) {
			lines := check(t, c.src, 60, c.want)
			for _, bad := range []string{"\x1b[31m", "\x1b[2J", "\x1b]52", "\x1b]0", "\u009b", "‮"} {
				if strings.Contains(strings.Join(lines, "\n"), bad) {
					t.Errorf("output holds %q: %q", bad, lines)
				}
			}
		})
	}
	lines := check(t, "```\nok\x1b]8;;https://evil\x07here\n\x1b[31mred\n```", 60, "okhere", "red")
	if s := strings.Join(lines, "\n"); strings.Contains(s, "evil") || strings.Contains(s, "\x1b[31m") {
		t.Errorf("a code block let an escape through: %q", lines)
	}
	lines = markdown.Render("[a](https://example.com/\x1b]8;;evil\x07)", 60)
	if s := strings.Join(lines, "\n"); strings.Contains(s, "\x1b]8;;evil") || strings.Count(s, "\x1b]8;;") > 2 {
		t.Errorf("a link URL let an escape through: %q", lines)
	}
}

func TestEmptyBody(t *testing.T) {
	if lines := markdown.Render("", 40); len(lines) != 0 {
		t.Errorf("an empty body rendered %q", lines)
	}
}
