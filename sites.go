package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	stdhtml "html"
	"io"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

type Page struct{ Title, URL, Body string }

type Site struct {
	ID, Name string
	Noise    map[string]bool
	EmptyOK  bool
	Explicit bool
	Fuzzy    bool
	Get      func(term string, width int) (*Page, error)
}

func noise(s string) map[string]bool { return set(strings.Fields(s)...) }

func (s *Site) term(query string) string {
	fields := strings.Fields(query)
	var kept []string
	for _, f := range fields {
		if !s.Noise[strings.ToLower(strings.Trim(f, "?!,."))] {
			kept = append(kept, strings.TrimRight(f, "?!,"))
		}
	}
	if len(kept) == 0 && !s.EmptyOK {
		return fields[len(fields)-1]
	}
	return strings.Join(kept, " ")
}

func siteByID(id string) *Site {
	for _, s := range sites {
		if s.ID == id {
			return s
		}
	}
	return nil
}

func fill(words []string, width int) []string {
	var lines []string
	line := ""
	for i, w := range words {
		if i < len(words)-1 {
			w += ","
		}
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) > width:
			lines = append(lines, line)
			line = w
		default:
			line += " " + w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func text(s *goquery.Selection) string { return strings.TrimSpace(s.Text()) }

func thesaurusGet(term string, width int) (*Page, error) {
	u := "https://www.thesaurus.com/browse/" + url.PathEscape(strings.ToLower(term))
	b, err := fetch(u, req{})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	var out []string
	doc.Find(".definition-block").Each(func(_ int, block *goquery.Selection) {
		pos := strings.ToLower(text(block.Find(".part-of-speech-label").First()))
		out = append(out, bold(pos+": "+text(block.Find(".definition").First())), "")
		block.Find(".synonym-antonym-panel").Each(func(_ int, panel *goquery.Selection) {
			out = append(out, "  "+bold(text(panel.Find(".synonym-antonym-panel-label"))))
			labels := panel.Find(".similarity-level-label")
			panel.Find(".similarity-level-word-list").Each(func(i int, list *goquery.Selection) {
				var words []string
				list.Find("a").Each(func(_ int, a *goquery.Selection) { words = append(words, text(a)) })
				out = append(out, "  "+dim(strings.ToLower(text(labels.Eq(i)))))
				for _, l := range fill(words, width-4) {
					out = append(out, "    "+l)
				}
			})
			out = append(out, "")
		})
	})
	if len(out) == 0 {
		return nil, nil
	}
	return &Page{term, u, strings.TrimRight(strings.Join(out, "\n"), "\n")}, nil
}

func dictionaryGet(term string, width int) (*Page, error) {
	p, err := dictionaryWord(term, width)
	if p == nil && err == nil && term != strings.ToLower(term) {
		return dictionaryWord(strings.ToLower(term), width)
	}
	return p, err
}

func dictionaryWord(term string, width int) (*Page, error) {
	word := url.PathEscape(strings.ReplaceAll(term, " ", "_"))
	var data struct {
		En []struct {
			PartOfSpeech string `json:"partOfSpeech"`
			Language     string `json:"language"`
			Definitions  []struct {
				Definition     string `json:"definition"`
				ParsedExamples []struct {
					Example string `json:"example"`
				} `json:"parsedExamples"`
			} `json:"definitions"`
		} `json:"en"`
	}
	ok, err := fetchJSON("https://en.wiktionary.org/api/rest_v1/page/definition/"+word, req{ua: apiUA}, &data)
	if err != nil || !ok || len(data.En) == 0 {
		return nil, err
	}
	u := "https://en.wiktionary.org/wiki/" + word
	var out []string
	english := false
	for _, e := range data.En {
		english = english || e.Language == "English"
	}
	for _, e := range data.En {
		if english && e.Language != "English" {
			continue
		}
		out = append(out, bold(e.PartOfSpeech), "")
		n := 0
		for _, d := range e.Definitions {
			if d.Definition == "" {
				continue
			}
			n++
			for j, l := range strings.Split(renderHTML(d.Definition, u, width-6), "\n") {
				if j == 0 {
					out = append(out, fmt.Sprintf("%2d. %s", n, l))
				} else {
					out = append(out, "    "+l)
				}
			}
			for _, ex := range d.ParsedExamples {
				for _, l := range strings.Split(renderHTML(ex.Example, u, width-8), "\n") {
					out = append(out, "      "+dim(l))
				}
			}
		}
		out = append(out, "")
	}
	return &Page{term, u, strings.TrimRight(strings.Join(out, "\n"), "\n")}, nil
}

var cppLink = regexp.MustCompile(`<a href="([^"]+)"[^>]*>\s*<tt>([^<]+)</tt>`)

const cppRoot = "https://en.cppreference.com/w/cpp"

func cppIndex(page string) (map[string]string, error) {
	u := cppRoot + "/symbol_index" + page
	b, err := fetch(u, req{ttl: month})
	if err != nil {
		return nil, err
	}
	idx := map[string]string{}
	for _, m := range cppLink.FindAllSubmatch(b, -1) {
		name := stdhtml.UnescapeString(string(m[2]))
		if i := strings.IndexAny(name, "<("); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimSpace(name)
		href := strings.TrimPrefix(strings.TrimPrefix(string(m[1]), "/w"), "/cpp")
		if _, seen := idx[name]; !seen && strings.HasPrefix(href, "/") && !strings.HasPrefix(href, "/symbol_index") {
			idx[name] = cppRoot + href
		}
	}
	if len(idx) == 0 {
		forget(u)
	}
	return idx, nil
}

var (
	templateArgs  = regexp.MustCompile(`<[^<>]*>`)
	cppNamespaces = set("chrono", "filesystem", "ranges", "views")
	cppOperators  = map[string]string{"operator[]": "operator_at", "operator==": "operator_cmp", "operator<<": "operator_ltlt", "operator>>": "operator_gtgt"}
)

func stripTemplates(s string) string {
	for templateArgs.MatchString(s) {
		s = templateArgs.ReplaceAllString(s, "")
	}
	return s
}

func unique(items []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, i := range items {
		if !seen[i] {
			seen[i] = true
			out = append(out, i)
		}
	}
	return out
}

func cppGet(term string, width int) (*Page, error) {
	norm := strings.ReplaceAll(strings.TrimPrefix(stripTemplates(term), "std::"), " ", "::")
	var parts []string
	for _, p := range strings.Split(norm, "::") {
		p = strings.TrimSuffix(p, "()")
		if op, ok := cppOperators[p]; ok {
			p = op
		}
		if p != "" && p != "std" {
			parts = append(parts, p)
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	var known, tries []string
	if len(parts) > 1 && cppNamespaces[parts[0]] {
		sub, err := cppIndex("/" + parts[0])
		if err != nil {
			return nil, err
		}
		if root, ok := sub[parts[1]]; ok {
			known = append(known, strings.Join(append([]string{root}, parts[2:]...), "/"))
		}
	}
	idx, err := cppIndex("")
	if err != nil {
		return nil, err
	}
	if root, ok := idx[parts[0]]; ok {
		known = append(known, strings.Join(append([]string{root}, parts[1:]...), "/"))
	}
	for _, u := range known {
		b, err := fetch(u, req{})
		if err != nil {
			return nil, err
		}
		if p, err := cppPage(b, u, width); p != nil || err != nil {
			return p, err
		}
	}
	joined := strings.Join(parts, "_")
	for _, name := range []string{joined, strings.TrimSuffix(joined, "s"), strings.Join(parts, "-"), parts[0], strings.TrimSuffix(parts[0], "s")} {
		tries = append(tries, cppRoot+"/language/"+name, cppRoot+"/keyword/"+name)
	}
	tries = unique(append(tries, cppRoot+"/"+parts[0]))
	for i, b := range fetchAll(tries, req{}) {
		if p, err := cppPage(b, tries[i], width); p != nil || err != nil {
			return p, err
		}
	}
	return nil, nil
}

func cppPage(b []byte, u string, width int) (*Page, error) {
	if b == nil {
		return nil, nil
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	body := doc.Find("#mw-content-text")
	if body.Length() == 0 {
		return nil, nil
	}
	body.Find(".t-navbar, .toc, #toc, .editsection, .printfooter, .catlinks, .noprint, .t-page-template").Remove()
	return &Page{text(doc.Find("#firstHeading")), u, renderSel(body, u, width)}, nil
}

var (
	pyLine      = regexp.MustCompile(`^(\S+)\s+(py:\S+)\s+\S+\s+(\S+)`)
	pyLabelLine = regexp.MustCompile(`^(\S+)\s+std:label\s+\S+\s+(\S+)\s+(.+)$`)
)

func pyIndex() (map[string]string, error) {
	idx, _, err := pyInventory()
	return idx, err
}

func pyInventory() (map[string]string, []llmsEntry, error) {
	const u = "https://docs.python.org/3/objects.inv"
	raw, err := fetch(u, req{ttl: month})
	if err != nil || raw == nil {
		return nil, nil, err
	}
	at := 0
	for range 4 {
		at += bytes.IndexByte(raw[at:], '\n') + 1
	}
	z, err := zlib.NewReader(bytes.NewReader(raw[at:]))
	if err != nil {
		forget(u)
		return nil, nil, err
	}
	inv, err := io.ReadAll(z)
	if err != nil {
		forget(u)
		return nil, nil, err
	}
	idx := map[string]string{}
	var labels []llmsEntry
	for _, line := range strings.Split(string(inv), "\n") {
		if m := pyLabelLine.FindStringSubmatch(line); m != nil && m[3] != "-" {
			labels = append(labels, llmsEntry{m[3], strings.TrimSuffix(m[2], "$") + map[bool]string{true: m[1]}[strings.HasSuffix(m[2], "$")], ""})
			continue
		}
		m := pyLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if _, seen := idx[m[1]]; !seen {
			idx[m[1]] = strings.TrimSuffix(m[3], "$") + map[bool]string{true: m[1]}[strings.HasSuffix(m[3], "$")]
		}
	}
	return idx, labels, nil
}

func pyMatch(idx map[string]string, match func(string) bool) string {
	key := ""
	for k := range idx {
		if match(k) && (key == "" || len(k) < len(key) || (len(k) == len(key) && k < key)) {
			key = k
		}
	}
	return key
}

func pyLabelMatch(labels []llmsEntry, term string) *llmsEntry {
	want := strings.ToLower(term)
	for i := range labels {
		t := strings.ToLower(labels[i].title)
		if t == want || t == "the "+want {
			return &labels[i]
		}
	}
	return llmsBest(labels, term)
}

func pythonGet(term string, width int) (*Page, error) {
	name := strings.ReplaceAll(term, " ", ".")
	idx, labels, err := pyInventory()
	if err != nil {
		return nil, err
	}
	key := name
	if _, ok := idx[key]; !ok {
		key = pyMatch(idx, func(k string) bool { return strings.HasSuffix(k, "."+name) })
		if key == "" {
			want := strings.ToLower(name)
			key = pyMatch(idx, func(k string) bool {
				lower := strings.ToLower(k)
				return lower == want || strings.HasSuffix(lower, "."+want)
			})
		}
	}
	var uri string
	if key != "" {
		uri = idx[key]
	} else if e := pyLabelMatch(labels, term); e != nil {
		key, uri = e.title, e.url
	} else {
		return nil, nil
	}
	file, id, _ := strings.Cut(uri, "#")
	u := "https://docs.python.org/3/" + uri
	b, err := fetch("https://docs.python.org/3/"+file, req{})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	main := doc.Find(`[role="main"]`)
	main.Find(".headerlink").Remove()
	scoped := main
	if id != "" {
		t := doc.Find(`[id="` + id + `"]`).First()
		switch goquery.NodeName(t) {
		case "dt":
			scoped = t.Parent()
		case "section":
			scoped = t
		}
	}
	return &Page{key, u, renderSel(scoped, u, width)}, nil
}

var rustLink = regexp.MustCompile(`<a href="([^"]+\.html)">([^<]+)</a>`)

type rustItem struct{ path, href string }

func rustFind(items []rustItem, n string) *rustItem {
	var best *rustItem
	pick := func(match func(string) bool) {
		for i := range items {
			if match(items[i].path) && (best == nil || len(items[i].path) < len(best.path)) {
				best = &items[i]
			}
		}
	}
	pick(func(p string) bool { return p == n || strings.HasSuffix(p, "::"+n) })
	if best == nil {
		l := strings.ToLower(n)
		pick(func(p string) bool { p = strings.ToLower(p); return p == l || strings.HasSuffix(p, "::"+l) })
	}
	return best
}

func rustGet(term string, width int) (*Page, error) {
	p, err := rustDocs("https://doc.rust-lang.org/std/", term, width)
	if p != nil || err != nil || !strings.Contains(term, "::") {
		return p, err
	}
	return docsrsGet(term, width)
}

func docsrsGet(term string, width int) (*Page, error) {
	krate, rest, _ := strings.Cut(strings.ReplaceAll(term, " ", "::"), "::")
	base := "https://docs.rs/" + krate + "/latest/" + strings.ReplaceAll(krate, "-", "_") + "/"
	if rest == "" {
		return rustPage(base+"index.html", krate, width)
	}
	return rustDocs(base, rest, width)
}

func rustPage(u, title string, width int) (*Page, error) {
	b, err := fetch(u, req{})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	main := doc.Find("#main-content")
	main.Find(".rightside, .src, .anchor, .doc-anchor, .out-of-band, rustdoc-toolbar, .sub-heading").Remove()
	return &Page{title, u, renderSel(main, u, width)}, nil
}

func rustDocs(base, term string, width int) (*Page, error) {
	norm := strings.ReplaceAll(strings.TrimPrefix(stripTemplates(term), "std::"), " ", "::")
	var parts []string
	for _, p := range strings.Split(norm, "::") {
		if p != "" {
			parts = append(parts, strings.TrimRight(p, "!()"))
		}
	}
	if len(parts) == 0 {
		return nil, nil
	}
	b, err := fetch(base+"all.html", req{ttl: month})
	if err != nil {
		return nil, err
	}
	var items []rustItem
	for _, m := range rustLink.FindAllSubmatch(b, -1) {
		items = append(items, rustItem{string(m[2]), string(m[1])})
	}
	if len(items) == 0 {
		forget(base + "all.html")
	}
	whole := rustFind(items, strings.Join(parts, "::"))
	owner := whole
	if owner == nil && len(parts) > 1 {
		owner = rustFind(items, strings.Join(parts[:len(parts)-1], "::"))
	}
	title := strings.Join(parts, "::")
	candidates := []string{"keyword." + title + ".html", "primitive." + title + ".html", strings.Join(parts, "/") + "/index.html"}
	if owner != nil {
		candidates, title = []string{owner.href}, owner.path
	}
	var u string
	var page []byte
	for _, href := range candidates {
		u = base + href
		if page, err = fetch(u, req{}); err != nil {
			return nil, err
		}
		if page != nil {
			break
		}
	}
	if page == nil {
		return nil, nil
	}
	doc, err := parseDoc(page)
	if err != nil {
		return nil, err
	}
	main := doc.Find("#main-content")
	if main.Length() == 0 {
		return nil, nil
	}
	main.Find(".rightside, .src, .anchor, .doc-anchor, .out-of-band, rustdoc-toolbar, .sub-heading").Remove()
	if whole == nil && owner != nil {
		member := parts[len(parts)-1]
		for _, prefix := range []string{"method.", "tymethod."} {
			if box := doc.Find(`[id="` + prefix + member + `"]`).First().Closest("details"); box.Length() > 0 {
				return &Page{strings.Join(parts, "::"), u + "#" + prefix + member, renderSel(box, u, width)}, nil
			}
		}
	}
	return &Page{title, u, renderSel(main, u, width)}, nil
}

func longest(term string) string {
	best := ""
	for _, w := range strings.Fields(term) {
		if len(w) > len(best) {
			best = w
		}
	}
	return strings.ToLower(best)
}

func mdnGet(term string, width int) (*Page, error) {
	var found struct {
		Documents []struct {
			URL   string `json:"mdn_url"`
			Title string `json:"title"`
		} `json:"documents"`
	}
	ok, err := fetchJSON("https://developer.mozilla.org/api/v1/search?locale=en-US&q="+url.QueryEscape(term), req{}, &found)
	if err != nil || !ok {
		return nil, err
	}
	want := longest(term)
	for _, d := range found.Documents {
		if !strings.Contains(strings.ToLower(d.Title+" "+d.URL), want) {
			continue
		}
		u := "https://developer.mozilla.org" + d.URL
		var data struct {
			Doc struct {
				Title string `json:"title"`
				Body  []struct {
					Type  string `json:"type"`
					Value struct {
						Title   string `json:"title"`
						Content string `json:"content"`
					} `json:"value"`
				} `json:"body"`
			} `json:"doc"`
		}
		ok, err := fetchJSON(u+"/index.json", req{}, &data)
		if err != nil || !ok {
			return nil, err
		}
		var out []string
		for _, part := range data.Doc.Body {
			if part.Type != "prose" {
				continue
			}
			if part.Value.Title != "" {
				out = append(out, heading(part.Value.Title), "")
			}
			out = append(out, renderHTML(part.Value.Content, u, width), "")
		}
		return &Page{data.Doc.Title, u, strings.TrimRight(strings.Join(out, "\n"), "\n")}, nil
	}
	return nil, nil
}

type appleNode struct {
	Title    string      `json:"title"`
	Path     string      `json:"path"`
	Type     string      `json:"type"`
	Children []appleNode `json:"children"`
}

var appleFrameworks = []string{"swiftui", "foundation", "uikit", "appkit", "swift", "combine", "swiftdata", "healthkit", "storekit"}
var applePrefix = regexp.MustCompile(`^(struct|class|protocol|enum|func|var|init)\s+`)

func appleWalk(n *appleNode, visit func(*appleNode)) {
	visit(n)
	for i := range n.Children {
		appleWalk(&n.Children[i], visit)
	}
}

func esc(s string) string { return stdhtml.EscapeString(s) }

type appleInlineNode struct {
	Type          string            `json:"type"`
	Text          string            `json:"text"`
	Code          string            `json:"code"`
	Identifier    string            `json:"identifier"`
	InlineContent []appleInlineNode `json:"inlineContent"`
}

type appleBlock struct {
	Type          string            `json:"type"`
	Level         int               `json:"level"`
	Text          string            `json:"text"`
	InlineContent []appleInlineNode `json:"inlineContent"`
	Code          []string          `json:"code"`
	Items         []struct {
		Content []appleBlock `json:"content"`
	} `json:"items"`
	Name    string       `json:"name"`
	Style   string       `json:"style"`
	Content []appleBlock `json:"content"`
}

type appleRefs map[string]struct {
	Title string `json:"title"`
}

func appleInline(items []appleInlineNode, refs appleRefs) string {
	var b strings.Builder
	for _, i := range items {
		switch i.Type {
		case "text":
			b.WriteString(esc(i.Text))
		case "codeVoice":
			b.WriteString("<code>" + esc(i.Code) + "</code>")
		case "reference":
			b.WriteString("<code>" + esc(refs[i.Identifier].Title) + "</code>")
		case "emphasis":
			b.WriteString("<em>" + appleInline(i.InlineContent, refs) + "</em>")
		case "strong":
			b.WriteString("<strong>" + appleInline(i.InlineContent, refs) + "</strong>")
		}
	}
	return b.String()
}

func appleBlocks(items []appleBlock, refs appleRefs) string {
	var b strings.Builder
	for _, x := range items {
		switch x.Type {
		case "heading":
			fmt.Fprintf(&b, "<h%d>%s</h%d>", x.Level, esc(x.Text), x.Level)
		case "paragraph":
			b.WriteString("<p>" + appleInline(x.InlineContent, refs) + "</p>")
		case "codeListing":
			b.WriteString("<pre>" + esc(strings.Join(x.Code, "\n")) + "</pre>")
		case "unorderedList", "orderedList":
			tag := "ul"
			if x.Type == "orderedList" {
				tag = "ol"
			}
			b.WriteString("<" + tag + ">")
			for _, i := range x.Items {
				b.WriteString("<li>" + appleBlocks(i.Content, refs) + "</li>")
			}
			b.WriteString("</" + tag + ">")
		case "aside":
			name := x.Name
			if name == "" {
				name = x.Style
			}
			b.WriteString("<blockquote><p><strong>" + esc(name) + "</strong></p>" + appleBlocks(x.Content, refs) + "</blockquote>")
		}
	}
	return b.String()
}

func appleGet(term string, width int) (*Page, error) {
	want := strings.ToLower(strings.ReplaceAll(term, " ", ""))
	var best *appleNode
	rank := 99
	for _, fw := range appleFrameworks {
		var idx struct {
			InterfaceLanguages struct {
				Swift []appleNode `json:"swift"`
			} `json:"interfaceLanguages"`
		}
		ok, err := fetchJSON("https://developer.apple.com/tutorials/data/index/"+fw, req{ttl: month}, &idx)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		for i := range idx.InterfaceLanguages.Swift {
			appleWalk(&idx.InterfaceLanguages.Swift[i], func(n *appleNode) {
				if n.Path == "" || n.Title == "" || n.Type == "groupMarker" {
					return
				}
				t := applePrefix.ReplaceAllString(strings.ToLower(n.Title), "")
				bare, _, _ := strings.Cut(t, "(")
				r := -1
				if t == want {
					r = 0
				} else if bare == want {
					r = 1
				}
				if r >= 0 && r < rank {
					rank, best = r, n
				}
			})
		}
		if rank == 0 {
			break
		}
	}
	if best == nil {
		return nil, nil
	}
	var data struct {
		Abstract []appleInlineNode `json:"abstract"`
		Metadata struct {
			Title string `json:"title"`
		} `json:"metadata"`
		Sections []struct {
			Kind         string `json:"kind"`
			Declarations []struct {
				Tokens []struct {
					Text string `json:"text"`
				} `json:"tokens"`
			} `json:"declarations"`
			Content    []appleBlock `json:"content"`
			Parameters []struct {
				Name    string       `json:"name"`
				Content []appleBlock `json:"content"`
			} `json:"parameters"`
		} `json:"primaryContentSections"`
		References appleRefs `json:"references"`
	}
	ok, err := fetchJSON("https://developer.apple.com/tutorials/data"+best.Path+".json", req{}, &data)
	if err != nil || !ok {
		return nil, err
	}
	u := "https://developer.apple.com" + best.Path
	var b strings.Builder
	b.WriteString("<p>" + appleInline(data.Abstract, data.References) + "</p>")
	for _, s := range data.Sections {
		for _, d := range s.Declarations {
			b.WriteString("<pre>")
			for _, t := range d.Tokens {
				b.WriteString(esc(t.Text))
			}
			b.WriteString("</pre>")
		}
		if len(s.Parameters) > 0 {
			b.WriteString("<h2>Parameters</h2><dl>")
			for _, p := range s.Parameters {
				b.WriteString("<dt><code>" + esc(p.Name) + "</code></dt><dd>" + appleBlocks(p.Content, data.References) + "</dd>")
			}
			b.WriteString("</dl>")
		}
		b.WriteString(appleBlocks(s.Content, data.References))
	}
	return &Page{data.Metadata.Title, u, renderHTML(b.String(), u, width)}, nil
}

var wikiDrop = set("References", "External links", "Notes", "Citations", "Further reading", "Bibliography", "Sources")

func wikipediaGet(term string, width int) (*Page, error) {
	return mediawiki("en.wikipedia.org", term, width)
}

func mediawiki(host, term string, width int) (*Page, error) {
	var found struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	ok, err := fetchJSON("https://"+host+"/w/api.php?action=query&list=search&format=json&srlimit=1&srsearch="+url.QueryEscape(term), req{ua: apiUA}, &found)
	if err != nil || !ok || len(found.Query.Search) == 0 {
		return nil, err
	}
	return wikiPage(host, found.Query.Search[0].Title, width)
}

func wikiPage(host, title string, width int) (*Page, error) {
	slug := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	b, err := fetch("https://"+host+"/api/rest_v1/page/html/"+slug, req{ua: apiUA})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	body := doc.Find("body")
	body.Find(".navbox, .navbox-styles, .mw-editsection, sup.reference, sup.mw-ref, .reflist, .metadata, .sistersitebox, .mw-empty-elt, .hatnote, .thumb, figure, .infobox, .sidebar").Remove()
	body.Find("section").Each(func(_ int, s *goquery.Selection) {
		if wikiDrop[text(s.Find("h2").First())] {
			s.Remove()
		}
	})
	u := "https://" + host + "/wiki/" + slug
	return &Page{title, u, renderSel(body, u, width)}, nil
}

func titleVariants(q string) []string {
	q = strings.TrimSpace(strings.Trim(q, "?!,"))
	words := strings.Fields(q)
	titled := make([]string, len(words))
	for i, w := range words {
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		titled[i] = string(r)
	}
	out := []string{q, strings.Join(titled, " ")}
	if len(q) <= 5 {
		out = append(out, strings.ToUpper(q))
	}
	return unique(out)
}

func wikiTitleGet(query string, width int) (*Page, error) {
	if query == "" || strings.ContainsAny(query, "|#<>[]{}") || len(strings.Fields(query)) > 6 {
		return nil, nil
	}
	variants := titleVariants(query)
	var data struct {
		Query struct {
			Normalized []struct{ From, To string } `json:"normalized"`
			Redirects  []struct{ From, To string } `json:"redirects"`
			Pages      []struct {
				Title   string `json:"title"`
				Missing bool   `json:"missing"`
			} `json:"pages"`
		} `json:"query"`
	}
	u := "https://en.wikipedia.org/w/api.php?action=query&format=json&formatversion=2&redirects=1&titles=" + url.QueryEscape(strings.Join(variants, "|"))
	ok, err := fetchJSON(u, req{ua: apiUA}, &data)
	if err != nil || !ok {
		return nil, err
	}
	moved := map[string]string{}
	for _, n := range data.Query.Normalized {
		moved[n.From] = n.To
	}
	for _, n := range data.Query.Redirects {
		moved[n.From] = n.To
	}
	real := map[string]bool{}
	for _, p := range data.Query.Pages {
		real[p.Title] = !p.Missing
	}
	for _, v := range variants {
		for range 4 {
			next, ok := moved[v]
			if !ok {
				break
			}
			v = next
		}
		if real[v] {
			return wikiPage("en.wikipedia.org", v, width)
		}
	}
	return nil, nil
}

func stackoverflowGet(term string, width int) (*Page, error) {
	const root = "https://api.stackexchange.com/2.3"
	var found struct {
		Items []struct {
			ID    int    `json:"question_id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	ok, err := fetchJSON(root+"/search/advanced?order=desc&sort=relevance&site=stackoverflow&pagesize=1&accepted=True&q="+url.QueryEscape(term), req{ua: apiUA}, &found)
	if err != nil || !ok || len(found.Items) == 0 {
		return nil, err
	}
	q := found.Items[0]
	var answers struct {
		Items []struct {
			ID       int    `json:"answer_id"`
			Body     string `json:"body"`
			Score    int    `json:"score"`
			Accepted bool   `json:"is_accepted"`
			Owner    struct {
				Name string `json:"display_name"`
			} `json:"owner"`
		} `json:"items"`
	}
	ok, err = fetchJSON(root+"/questions/"+strconv.Itoa(q.ID)+"/answers?order=desc&sort=votes&site=stackoverflow&filter=withbody&pagesize=5", req{ua: apiUA}, &answers)
	if err != nil || !ok || len(answers.Items) == 0 {
		return nil, err
	}
	a := answers.Items[0]
	for _, x := range answers.Items {
		if x.Accepted {
			a = x
			break
		}
	}
	u := "https://stackoverflow.com/a/" + strconv.Itoa(a.ID)
	by := fmt.Sprintf("%s, %d votes", stdhtml.UnescapeString(a.Owner.Name), a.Score)
	if a.Accepted {
		by += ", accepted"
	}
	return &Page{stdhtml.UnescapeString(q.Title), u, dim(by) + "\n\n" + renderHTML(a.Body, u, width)}, nil
}

const goRoot = "https://pkg.go.dev"

var (
	goStdLink  = regexp.MustCompile(`<a href="/([a-z0-9/]+)@go[0-9.]+"`)
	goHit      = regexp.MustCompile(`<a href="([^"]+)"[^>]*data-test-id="snippet-title"`)
	goExported = regexp.MustCompile(`^[a-z0-9/]+\.[A-Z]`)
)

func goStd() (map[string]string, error) {
	b, err := fetch(goRoot+"/std", req{ttl: month})
	if err != nil {
		return nil, err
	}
	idx := map[string]string{}
	defer func() {
		if len(idx) == 0 {
			forget(goRoot + "/std")
		}
	}()
	for _, m := range goStdLink.FindAllSubmatch(b, -1) {
		path := string(m[1])
		name := path[strings.LastIndexByte(path, '/')+1:]
		if old, seen := idx[name]; !seen || len(path) < len(old) || (len(path) == len(old) && path < old) {
			idx[name] = path
		}
		idx[path] = path
	}
	return idx, nil
}

func goSearch(q, mode string) (string, error) {
	b, err := fetch(goRoot+"/search?m="+mode+"&q="+url.QueryEscape(q), req{})
	if err != nil || b == nil {
		return "", err
	}
	m := goHit.FindSubmatch(b)
	if m == nil {
		return "", nil
	}
	return strings.TrimPrefix(string(m[1]), "/"), nil
}

var goSymbolTail = regexp.MustCompile(`^(.*?)\.([A-Z].*)$`)

func goSplit(term string) (pkg, sym string) {
	t := term
	if !strings.Contains(t, "/") {
		t = strings.ReplaceAll(t, " ", ".")
	}
	head, tail := "", t
	if i := strings.LastIndexByte(t, '/'); i >= 0 {
		head, tail = t[:i+1], t[i+1:]
	}
	if strings.Contains(head, ".") {
		if m := goSymbolTail.FindStringSubmatch(tail); m != nil {
			return head + m[1], m[2]
		}
		return head + tail, ""
	}
	name, rest, _ := strings.Cut(tail, ".")
	return head + name, rest
}

var goSpecAlias = map[string]string{"generics": "type parameter declarations", "goroutine": "go statements", "goroutines": "go statements", "channel": "channel types", "channels": "channel types"}

func goSpec(term string, width int) (*Page, error) {
	const u = "https://go.dev/ref/spec"
	b, err := fetch(u, req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	want := strings.ToLower(term)
	if alias, ok := goSpecAlias[want]; ok {
		want = alias
	}
	var head *goquery.Selection
	doc.Find("h2, h3, h4").EachWithBreak(func(_ int, h *goquery.Selection) bool {
		t := strings.ToLower(text(h))
		if t == want || strings.HasPrefix(t, want+" ") || strings.HasPrefix(t, strings.TrimSuffix(want, "s")+" ") {
			head = h
			return false
		}
		return true
	})
	if head == nil {
		return nil, nil
	}
	var html strings.Builder
	head.NextUntil("h2, h3, h4").Each(func(_ int, s *goquery.Selection) {
		if h, err := goquery.OuterHtml(s); err == nil {
			html.WriteString(h)
		}
	})
	id, _ := head.Attr("id")
	return &Page{text(head), u + "#" + id, renderHTML(html.String(), u, width)}, nil
}

func goPage(pkg, sym string, width int) (*Page, error) {
	u := goRoot + "/" + pkg
	b, err := fetch(u, req{})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	content := doc.Find(".Documentation-content").First()
	content.Find(".Documentation-idLink, .Documentation-index, .Documentation-exampleButtonsContainer, .Documentation-examples, .Documentation-sinceVersion, a[title^='Go to']").Remove()
	if sym != "" {
		var target *goquery.Selection
		content.Find("[id]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			id, _ := s.Attr("id")
			if strings.EqualFold(id, sym) {
				target = s
				return false
			}
			return true
		})
		if target != nil {
			id, _ := target.Attr("id")
			box := target.Closest("div.Documentation-typeMethod, div.Documentation-typeFunc, div.Documentation-function, div.Documentation-type")
			if box.Length() > 0 {
				return &Page{pkg + "." + id, u + "#" + id, renderSel(box, u, width)}, nil
			}
		}
		if pkg == "builtin" {
			return nil, nil
		}
	}
	body := renderSel(content, u, width)
	if readme := renderSel(doc.Find(".UnitReadme-content").First(), u, width); readme != "" {
		body = readme + "\n\n" + body
	}
	return &Page{pkg, u, body}, nil
}

func goGet(term string, width int) (*Page, error) {
	pkg, sym := goSplit(term)
	if strings.Contains(pkg, ".") {
		return goPage(pkg, sym, width)
	}
	std, err := goStd()
	if err != nil {
		return nil, err
	}
	if path, ok := std[strings.ToLower(pkg)]; ok {
		return goPage(path, sym, width)
	}
	if sym == "" {
		if p, err := goPage("builtin", pkg, width); p != nil || err != nil {
			return p, err
		}
	}
	if p, err := goSpec(term, width); p != nil || err != nil {
		return p, err
	}
	if pkg != strings.ToLower(pkg) {
		hit, err := goSearch(strings.ReplaceAll(term, " ", "."), "symbol")
		if err != nil || hit == "" {
			return nil, err
		}
		path, s, _ := strings.Cut(hit, "#")
		return goPage(path, s, width)
	}
	if sym != "" {
		return nil, nil
	}
	hit, err := goSearch(pkg, "package")
	if err != nil || !strings.HasSuffix(strings.ToLower(hit), "/"+strings.ToLower(pkg)) {
		return nil, err
	}
	return goPage(hit, "", width)
}

func pkgName(term string) string { return strings.ReplaceAll(strings.ToLower(term), " ", "-") }

func npmGet(term string, width int) (*Page, error) {
	name := pkgName(term)
	var data struct {
		Readme string            `json:"readme"`
		Tags   map[string]string `json:"dist-tags"`
	}
	ok, err := fetchJSON("https://registry.npmjs.org/"+strings.ReplaceAll(name, "/", "%2f"), req{}, &data)
	if err != nil || !ok || data.Readme == "" {
		return nil, err
	}
	u := "https://www.npmjs.com/package/" + name
	return &Page{name + " " + data.Tags["latest"], u, renderMD(data.Readme, u, width)}, nil
}

func pypiGet(term string, width int) (*Page, error) {
	name := pkgName(term)
	var data struct {
		Info struct {
			Description string `json:"description"`
			Type        string `json:"description_content_type"`
			Version     string `json:"version"`
		} `json:"info"`
	}
	ok, err := fetchJSON("https://pypi.org/pypi/"+url.PathEscape(name)+"/json", req{}, &data)
	if err != nil || !ok || data.Info.Description == "" {
		return nil, err
	}
	u := "https://pypi.org/project/" + name + "/"
	body := data.Info.Description
	if strings.Contains(data.Info.Type, "markdown") {
		body = renderMD(body, u, width)
	}
	return &Page{name + " " + data.Info.Version, u, body}, nil
}

func cratesGet(term string, width int) (*Page, error) {
	name := pkgName(term)
	var data struct {
		Crate struct {
			Description string `json:"description"`
			Stable      string `json:"max_stable_version"`
			Max         string `json:"max_version"`
		} `json:"crate"`
	}
	ok, err := fetchJSON("https://crates.io/api/v1/crates/"+url.PathEscape(name), req{ua: apiUA}, &data)
	if err != nil || !ok {
		return nil, err
	}
	v := data.Crate.Stable
	if v == "" {
		v = data.Crate.Max
	}
	u := "https://crates.io/crates/" + name
	b, err := fetch("https://crates.io/api/v1/crates/"+url.PathEscape(name)+"/"+v+"/readme", req{ua: apiUA})
	if err != nil {
		return nil, err
	}
	body := data.Crate.Description
	if b != nil {
		body = renderHTML(string(b), u, width)
	}
	return &Page{name + " " + v, u, body}, nil
}

func manGet(term string, width int) (*Page, error) {
	m := exec.Command("man", strings.Fields(term)...)
	m.Env = append(os.Environ(), "MANWIDTH="+strconv.Itoa(width), "MANPAGER=cat", "PAGER=cat")
	raw, err := m.Output()
	if err != nil {
		return nil, nil
	}
	col := exec.Command("col", "-bx")
	col.Stdin = bytes.NewReader(raw)
	clean, err := col.Output()
	if err != nil {
		return nil, err
	}
	return &Page{term, "man " + term, strings.TrimSpace(string(clean))}, nil
}

func weatherGet(term string, width int) (*Page, error) {
	u := "https://wttr.in/" + url.PathEscape(term) + "?n"
	b, err := fetch(u, req{ua: "curl/8.7.1", ttl: 15 * time.Minute})
	if err != nil || b == nil {
		return nil, err
	}
	title := term
	if title == "" {
		title = "here"
	}
	return &Page{title, u, strings.TrimRight(string(b), "\n")}, nil
}

func qmd(args ...string) string {
	c := exec.Command("mise", append([]string{"exec", "node@26", "--", "qmd"}, args...)...)
	c.Env = append(os.Environ(), "BUN_INSTALL=", "GGML_METAL_TENSOR_DISABLE=1")
	out, _ := c.Output()
	return string(out)
}

var (
	qmdHit  = regexp.MustCompile(`qmd://\S+?\.md`)
	qmdLine = regexp.MustCompile(`(?m)^\s*\d+: ?`)
)

func notesGet(term string, width int) (*Page, error) {
	hit := qmdHit.FindString(qmd("search", term, "-n", "1"))
	if hit == "" {
		return nil, nil
	}
	src := qmdLine.ReplaceAllString(qmd("get", hit), "")
	if _, rest, ok := strings.Cut(src, "\n---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			src = body
		}
	}
	return &Page{strings.TrimPrefix(hit, "qmd://vault/"), hit, renderMD(src, "", width)}, nil
}

var pkgNoise = noise("npm npx pypi pip crate crates crates.io cargo add package readme library install docs doc documentation")

var sites = []*Site{
	{ID: "thesaurus", Name: "Thesaurus.com", Get: thesaurusGet, Noise: noise("synonym synonyms antonym antonyms thesaurus opposite opposites of for another other better similar word words different way ways to say like what's whats what is a an the")},
	{ID: "dictionary", Name: "Wiktionary", Get: dictionaryGet, Noise: noise("define definition meaning dictionary etymology pronounce how to what does mean means of is the word")},
	{ID: "cppreference", Name: "cppreference", Get: cppGet, Noise: noise("c++ c++11 c++14 c++17 c++20 c++23 c++26 cpp cppreference docs doc documentation reference ref stl page")},
	{ID: "python", Name: "Python docs", Get: pythonGet, Noise: noise("python python3 py docs doc documentation reference stdlib standard library module")},
	{ID: "rust", Name: "Rust std docs", Get: rustGet, Noise: noise("rust rustlang docs doc documentation reference std stdlib standard library macro")},
	{ID: "go", Name: "pkg.go.dev", Get: goGet, Noise: noise("go golang docs doc documentation reference package pkg stdlib standard library")},
	{ID: "mdn", Name: "MDN", Get: mdnGet, Fuzzy: true, Noise: noise("mdn docs doc documentation reference javascript js")},
	{ID: "apple", Name: "Apple Developer", Get: appleGet, Noise: noise("apple developer docs doc documentation reference swiftui swift uikit appkit foundation storekit ios macos")},
	{ID: "wikipedia", Name: "Wikipedia", Get: wikipediaGet, Noise: noise("wikipedia wiki who what when where was is are were the docs doc documentation reference")},
	{ID: "stackoverflow", Name: "Stack Overflow", Get: stackoverflowGet, Fuzzy: true, Noise: noise("stackoverflow so")},
	{ID: "npm", Name: "npm", Get: npmGet, Noise: pkgNoise},
	{ID: "pypi", Name: "PyPI", Get: pypiGet, Noise: pkgNoise},
	{ID: "crates", Name: "crates.io", Get: cratesGet, Noise: pkgNoise},
	{ID: "man", Name: "man", Get: manGet, Noise: noise("man manual manpage page flags flag options option for of the command")},
	{ID: "weather", Name: "wttr.in", Get: weatherGet, EmptyOK: true, Explicit: true, Noise: noise("weather forecast temperature in for at today tomorrow the will it rain")},
	{ID: "notes", Name: "Notes", Get: notesGet, Explicit: true, Noise: noise("my notes note vault in from on about")},
}
