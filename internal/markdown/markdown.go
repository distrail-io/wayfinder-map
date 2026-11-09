// Package markdown renders an issue body for the terminal. It parses
// GitHub-flavoured markdown with goldmark and turns the syntax tree into
// styled lines wrapped to a width. Every piece of text from the body is
// sanitised, so the only escape sequences in the output are the renderer's
// own styles and OSC 8 links.
package markdown

import (
	"fmt"
	"html"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/distrail-io/wayfinder-map/internal/wayfinder"
)

var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

// Render turns a markdown body into lines no wider than width. Raw HTML is
// reduced to its text, and images, in markdown or HTML, become a 🖼
// placeholder with their alt text.
func Render(src string, width int) []string {
	r := &renderer{src: []byte(presanitise(src))}
	return r.block(md.Parser().Parse(text.NewReader(r.src)), max(width, 1))
}

// Image is one of a body's images.
type Image struct {
	Alt string
	// URL is the image's signed URL from the body's HTML, or empty if the
	// body's HTML doesn't match.
	URL string
	// Width and Height are the size its <img> tag asks for, in pixels, or
	// 0 if it doesn't.
	Width, Height int
}

// Images lists a body's images in order: each ![]() and each <img>, but
// none in code. The n-th takes its URL from the n-th <img src> in the
// body's HTML, which GitHub signs so that a private repo's images load
// without a login. If the counts differ, which image is which is unknown,
// so none has a URL.
func Images(src, bodyHTML string) []Image {
	r := &renderer{src: []byte(presanitise(src))}
	r.block(md.Parser().Parse(text.NewReader(r.src)), 80)
	var urls []string
	for _, m := range tag.FindAllStringSubmatch(comment.ReplaceAllString(bodyHTML, ""), -1) {
		if strings.EqualFold(m[2], "img") {
			urls = append(urls, attr(m[3], "src"))
		}
	}
	if len(urls) == len(r.images) {
		for i := range r.images {
			r.images[i].URL = urls[i]
		}
	}
	return r.images
}

// presanitise removes escape sequences whole before parsing, which could
// otherwise split one across text nodes and leave its tail as text. It
// keeps the line breaks and tabs that markdown's structure rests on. Each
// piece of text is sanitised again once parsed, as entities can make more.
func presanitise(src string) string {
	lines := strings.Split(src, "\n")
	for i, l := range lines {
		parts := strings.Split(l, "\t")
		for k, p := range parts {
			parts[k] = wayfinder.Sanitize(p)
		}
		lines[i] = strings.Join(parts, "\t")
	}
	return strings.Join(lines, "\n")
}

type renderer struct {
	src    []byte
	images []Image // the images met so far
}

// block renders a block node in width cells.
func (r *renderer) block(n ast.Node, width int) []string {
	switch n := n.(type) {
	case *ast.Document:
		return r.children(n, width, true)
	case *ast.Paragraph, *ast.TextBlock:
		return wrap(r.inlines(n, style{}), width)
	case *ast.Heading:
		return wrap(r.inlines(n, style{bold: true}), width)
	case *ast.ThematicBreak:
		return []string{strings.Repeat("─", width)}
	case *ast.CodeBlock, *ast.FencedCodeBlock:
		return r.code(n, width)
	case *ast.Blockquote:
		lines := r.children(n, max(width-2, 1), true)
		for i, l := range lines {
			lines[i] = strings.TrimRight("│ "+l, " ")
		}
		return lines
	case *ast.List:
		return r.list(n, width)
	case *ast.HTMLBlock:
		var b strings.Builder
		for i := range n.Lines().Len() {
			seg := n.Lines().At(i)
			b.Write(seg.Value(r.src))
		}
		if n.HasClosure() {
			b.Write(n.ClosureLine.Value(r.src))
		}
		// Each of its lines wraps on its own.
		var lines []string
		var line []span
		for _, s := range append(r.reduceHTML(b.String(), style{}), span{br: true}) {
			if !s.br {
				line = append(line, s)
				continue
			}
			lines = append(lines, wrap(line, width)...)
			line = nil
		}
		return lines
	case *east.Table:
		return r.table(n, width)
	case *ast.LinkReferenceDefinition:
		return nil
	}
	return r.children(n, width, true)
}

