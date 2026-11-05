package main

import (
	"bytes"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/mattn/go-runewidth"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
	"golang.org/x/term"
)

var color = os.Getenv("NO_COLOR") == "" && (term.IsTerminal(int(os.Stdout.Fd())) || os.Getenv("Q_COLOR") == "1")

func sgr(open, end string) func(string) string {
	return func(t string) string {
		if !color {
			return t
		}
		return "\x1b[" + open + "m" + t + "\x1b[" + end + "m"
	}
}

var displayMath = regexp.MustCompile(`(?s)^\{\\displaystyle\s*(.*)\}$`)

var (
	bold    = sgr("1", "22")
	dim     = sgr("2", "22")
	italic  = sgr("3", "23")
	under   = sgr("4", "24")
	cyan    = sgr("36", "39")
	heading = sgr("1;34", "22;39")
	ansi    = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;;[^\x07]*\x07")
	pieces  = regexp.MustCompile(`\n|[ \t\r\f\v]+|[^ \t\r\f\v\n]+`)
)

var allowedEscape = regexp.MustCompile("\x1b\\[[0-9;]*m|\x1b\\]8;;[^\x07\x1b]*\x07")

func stripControl(s string) string {
	return strings.Map(func(c rune) rune {
		if c == '\n' || c == '\t' || (c >= 0x20 && c != 0x7f && !(c >= 0x80 && c <= 0x9f)) {
			return c
		}
		return -1
	}, s)
}

func sanitize(s string) string {
	var b strings.Builder
	at := 0
	for _, m := range allowedEscape.FindAllStringIndex(s, -1) {
		b.WriteString(stripControl(s[at:m[0]]))
		if color {
			b.WriteString(s[m[0]:m[1]])
		}
		at = m[1]
	}
	b.WriteString(stripControl(s[at:]))
	return b.String()
}

func hyperlink(t, href string) string {
	if !color {
		return t
	}
	return "\x1b]8;;" + href + "\x07" + t + "\x1b]8;;\x07"
}

func vis(s string) int { return runewidth.StringWidth(ansi.ReplaceAllString(s, "")) }

type style struct {
	b, i, code bool
	href       string
}

type seg struct {
	t string
	s style
}

func paint(t string, s style) string {
	if s.code {
		t = cyan(t)
	}
	if s.b {
		t = bold(t)
	}
	if s.i {
		t = italic(t)
	}
	if s.href != "" {
		t = hyperlink(under(t), s.href)
	}
	return t
}

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, i := range items {
		m[i] = true
	}
	return m
}

var (
	skipTags  = set("script", "style", "noscript", "svg", "button", "form", "input", "iframe", "template", "link", "meta", "nav", "head", "math")
	blockTags = set("address", "article", "aside", "blockquote", "body", "caption", "center", "dd", "details", "div", "dl", "dt",
		"figcaption", "figure", "footer", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "html", "li", "main", "ol", "p", "pre",
		"section", "summary", "table", "textarea", "tbody", "td", "tfoot", "th", "thead", "tr", "ul")
)

type renderer struct {
	base *url.URL
	memo map[*html.Node]bool
}

func newRenderer(base string) *renderer {
	u, _ := url.Parse(base)
	return &renderer{base: u, memo: map[*html.Node]bool{}}
}

func (r *renderer) hasBlock(n *html.Node) bool {
	if v, ok := r.memo[n]; ok {
		return v
	}
	found := false
	for c := n.FirstChild; c != nil && !found; c = c.NextSibling {
		if c.Type == html.ElementNode && (blockTags[c.Data] || r.hasBlock(c)) {
			found = true
		}
	}
	r.memo[n] = found
	return found
}

func (r *renderer) isBlock(n *html.Node) bool {
	return n.Type == html.ElementNode && (blockTags[n.Data] || r.hasBlock(n))
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func (r *renderer) href(n *html.Node) string {
	href := attr(n, "href")
	if n.Data != "a" || href == "" || strings.HasPrefix(href, "#") || r.base == nil {
		return ""
	}
	u, err := r.base.Parse(href)
	if err != nil {
		return ""
	}
	return u.String()
}

func (r *renderer) inline(n *html.Node, s style, out *[]seg) {
	if n.Type == html.TextNode {
		*out = append(*out, seg{strings.ReplaceAll(n.Data, "\n", " "), s})
		return
	}
	if n.Type != html.ElementNode || skipTags[n.Data] {
		return
	}
	switch n.Data {
	case "br":
		*out = append(*out, seg{"\n", s})
		return
	case "img":
		alt := attr(n, "alt")
		if m := displayMath.FindStringSubmatch(alt); m != nil {
			c := s
			c.code = true
			*out = append(*out, seg{strings.TrimSpace(m[1]), c})
		} else if alt != "" {
			i := s
			i.i = true
			*out = append(*out, seg{"[" + alt + "]", i})
		}
		return
	case "b", "strong":
		s.b = true
	case "i", "em", "cite", "var":
		s.i = true
	case "code", "tt", "kbd", "samp":
		s.code = true
	case "sup":
		var b strings.Builder
		nodeText(n, &b)
		if strings.HasPrefix(strings.TrimSpace(b.String()), "[") {
			return
		}
		*out = append(*out, seg{"^", s})
	case "a":
		s.href = r.href(n)
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.inline(c, s, out)
	}
}

func wrap(segs []seg, width int) []string {
	var lines []string
	var line strings.Builder
	used := 0
	gap := false
	var word []seg
	flush := func() {
		lines = append(lines, line.String())
		line.Reset()
		used = 0
		gap = false
	}
	emit := func() {
		if len(word) == 0 {
			return
		}
		w := 0
		for _, p := range word {
			w += runewidth.StringWidth(p.t)
		}
		if w > width {
			if used > 0 {
				flush()
			}
			for _, p := range word {
				for _, r := range p.t {
					rw := runewidth.RuneWidth(r)
					if used+rw > width {
						flush()
					}
					line.WriteString(paint(string(r), p.s))
					used += rw
				}
			}
			word = nil
			gap = false
			return
		}
		if used > 0 && used+1+w > width {
			flush()
		}
		if used > 0 && gap {
			line.WriteByte(' ')
			used++
		}
		for _, p := range word {
			line.WriteString(paint(p.t, p.s))
		}
		used += w
		word = nil
		gap = false
	}
	for _, sg := range segs {
		for _, part := range pieces.FindAllString(sg.t, -1) {
			switch {
			case part == "\n":
				emit()
				if used > 0 {
					flush()
				}
			case strings.TrimLeft(part, " \t\r\f\v") == "":
				emit()
				gap = used > 0
			default:
				word = append(word, seg{part, sg.s})
			}
		}
	}
	emit()
	if used > 0 {
		flush()
	}
	return lines
}

func indent(lines []string, first, rest string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		switch {
		case l == "":
		case i == 0:
			out[i] = first + l
		default:
			out[i] = rest + l
		}
	}
	return out
}

func trimBlank(l []string) []string {
	for len(l) > 0 && l[0] == "" {
		l = l[1:]
	}
	for len(l) > 0 && l[len(l)-1] == "" {
		l = l[:len(l)-1]
	}
	return l
}

func children(n *html.Node, tags ...string) []*html.Node {
	want := set(tags...)
	var out []*html.Node
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && want[c.Data] {
			out = append(out, c)
		}
	}
	return out
}

func rows(table *html.Node) []*html.Node {
	out := children(table, "tr")
	for _, g := range children(table, "thead", "tbody", "tfoot") {
		out = append(out, children(g, "tr")...)
	}
	return out
}

func span(c *html.Node, key string) int {
	v, err := strconv.Atoi(attr(c, key))
	if err != nil || v < 1 {
		return 1
	}
	return v
}