// children renders n's child blocks one after another, with a blank line
// between them if spaced. A child that renders nothing takes no space.
func (r *renderer) children(n ast.Node, width int, spaced bool) []string {
	var lines []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out := r.block(c, width)
		if len(out) == 0 {
			continue
		}
		if spaced && len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, out...)
	}
	return lines
}

// list renders each item after its bullet, number or task box, with the
// lines it wraps onto lined up under its text. A loose list has a blank
// line between items.
func (r *renderer) list(n *ast.List, width int) []string {
	var lines []string
	num := n.Start
	for item := n.FirstChild(); item != nil; item = item.NextSibling() {
		bullet := "• "
		if n.IsOrdered() {
			bullet = fmt.Sprintf("%d. ", num)
			num++
		}
		if box := taskBox(item); box != "" {
			bullet = box + " "
			if n.IsOrdered() {
				bullet = fmt.Sprintf("%d. %s ", num-1, box)
			}
		}
		pad := ansi.StringWidth(bullet)
		body := r.children(item, max(width-pad, 1), !n.IsTight)
		if len(body) == 0 {
			body = []string{""}
		}
		if !n.IsTight && len(lines) > 0 {
			lines = append(lines, "")
		}
		for i, l := range body {
			prefix := strings.Repeat(" ", pad)
			if i == 0 {
				prefix = bullet
			}
			lines = append(lines, strings.TrimRight(prefix+l, " "))
		}
	}
	return lines
}

// taskBox is ☐ or ☑ if the list item starts with a task checkbox.
func taskBox(item ast.Node) string {
	if c := item.FirstChild(); c != nil {
		if box, ok := c.FirstChild().(*east.TaskCheckBox); ok {
			if box.IsChecked {
				return "☑"
			}
			return "☐"
		}
	}
	return ""
}

// code renders a code block's lines as they are, broken where they are
// wider than width, with tabs as four spaces.
func (r *renderer) code(n ast.Node, width int) []string {
	var lines []string
	for i := range n.Lines().Len() {
		seg := n.Lines().At(i)
		l := strings.TrimRight(string(seg.Value(r.src)), "\r\n")
		l = wayfinder.Sanitize(strings.ReplaceAll(l, "\t", "    "))
		if l == "" {
			lines = append(lines, "")
			continue
		}
		for l != "" {
			head := ansi.Truncate(l, width, "")
			if head == "" { // a character wider than the pane
				head = string([]rune(l)[:1])
			}
			lines = append(lines, render([]frag{{head, style{code: true}}}))
			l = l[len(head):]
		}
	}
	return lines
}

// table renders a table's cells as plain text in aligned columns, with a
// rule under the header. A row too wide for the pane is cut.
func (r *renderer) table(n *east.Table, width int) []string {
	var rows [][]string
	var widths []int
	for row := n.FirstChild(); row != nil; row = row.NextSibling() {
		var cells []string
		for i, cell := 0, row.FirstChild(); cell != nil; i, cell = i+1, cell.NextSibling() {
			var b strings.Builder
			for _, s := range r.inlines(cell, style{}) {
				b.WriteString(s.text)
			}
			c := strings.TrimSpace(b.String())
			cells = append(cells, c)
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], ansi.StringWidth(c))
		}
		rows = append(rows, cells)
	}
	var lines []string
	line := func(cells []string, sep string) {
		var b strings.Builder
		for i, c := range cells {
			if i > 0 {
				b.WriteString(sep)
			}
			b.WriteString(c + strings.Repeat(" ", widths[i]-ansi.StringWidth(c)))
		}
		lines = append(lines, ansi.Truncate(strings.TrimRight(b.String(), " "), width, "…"))
	}
	for i, cells := range rows {
		line(cells, " │ ")
		if i == 0 {
			rule := make([]string, len(widths))
			for k, w := range widths {
				rule[k] = strings.Repeat("─", w)
			}
			line(rule, "─┼─")
		}
	}
	return lines
}

// style is how a run of text is shown.
type style struct {
	bold, italic, underline, strike, code bool
	link                                  string // an OSC 8 target, if any
}

// span is a run of text in one style, or a hard line break.
type span struct {
	text string
	st   style
	br   bool
}

// inlines collects n's inline children as spans in style st.
func (r *renderer) inlines(n ast.Node, st style) []span {
	var out []span
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		out = append(out, r.inline(c, st)...)
	}
	return out
}

func (r *renderer) inline(n ast.Node, st style) []span {
	switch n := n.(type) {
	case *ast.Text:
		v := n.Value(r.src)
		if !n.IsRaw() {
			v = util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(v)))
		}
		out := []span{{text: clean(string(v)), st: st}}
		switch {
		case n.HardLineBreak():
			out = append(out, span{br: true})
		case n.SoftLineBreak():
			out = append(out, span{text: " ", st: st})
		}
		return out
	case *ast.String:
		return []span{{text: clean(string(n.Value)), st: st}}
	case *ast.CodeSpan:
		st.code = true
		var b strings.Builder
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if t, ok := c.(*ast.Text); ok {
				b.Write(t.Value(r.src))
			}
		}
		return []span{{text: clean(strings.ReplaceAll(b.String(), "\n", " ")), st: st}}
	case *ast.Emphasis:
		if n.Level >= 2 {
			st.bold = true
		} else {
			st.italic = true
		}
		return r.inlines(n, st)
	case *east.Strikethrough:
		st.strike = true
		return r.inlines(n, st)
	case *ast.Link:
		dest := clean(string(n.Destination))
		st.underline, st.link = true, safeURL(dest)
		out := r.inlines(n, st)
		var label strings.Builder
		for _, s := range out {
			label.WriteString(s.text)
		}
		if dest != "" && label.String() != dest {
			out = append(out, span{text: " (" + dest + ")"})
		}
		return out
	case *ast.AutoLink:
		label := clean(string(n.Label(r.src)))
		st.underline, st.link = true, safeURL(clean(string(n.URL(r.src))))
		return []span{{text: label, st: st}}
	case *ast.Image:
		var alt strings.Builder
		for _, s := range r.inlines(n, style{}) {
			alt.WriteString(s.text)
		}
		return []span{r.image(Image{Alt: alt.String()}, st)}
	case *ast.RawHTML:
		var b strings.Builder
		for i := range n.Segments.Len() {
			seg := n.Segments.At(i)
			b.Write(seg.Value(r.src))
		}
		return r.reduceHTML(b.String(), st)
	case *east.TaskCheckBox:
		return nil // the list item shows it in place of its bullet
	}
	return r.inlines(n, st)
}

// clean sanitises text from the body, keeping tabs as spaces.
func clean(s string) string { return wayfinder.Sanitize(strings.ReplaceAll(s, "\t", " ")) }

// safeURL is url if it is a web or mail link the terminal may open, else
// empty: a link to anything else shows as text only.
func safeURL(url string) string {
	l := strings.ToLower(url)
	if strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "mailto:") {
		return url
	}
	return ""
}

// image is the body's next image, shown as a 🖼 placeholder with its alt
// text.
func (r *renderer) image(im Image, st style) span {
	im.Alt = strings.TrimSpace(im.Alt)
	r.images = append(r.images, im)
	alt := im.Alt
	if alt == "" {
		alt = "image"
	}
	return span{text: "🖼 " + alt, st: st}
}