func expandRows(trs []*html.Node) [][]*html.Node {
	grid := make([][]*html.Node, len(trs))
	carry := map[int]int{}
	for i, tr := range trs {
		col := 0
		var row []*html.Node
		for _, c := range children(tr, "td", "th") {
			for carry[col] > 0 {
				carry[col]--
				row = append(row, nil)
				col++
			}
			row = append(row, c)
			if n := span(c, "rowspan"); n > 1 {
				carry[col] = n - 1
			}
			col++
		}
		for carry[col] > 0 {
			carry[col]--
			row = append(row, nil)
			col++
		}
		grid[i] = row
	}
	return grid
}

func contains(n *html.Node, tag string) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && (c.Data == tag || contains(c, tag)) {
			return true
		}
	}
	return false
}

func (r *renderer) table(n *html.Node, width int) []string {
	var out []string
	for _, c := range children(n, "caption") {
		if lines := trimBlank(r.blocks(c, width)); len(lines) > 0 {
			out = append(append(out, lines...), "")
		}
	}
	trs := rows(n)
	grid := expandRows(trs)
	cols := 0
	for _, row := range grid {
		cols = max(cols, len(row))
	}
	if cols == 0 {
		return out
	}
	if cols == 1 || contains(n, "table") {
		for _, tr := range trs {
			out = append(out, r.blocks(tr, width)...)
		}
		return out
	}
	const gap = 2
	if cols*8+gap*(cols-1) > width {
		for _, row := range grid {
			for _, c := range row {
				if c != nil {
					out = append(out, trimBlank(r.blocks(c, width))...)
				}
			}
			out = append(out, "")
		}
		return out
	}
	spans := func(c *html.Node) bool { return c != nil && span(c, "colspan") > 1 }
	widths := make([]int, cols)
	for i := range widths {
		widths[i] = 1
	}
	for _, row := range grid {
		for i, c := range row {
			if c == nil || spans(c) {
				continue
			}
			for _, l := range r.blocks(c, 1000) {
				widths[i] = max(widths[i], vis(l))
			}
		}
	}
	room := width - gap*(cols-1)
	for {
		total, wide := 0, 0
		for i, w := range widths {
			total += w
			if w > widths[wide] {
				wide = i
			}
		}
		if total <= room || widths[wide] <= 8 {
			break
		}
		widths[wide]--
	}
	for _, row := range grid {
		anySpan := false
		header := len(row) > 0
		for _, c := range row {
			anySpan = anySpan || spans(c)
			header = header && c != nil && c.Data == "th"
		}
		if len(row) == 1 || anySpan {
			for _, c := range row {
				if c != nil {
					out = append(out, r.blocks(c, width)...)
				}
			}
			continue
		}
		cells := make([][]string, len(row))
		h := 1
		for i, c := range row {
			if c != nil {
				cells[i] = trimBlank(r.blocks(c, widths[i]))
			}
			h = max(h, len(cells[i]))
		}
		for y := 0; y < h; y++ {
			var b strings.Builder
			for i := range row {
				t := ""
				if y < len(cells[i]) {
					t = cells[i][y]
				}
				b.WriteString(t)
				if i < len(row)-1 {
					b.WriteString(strings.Repeat(" ", max(0, widths[i]-vis(t))+gap))
				}
			}
			line := strings.TrimRight(b.String(), " ")
			if header {
				line = bold(line)
			}
			out = append(out, line)
		}
	}
	return out
}

func nodeText(n *html.Node, b *strings.Builder) {
	if n.Type == html.TextNode {
		b.WriteString(n.Data)
	}
	if n.Type == html.ElementNode && n.Data == "br" {
		b.WriteByte('\n')
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		nodeText(c, b)
	}
}

func (r *renderer) flat(n *html.Node) []seg {
	var segs []seg
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		r.inline(c, style{}, &segs)
	}
	return segs
}