var (
	comment = regexp.MustCompile(`(?s)<!--.*?(-->|$)`)
	tag     = regexp.MustCompile(`(?s)<(/?)([a-zA-Z][a-zA-Z0-9-]*)([^>]*)>`)
	attrs   = regexp.MustCompile(`(?i)(?:^|\s)([a-z][a-z0-9-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)
)

// attr is the value of a tag's attribute, unescaped, or empty.
func attr(tagAttrs, name string) string {
	for _, a := range attrs.FindAllStringSubmatch(tagAttrs, -1) {
		if strings.EqualFold(a[1], name) {
			return html.UnescapeString(a[2] + a[3] + a[4])
		}
	}
	return ""
}

// size is a tag's attribute as a whole number of pixels, or 0.
func size(tagAttrs, name string) int {
	n, err := strconv.Atoi(strings.TrimSpace(attr(tagAttrs, name)))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// lineTags are the HTML elements whose ends start a new line.
var lineTags = map[string]bool{"br": true, "p": true, "div": true, "li": true, "tr": true, "summary": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "details": true, "table": true}

// reduceHTML reduces raw HTML to spans of its sanitised text in style st:
// comments and tags go, an <img> becomes an image, and line breaks, and the
// elements that end a line, end one.
func (r *renderer) reduceHTML(s string, st style) []span {
	s = comment.ReplaceAllString(s, "")
	var out []span
	text := func(t string) {
		for i, l := range strings.Split(html.UnescapeString(t), "\n") {
			if i > 0 {
				out = append(out, span{br: true})
			}
			out = append(out, span{text: clean(l), st: st})
		}
	}
	at := 0
	for _, m := range tag.FindAllStringSubmatchIndex(s, -1) {
		text(s[at:m[0]])
		at = m[1]
		name, a := strings.ToLower(s[m[4]:m[5]]), s[m[6]:m[7]]
		switch {
		case name == "img":
			out = append(out, r.image(Image{Alt: clean(attr(a, "alt")), Width: size(a, "width"), Height: size(a, "height")}, st))
		case name == "br" || (m[3] > m[2] && lineTags[name]):
			out = append(out, span{br: true})
		}
	}
	text(s[at:])
	return out
}

// frag is a run of a word in one style.
type frag struct {
	text string
	st   style
}

// wrap fills lines up to width with the spans' words, breaking at spaces
// and hard breaks, and breaking a word only if it is wider than a line.
func wrap(spans []span, width int) []string {
	var lines []string
	var line []frag
	w := 0
	emit := func() {
		lines = append(lines, render(line))
		line, w = nil, 0
	}
	for _, wd := range words(spans) {
		if wd == nil {
			emit()
			continue
		}
		ww := widthOf(wd)
		if w > 0 && w+1+ww <= width {
			sp := style{}
			if last := line[len(line)-1].st; last == wd[0].st {
				sp = last
			}
			line = append(append(line, frag{" ", sp}), wd...)
			w += 1 + ww
			continue
		}
		if w > 0 {
			emit()
		}
		for ww > width {
			var head []frag
			head, wd = cut(wd, width)
			line = head
			emit()
			ww = widthOf(wd)
		}
		line, w = wd, ww
	}
	if w > 0 {
		emit()
	}
	return lines
}

// words splits spans into words at spaces, keeping each fragment's style. A
// nil word is a hard line break.
func words(spans []span) [][]frag {
	var out [][]frag
	var cur []frag
	flush := func() {
		if len(cur) > 0 {
			out = append(out, cur)
			cur = nil
		}
	}
	for _, s := range spans {
		if s.br {
			flush()
			out = append(out, nil)
			continue
		}
		for i, part := range strings.Split(s.text, " ") {
			if i > 0 {
				flush()
			}
			if part != "" {
				cur = append(cur, frag{part, s.st})
			}
		}
	}
	flush()
	return out
}

func widthOf(fs []frag) int {
	w := 0
	for _, f := range fs {
		w += ansi.StringWidth(f.text)
	}
	return w
}

// cut splits a word into its first width cells and the rest. It takes at
// least one character, so a character wider than width still moves on.
func cut(wd []frag, width int) (head, tail []frag) {
	w := 0
	for i, f := range wd {
		fw := ansi.StringWidth(f.text)
		if w+fw <= width {
			head, w = append(head, f), w+fw
			continue
		}
		h := ansi.Truncate(f.text, width-w, "")
		if h == "" && len(head) == 0 {
			h = string([]rune(f.text)[:1])
		}
		if h != "" {
			head = append(head, frag{h, f.st})
		}
		tail = append([]frag{{f.text[len(h):], f.st}}, wd[i+1:]...)
		return head, tail
	}
	return head, nil
}

// render writes a line's fragments with their styles, merging neighbours
// in the same style. Each styled run closes itself, so every line stands
// alone.
func render(line []frag) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		st := line[i].st
		var run strings.Builder
		for ; i < len(line) && line[i].st == st; i++ {
			run.WriteString(line[i].text)
		}
		if st == (style{}) {
			b.WriteString(run.String())
			continue
		}
		for _, on := range []struct {
			set  bool
			code string
		}{{st.bold, "1"}, {st.italic, "3"}, {st.underline, "4"}, {st.strike, "9"}, {st.code, "36"}} {
			if on.set {
				b.WriteString("\x1b[" + on.code + "m")
			}
		}
		if st.link != "" {
			b.WriteString(ansi.SetHyperlink(st.link) + run.String() + ansi.ResetHyperlink())
		} else {
			b.WriteString(run.String())
		}
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