func (r *renderer) blocks(n *html.Node, width int) []string {
	width = max(width, 8)
	var out []string
	var run []*html.Node
	para := func(gap bool) {
		if len(run) == 0 {
			return
		}
		var segs []seg
		for _, c := range run {
			r.inline(c, style{}, &segs)
		}
		run = nil
		if lines := wrap(segs, width); len(lines) > 0 {
			out = append(out, lines...)
			if gap {
				out = append(out, "")
			}
		}
	}
	push := func(l []string) {
		if t := trimBlank(l); len(t) > 0 {
			out = append(append(out, t...), "")
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.CommentNode || (c.Type == html.ElementNode && skipTags[c.Data]) {
			continue
		}
		if !r.isBlock(c) {
			run = append(run, c)
			continue
		}
		para(true)
		switch c.Data {
		case "h1", "h2", "h3", "h4", "h5", "h6":
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			for _, l := range wrap(r.flat(c), width) {
				out = append(out, heading(l))
			}
			out = append(out, "")
		case "pre", "textarea":
			var b strings.Builder
			nodeText(c, &b)
			src := strings.Split(strings.ReplaceAll(b.String(), "\t", "    "), "\n")
			lines := make([]string, len(src))
			for i, l := range src {
				if l = strings.TrimRight(l, " \r"); l != "" {
					lines[i] = cyan(l)
				}
			}
			push(indent(lines, "  ", "  "))
		case "hr":
			push([]string{dim(strings.Repeat("─", min(width, 40)))})
		case "ul", "ol":
			num, err := strconv.Atoi(attr(c, "start"))
			if err != nil {
				num = 1
			}
			var lines []string
			for _, li := range children(c, "li") {
				mark := "• "
				if c.Data == "ol" {
					mark = strconv.Itoa(num) + ". "
					num++
				}
				pad := strings.Repeat(" ", vis(mark))
				lines = append(lines, indent(trimBlank(r.blocks(li, width-vis(mark))), mark, pad)...)
			}
			push(lines)
		case "dl":
			var lines []string
			for _, x := range children(c, "dt", "dd", "div") {
				if x.Data == "dd" {
					lines = append(lines, indent(trimBlank(r.blocks(x, width-4)), "    ", "    ")...)
				} else {
					lines = append(lines, trimBlank(r.blocks(x, width))...)
				}
			}
			push(lines)
		case "blockquote":
			bar := dim("│ ")
			push(indent(trimBlank(r.blocks(c, width-2)), bar, bar))
		case "table":
			push(r.table(c, width))
		default:
			if inner := trimBlank(r.blocks(c, width)); len(inner) > 0 {
				if href := r.href(c); href != "" {
					for i, l := range inner {
						inner[i] = hyperlink(under(l), href)
					}
				}
				out = append(out, inner...)
				if c.Data != "td" && c.Data != "th" {
					out = append(out, "")
				}
			}
		}
	}
	para(false)
	return out
}

var blankRuns = regexp.MustCompile(`\n{3,}`)

func renderNode(n *html.Node, base string, width int) string {
	lines := trimBlank(newRenderer(base).blocks(n, width))
	return blankRuns.ReplaceAllString(strings.Join(lines, "\n"), "\n\n")
}

func renderSel(s *goquery.Selection, base string, width int) string {
	if s.Length() == 0 {
		return ""
	}
	return renderNode(s.Get(0), base, width)
}

func parseDoc(b []byte) (*goquery.Document, error) {
	return goquery.NewDocumentFromReader(bytes.NewReader(b))
}

func renderHTML(src, base string, width int) string {
	doc, err := parseDoc([]byte(src))
	if err != nil {
		return src
	}
	return renderSel(doc.Find("body"), base, width)
}

var md = goldmark.New(goldmark.WithExtensions(extension.GFM), goldmark.WithRendererOptions(gmhtml.WithUnsafe()))

func renderMD(src, base string, width int) string {
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return src
	}
	return renderHTML(b.String(), base, width)
}
