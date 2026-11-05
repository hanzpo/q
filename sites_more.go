package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type llmsEntry struct{ title, url, desc string }

var (
	llmsLine   = regexp.MustCompile(`(?m)^\s*- \[([^\]]+)\]\((\S+?)\)(?:\s*[:-]\s*(.*))?$`)
	frontBlock = regexp.MustCompile(`(?s)\A---\n.*?\n---\n`)
)

func llmsParse(b []byte) []llmsEntry {
	var out []llmsEntry
	for _, m := range llmsLine.FindAllSubmatch(b, -1) {
		out = append(out, llmsEntry{string(m[1]), string(m[2]), string(m[3])})
	}
	return out
}

func pathSegments(link string) []string {
	if u, err := url.Parse(link); err == nil && u.Path != "" {
		link = u.Path
	}
	link = strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(link), ".md"), "/index")
	var out []string
	for _, p := range strings.Split(link, "/") {
		if p != "" && p != "docs" && p != "en" {
			out = append(out, p)
		}
	}
	return out
}

var agentLines = []string{
	"[skip to content]", "> documentation index", "> fetch the complete documentation index", "> use this file to discover",
	"> for an index of all documentation", "this documentation is available as markdown", "last updated ",
}

func stripAgentLines(src string) string {
	var out []string
	for _, l := range strings.Split(src, "\n") {
		low := strings.ToLower(strings.TrimSpace(l))
		drop := false
		for _, p := range agentLines {
			drop = drop || strings.HasPrefix(low, p)
		}
		if !drop {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

func llmsBest(entries []llmsEntry, term string) *llmsEntry {
	words := strings.Fields(strings.ToLower(term))
	want := strings.ToLower(term)
	slug := strings.Join(words, "-")
	var best *llmsEntry
	bestScore := 0
	for i := range entries {
		e := &entries[i]
		title, link, desc := strings.ToLower(e.title), strings.ToLower(e.url), strings.ToLower(e.desc)
		score := 0
		all := true
		for _, w := range words {
			s := 0
			if strings.Contains(title, w) {
				s += 3
			}
			if strings.Contains(link, w) {
				s += 2
			}
			if strings.Contains(desc, w) {
				s++
			}
			all = all && s > 0
			score += s
		}
		if !all {
			continue
		}
		if title == want {
			score += 100
		}
		segs := pathSegments(e.url)
		for j, seg := range segs {
			if seg == slug {
				score += max(60-20*j, 0)
				if j == len(segs)-1 {
					score += 10
				}
				if len(segs) == 1 {
					score += 150
				}
				break
			}
		}
		if score > bestScore || (score == bestScore && best != nil && len(e.url) < len(best.url)) {
			best, bestScore = e, score
		}
	}
	return best
}

func llmsPage(e *llmsEntry, width int) (*Page, error) {
	u := e.url
	if !strings.HasSuffix(u, ".md") {
		u = strings.TrimRight(u, "/") + ".md"
	}
	b, err := fetch(u, req{})
	if err != nil {
		return nil, err
	}
	if b == nil && strings.HasSuffix(u, "/index.md") {
		u = strings.TrimSuffix(u, "/index.md") + ".md"
		if b, err = fetch(u, req{}); err != nil {
			return nil, err
		}
	}
	if b == nil {
		if b, err = fetch(e.url, req{}); err != nil || b == nil {
			return nil, err
		}
	}
	src := string(b)
	web := strings.TrimSuffix(strings.TrimSuffix(u, ".md"), "/index")
	if head := strings.ToLower(strings.TrimSpace(src[:min(len(src), 200)])); strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html") {
		doc, err := parseDoc(b)
		if err != nil {
			return nil, err
		}
		main := doc.Find("main, article").First()
		return &Page{e.title, web, renderSel(main, web, width)}, nil
	}
	return &Page{e.title, web, renderMD(stripAgentLines(frontBlock.ReplaceAllString(src, "")), web, width)}, nil
}

func llmsSite(index string) func(string, int) (*Page, error) {
	return func(term string, width int) (*Page, error) {
		b, err := fetch(index, req{})
		if err != nil || b == nil {
			return nil, err
		}
		e := llmsBest(llmsParse(b), term)
		if e == nil {
			return nil, nil
		}
		return llmsPage(e, width)
	}
}

var sitemapLoc = regexp.MustCompile(`<loc>([^<]+)</loc>`)

func sitemapSite(index string) func(string, int) (*Page, error) {
	return func(term string, width int) (*Page, error) {
		b, err := fetch(index, req{ttl: month})
		if err != nil || b == nil {
			return nil, err
		}
		var entries []llmsEntry
		for _, m := range sitemapLoc.FindAllSubmatch(b, -1) {
			link := string(m[1])
			segs := pathSegments(link)
			if len(segs) > 0 {
				entries = append(entries, llmsEntry{strings.ReplaceAll(segs[len(segs)-1], "-", " "), link, ""})
			}
		}
		e := llmsBest(entries, term)
		if e == nil {
			return nil, nil
		}
		return llmsPage(e, width)
	}
}

var cfCore = []string{"workers", "pages", "r2", "d1", "kv", "durable-objects", "queues"}

func cloudflareGet(term string, width int) (*Page, error) {
	const root = "https://developers.cloudflare.com/"
	b, err := fetch(root+"llms.txt", req{})
	if err != nil || b == nil {
		return nil, err
	}
	lists := map[string]bool{}
	for _, p := range cfCore {
		lists[root+p+"/llms.txt"] = true
	}
	for _, e := range llmsParse(b) {
		if !strings.HasSuffix(e.url, "/llms.txt") {
			continue
		}
		for _, w := range strings.Fields(strings.ToLower(term)) {
			if strings.Contains(strings.ToLower(e.title+" "+e.url), w) {
				lists[e.url] = true
			}
		}
	}
	var entries []llmsEntry
	for u := range lists {
		list, err := fetch(u, req{ttl: month})
		if err == nil && list != nil {
			entries = append(entries, llmsParse(list)...)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].url < entries[j].url })
	e := llmsBest(entries, term)
	if e == nil {
		return nil, nil
	}
	return llmsPage(e, width)
}

func tldrGet(term string, width int) (*Page, error) {
	name := strings.ReplaceAll(strings.ToLower(term), " ", "-")
	for _, dir := range []string{"common", "osx", "linux"} {
		u := "https://raw.githubusercontent.com/tldr-pages/tldr/main/pages/" + dir + "/" + name + ".md"
		b, err := fetch(u, req{ttl: month})
		if err != nil {
			return nil, err
		}
		if b != nil {
			web := "https://tldr.inbrowser.app/pages/" + dir + "/" + name
			return &Page{name, web, renderMD(string(b), web, width)}, nil
		}
	}
	return nil, nil
}

func githubGet(term string, width int) (*Page, error) {
	repo := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(term, "https://"), "github.com/"), "/")
	if strings.Count(repo, "/") != 1 {
		return nil, nil
	}
	web := "https://github.com/" + repo
	for _, f := range []string{"README.md", "readme.md", "Readme.md"} {
		b, err := fetch("https://raw.githubusercontent.com/"+repo+"/HEAD/"+f, req{})
		if err != nil {
			return nil, err
		}
		if b != nil {
			return &Page{repo, web, renderMD(string(b), web+"/blob/HEAD/", width)}, nil
		}
	}
	return nil, nil
}

var arxivID = regexp.MustCompile(`^\d{4}\.\d{4,5}(v\d+)?$`)

func arxivGet(term string, width int) (*Page, error) {
	q := "id_list=" + term
	if !arxivID.MatchString(term) {
		q = "search_query=" + url.QueryEscape(`ti:"`+term+`"`) + "&sortBy=relevance"
	}
	b, err := fetch("https://export.arxiv.org/api/query?max_results=20&"+q, req{ua: apiUA, timeout: time.Minute})
	if err != nil || b == nil {
		return nil, err
	}
	var feed struct {
		Entries []struct {
			ID        string `xml:"id"`
			Title     string `xml:"title"`
			Summary   string `xml:"summary"`
			Published string `xml:"published"`
			Authors   []struct {
				Name string `xml:"name"`
			} `xml:"author"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(b, &feed); err != nil {
		return nil, err
	}
	if len(feed.Entries) == 0 {
		return nil, nil
	}
	e := feed.Entries[0]
	for _, c := range feed.Entries {
		if strings.EqualFold(strings.Join(strings.Fields(c.Title), " "), term) {
			e = c
			break
		}
	}
	var names []string
	for _, a := range e.Authors {
		names = append(names, a.Name)
	}
	var segs []seg
	segs = append(segs, seg{strings.Join(strings.Fields(e.Summary), " "), style{}})
	head := dim(strings.Join(names, ", ") + ", " + e.Published[:min(10, len(e.Published))])
	return &Page{strings.Join(strings.Fields(e.Title), " "), strings.Replace(e.ID, "http://", "https://", 1), head + "\n\n" + strings.Join(wrap(segs, width), "\n")}, nil
}

var digits = regexp.MustCompile(`\d+`)

func rfcGet(term string, width int) (*Page, error) {
	n := digits.FindString(term)
	if n == "" {
		return nil, nil
	}
	u := "https://www.rfc-editor.org/rfc/rfc" + n + ".txt"
	b, err := fetch(u, req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	return &Page{"RFC " + n, u, strings.TrimSpace(strings.ReplaceAll(string(b), "\f", ""))}, nil
}

var etymologyHead = regexp.MustCompile(`^Etymology`)

func etymologyGet(term string, width int) (*Page, error) {
	word := strings.ReplaceAll(term, " ", "_")
	api := "https://en.wiktionary.org/w/api.php?action=parse&format=json&formatversion=2&page=" + url.QueryEscape(word)
	var toc struct {
		Parse struct {
			Sections []struct {
				Line  string `json:"line"`
				Level string `json:"level"`
				Index string `json:"index"`
			} `json:"sections"`
		} `json:"parse"`
	}
	ok, err := fetchJSON(api+"&prop=sections", req{ua: apiUA}, &toc)
	if err != nil || !ok {
		return nil, err
	}
	u := "https://en.wiktionary.org/wiki/" + url.PathEscape(word)
	english := false
	var out []string
	for _, s := range toc.Parse.Sections {
		if s.Level == "2" {
			english = s.Line == "English"
		}
		if !english || !etymologyHead.MatchString(s.Line) {
			continue
		}
		var part struct {
			Parse struct {
				Text string `json:"text"`
			} `json:"parse"`
		}
		ok, err := fetchJSON(api+"&prop=text&section="+s.Index, req{ua: apiUA}, &part)
		if err != nil || !ok {
			return nil, err
		}
		doc, err := parseDoc([]byte(part.Parse.Text))
		if err != nil {
			return nil, err
		}
		body := doc.Find("body")
		body.Find(".mw-editsection, .sister-wikipedia, .thumb, style, h3, h4, .mw-heading").Remove()
		body.Find("ol, table").Remove()
		if t := renderSel(body, u, width); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return &Page{term, u + "#Etymology", strings.Join(out, "\n\n")}, nil
}

var currencyQuery = regexp.MustCompile(`(?i)^([\d.,]*)\s*([a-z]{3})\s+(?:in|to)\s+([a-z]{3})$`)

func currencyGet(term string, width int) (*Page, error) {
	m := currencyQuery.FindStringSubmatch(term)
	if m == nil {
		return nil, nil
	}
	amount := 1.0
	if m[1] != "" {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", ""), 64)
		if err != nil {
			return nil, err
		}
		amount = v
	}
	from, to := strings.ToUpper(m[2]), strings.ToUpper(m[3])
	u := "https://api.frankfurter.dev/v1/latest?base=" + from + "&symbols=" + to
	var data struct {
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	ok, err := fetchJSON(u, req{ttl: 30 * time.Minute}, &data)
	if err != nil || !ok || data.Rates[to] == 0 {
		return nil, err
	}
	rate := data.Rates[to]
	body := fmt.Sprintf("%s\n%s", bold(fmt.Sprintf("%.2f %s = %.2f %s", amount, from, amount*rate, to)), dim(fmt.Sprintf("1 %s = %.5f %s, ECB rate for %s", from, rate, to, data.Date)))
	return &Page{from + " to " + to, u, body}, nil
}

type caniuseFeature struct {
	Title       string                       `json:"title"`
	Description string                       `json:"description"`
	Notes       string                       `json:"notes"`
	Stats       map[string]map[string]string `json:"stats"`
}

func caniuseGet(term string, width int) (*Page, error) {
	b, err := fetch("https://raw.githubusercontent.com/Fyrd/caniuse/main/fulldata-json/data-2.0.json", req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	var all struct {
		Agents map[string]struct {
			Browser  string `json:"browser"`
			Current  string `json:"current_version"`
			Versions []struct {
				Version string `json:"version"`
			} `json:"version_list"`
		} `json:"agents"`
		Data map[string]caniuseFeature `json:"data"`
	}
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, err
	}
	var entries []llmsEntry
	for key, f := range all.Data {
		entries = append(entries, llmsEntry{f.Title, key, f.Description})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].url < entries[j].url })
	e := llmsBest(entries, term)
	if key := strings.Join(strings.Fields(strings.ToLower(term)), "-"); all.Data[key].Title != "" {
		e = &llmsEntry{url: key}
	}
	if e == nil {
		return nil, nil
	}
	f := all.Data[e.url]
	u := "https://caniuse.com/" + e.url
	var out []string
	for _, l := range wrap([]seg{{f.Description, style{}}}, width) {
		out = append(out, l)
	}
	out = append(out, "")
	for _, id := range []string{"chrome", "edge", "firefox", "safari", "ios_saf", "and_chr"} {
		a := all.Agents[id]
		current := f.Stats[id][a.Current]
		since := ""
		seen := false
		for i := len(a.Versions) - 1; i >= 0; i-- {
			v := a.Versions[i].Version
			if v == a.Current {
				seen = true
			}
			if !seen {
				continue
			}
			if !strings.HasPrefix(f.Stats[id][v], "y") {
				break
			}
			since = v
		}
		label := "unknown"
		switch {
		case strings.HasPrefix(current, "y") && since != "":
			label = "yes, since " + since
		case strings.HasPrefix(current, "y"):
			label = "yes"
		case strings.HasPrefix(current, "a"):
			label = "partial"
		case strings.HasPrefix(current, "n"):
			label = "no"
		}
		out = append(out, fmt.Sprintf("%-20s%s", a.Browser, label))
	}
	if f.Notes != "" {
		out = append(out, "")
		out = append(out, wrap([]seg{{f.Notes, style{}}}, width)...)
	}
	return &Page{f.Title, u, strings.Join(out, "\n")}, nil
}

func archwikiGet(term string, width int) (*Page, error) {
	var found struct {
		Query struct {
			Search []struct {
				Title string `json:"title"`
			} `json:"search"`
		} `json:"query"`
	}
	ok, err := fetchJSON("https://wiki.archlinux.org/api.php?action=query&list=search&format=json&srlimit=1&srsearch="+url.QueryEscape(term), req{ua: apiUA}, &found)
	if err != nil || !ok || len(found.Query.Search) == 0 {
		return nil, err
	}
	title := found.Query.Search[0].Title
	u := "https://wiki.archlinux.org/title/" + url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	b, err := fetch(u, req{})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	body := doc.Find("#mw-content-text")
	body.Find(".mw-editsection, #toc, .toc, .catlinks, .printfooter, .archwiki-template-meta-related-articles-start").Remove()
	return &Page{title, u, renderSel(body, u, width)}, nil
}

var pgEntry = regexp.MustCompile(`<dt[^>]*>([^,<]+), <a class="indexterm" href="([^"]+)"`)

func postgresGet(term string, width int) (*Page, error) {
	const root = "https://www.postgresql.org/docs/current/"
	b, err := fetch(root+"bookindex.html", req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	want := strings.ToLower(term)
	href := ""
	for _, m := range pgEntry.FindAllSubmatch(b, -1) {
		if strings.ToLower(strings.TrimSpace(string(m[1]))) == want {
			href = string(m[2])
			break
		}
	}
	if href == "" {
		return nil, nil
	}
	u := root + href
	file, id, _ := strings.Cut(href, "#")
	page, err := fetch(root+file, req{})
	if err != nil || page == nil {
		return nil, err
	}
	doc, err := parseDoc(page)
	if err != nil {
		return nil, err
	}
	body := doc.Find("#docContent")
	body.Find(".navheader, .navfooter, #docComments, #comments").Remove()
	if id != "" {
		if t := doc.Find(`[id="` + id + `"]`).First(); goquery.NodeName(t) == "div" || goquery.NodeName(t) == "section" {
			return &Page{term, u, renderSel(t, u, width)}, nil
		}
	}
	return &Page{term, u, renderSel(body, u, width)}, nil
}

var sqliteLink = regexp.MustCompile(`<a href=['"](lang_[a-z_]+\.html)['"]>([^<]+)</a>`)

func sqliteGet(term string, width int) (*Page, error) {
	b, err := fetch("https://sqlite.org/lang.html", req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	var entries []llmsEntry
	for _, m := range sqliteLink.FindAllSubmatch(b, -1) {
		entries = append(entries, llmsEntry{string(m[2]), string(m[1]), ""})
	}
	e := llmsBest(entries, term)
	if e == nil {
		return nil, nil
	}
	u := "https://sqlite.org/" + e.url
	page, err := fetch(u, req{})
	if err != nil || page == nil {
		return nil, err
	}
	doc, err := parseDoc(page)
	if err != nil {
		return nil, err
	}
	body := doc.Find("body")
	body.Find(".nosearch, .menu, .fancy-toc1, .fancy-toc2, .fancy-toc3, .imgcontainer").Remove()
	return &Page{e.title, u, renderSel(body, u, width)}, nil
}

func gitGet(term string, width int) (*Page, error) {
	cmd := strings.TrimPrefix(strings.ReplaceAll(strings.ToLower(term), " ", "-"), "git-")
	u := "https://git-scm.com/docs/git-" + cmd
	b, err := fetch(u, req{})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	body := doc.Find("#main")
	body.Find("#dropdowns, .versions, #search-container, .sidebar, aside").Remove()
	return &Page{"git " + cmd, u, renderSel(body, u, width)}, nil
}

func brewGet(term string, width int) (*Page, error) {
	name := strings.ReplaceAll(strings.ToLower(term), " ", "-")
	var f struct {
		Name     string `json:"name"`
		Desc     string `json:"desc"`
		Homepage string `json:"homepage"`
		License  string `json:"license"`
		Caveats  string `json:"caveats"`
		Versions struct {
			Stable string `json:"stable"`
		} `json:"versions"`
		Deps []string `json:"dependencies"`
	}
	u := "https://formulae.brew.sh/formula/" + name
	ok, err := fetchJSON("https://formulae.brew.sh/api/formula/"+url.PathEscape(name)+".json", req{}, &f)
	if err != nil {
		return nil, err
	}
	if ok {
		out := []string{f.Desc, "", "version   " + f.Versions.Stable, "homepage  " + f.Homepage, "license   " + f.License}
		if len(f.Deps) > 0 {
			out = append(out, "depends   "+strings.Join(f.Deps, ", "))
		}
		out = append(out, "", "brew install "+f.Name)
		if f.Caveats != "" {
			out = append(out, "", f.Caveats)
		}
		return &Page{f.Name, u, strings.Join(out, "\n")}, nil
	}
	var c struct {
		Token    string `json:"token"`
		Desc     string `json:"desc"`
		Homepage string `json:"homepage"`
		Version  string `json:"version"`
	}
	ok, err = fetchJSON("https://formulae.brew.sh/api/cask/"+url.PathEscape(name)+".json", req{}, &c)
	if err != nil || !ok {
		return nil, err
	}
	body := strings.Join([]string{c.Desc, "", "version   " + c.Version, "homepage  " + c.Homepage, "", "brew install --cask " + c.Token}, "\n")
	return &Page{c.Token, "https://formulae.brew.sh/cask/" + name, body}, nil
}

func urbanGet(term string, width int) (*Page, error) {
	var data struct {
		List []struct {
			Word       string `json:"word"`
			Definition string `json:"definition"`
			Example    string `json:"example"`
			Author     string `json:"author"`
			Permalink  string `json:"permalink"`
			Up         int    `json:"thumbs_up"`
		} `json:"list"`
	}
	ok, err := fetchJSON("https://api.urbandictionary.com/v0/define?term="+url.QueryEscape(term), req{ua: apiUA}, &data)
	if err != nil || !ok || len(data.List) == 0 {
		return nil, err
	}
	best := data.List[0]
	for _, d := range data.List {
		if d.Up > best.Up {
			best = d
		}
	}
	clean := strings.NewReplacer("[", "", "]", "", "\r", "").Replace
	body := dim(strings.TrimSpace(best.Author)) + "\n\n" + strings.Join(wrap([]seg{{clean(best.Definition), style{}}}, width), "\n")
	if best.Example != "" {
		body += "\n\n" + strings.Join(wrap([]seg{{clean(best.Example), style{i: true}}}, width), "\n")
	}
	return &Page{best.Word, best.Permalink, body}, nil
}

func nodeFind(v any, match func(map[string]any) bool) map[string]any {
	switch x := v.(type) {
	case []any:
		for _, i := range x {
			if m := nodeFind(i, match); m != nil {
				return m
			}
		}
	case map[string]any:
		if _, desc := x["desc"].(string); desc && match(x) {
			return x
		}
		for _, k := range []string{"modules", "miscs", "classes", "methods", "properties", "events", "globals"} {
			if m := nodeFind(x[k], match); m != nil {
				return m
			}
		}
	}
	return nil
}

func nodeGet(term string, width int) (*Page, error) {
	mod, member, _ := strings.Cut(strings.ReplaceAll(strings.ToLower(term), " ", "."), ".")
	original := strings.ReplaceAll(term, " ", ".")
	if i := strings.IndexByte(original, '.'); i >= 0 {
		member = original[i+1:]
	}
	web := "https://nodejs.org/api/" + mod + ".html"
	if member != "" {
		var data map[string]any
		ok, err := fetchJSON("https://nodejs.org/api/"+mod+".json", req{ttl: month}, &data)
		if err != nil || !ok {
			return nil, err
		}
		exact := strings.ToLower(mod + "." + member)
		m := nodeFind(data, func(x map[string]any) bool {
			raw, _ := x["textRaw"].(string)
			return strings.HasPrefix(strings.ToLower(strings.Trim(raw, "`")), exact)
		})
		if m == nil {
			m = nodeFind(data, func(x map[string]any) bool {
				name, _ := x["name"].(string)
				return strings.EqualFold(name, member)
			})
		}
		if m != nil {
			raw, _ := m["textRaw"].(string)
			desc, _ := m["desc"].(string)
			return &Page{strings.Trim(raw, "`"), web, renderHTML(desc, web, width)}, nil
		}
		return nil, nil
	}
	b, err := fetch(web, req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	body := doc.Find("#apicontent")
	body.Find(".srclink, .mark, .api_stability_2").Remove()
	return &Page{"node:" + mod, web, renderSel(body, web, width)}, nil
}

func luaGet(term string, width int) (*Page, error) {
	const u = "https://www.lua.org/manual/5.4/manual.html"
	b, err := fetch(u, req{ttl: month})
	if err != nil || b == nil {
		return nil, err
	}
	doc, err := parseDoc(b)
	if err != nil {
		return nil, err
	}
	name := strings.ReplaceAll(term, " ", ".")
	head := doc.Find(`a[name="pdf-` + name + `"]`).First().Closest("h3")
	if head.Length() == 0 {
		return nil, nil
	}
	var html strings.Builder
	head.NextUntil("h3, hr, h2").Each(func(_ int, s *goquery.Selection) {
		if h, err := goquery.OuterHtml(s); err == nil {
			html.WriteString(h)
		}
	})
	return &Page{text(head), u + "#pdf-" + name, renderHTML(html.String(), u, width)}, nil
}

func oeisGet(term string, width int) (*Page, error) {
	q := strings.ReplaceAll(term, " ", "")
	var hits []struct {
		Number  int      `json:"number"`
		Name    string   `json:"name"`
		Data    string   `json:"data"`
		Comment []string `json:"comment"`
		Formula []string `json:"formula"`
	}
	ok, err := fetchJSON("https://oeis.org/search?fmt=json&q="+url.QueryEscape(q), req{ua: apiUA}, &hits)
	if err != nil || !ok || len(hits) == 0 {
		return nil, err
	}
	h := hits[0]
	id := fmt.Sprintf("A%06d", h.Number)
	out := wrap([]seg{{h.Name, style{b: true}}}, width)
	out = append(out, "")
	out = append(out, wrap([]seg{{strings.ReplaceAll(h.Data, ",", ", "), style{code: true}}}, width)...)
	for _, c := range h.Comment[:min(6, len(h.Comment))] {
		out = append(out, "")
		out = append(out, wrap([]seg{{c, style{}}}, width)...)
	}
	return &Page{id, "https://oeis.org/" + id, strings.Join(out, "\n")}, nil
}

func hackernewsGet(term string, width int) (*Page, error) {
	var data struct {
		Hits []struct {
			Title    string `json:"title"`
			URL      string `json:"url"`
			Points   int    `json:"points"`
			Comments int    `json:"num_comments"`
			ID       string `json:"objectID"`
		} `json:"hits"`
	}
	ok, err := fetchJSON("https://hn.algolia.com/api/v1/search?tags=story&hitsPerPage=10&query="+url.QueryEscape(term), req{ttl: time.Hour}, &data)
	if err != nil || !ok || len(data.Hits) == 0 {
		return nil, err
	}
	var out []string
	for _, h := range data.Hits {
		thread := "https://news.ycombinator.com/item?id=" + h.ID
		link := h.URL
		if link == "" {
			link = thread
		}
		out = append(out, bold(hyperlink(h.Title, link)), dim(hyperlink(fmt.Sprintf("%d points, %d comments", h.Points, h.Comments), thread)), "")
	}
	return &Page{term, "https://hn.algolia.com/?q=" + url.QueryEscape(term), strings.TrimRight(strings.Join(out, "\n"), "\n")}, nil
}

func booksGet(term string, width int) (*Page, error) {
	var data struct {
		Docs []struct {
			Key     string   `json:"key"`
			Title   string   `json:"title"`
			Authors []string `json:"author_name"`
			Year    int      `json:"first_publish_year"`
		} `json:"docs"`
	}
	ok, err := fetchJSON("https://openlibrary.org/search.json?limit=1&q="+url.QueryEscape(term), req{ua: apiUA}, &data)
	if err != nil || !ok || len(data.Docs) == 0 {
		return nil, err
	}
	d := data.Docs[0]
	u := "https://openlibrary.org" + d.Key
	head := dim(fmt.Sprintf("%s, %d", strings.Join(d.Authors, ", "), d.Year))
	var work struct {
		Description json.RawMessage `json:"description"`
	}
	if ok, err := fetchJSON(u+".json", req{ua: apiUA}, &work); err != nil || !ok {
		return &Page{d.Title, u, head}, nil
	}
	desc := ""
	if json.Unmarshal(work.Description, &desc) != nil {
		var box struct {
			Value string `json:"value"`
		}
		if json.Unmarshal(work.Description, &box) == nil {
			desc = box.Value
		}
	}
	return &Page{d.Title, u, head + "\n\n" + renderMD(desc, u, width)}, nil
}

func dockerGet(term string, width int) (*Page, error) {
	repo := strings.ToLower(term)
	web := "https://hub.docker.com/r/" + repo
	if !strings.Contains(repo, "/") {
		web = "https://hub.docker.com/_/" + repo
		repo = "library/" + repo
	}
	var data struct {
		Description string `json:"description"`
		Full        string `json:"full_description"`
		Stars       int    `json:"star_count"`
		Pulls       int64  `json:"pull_count"`
	}
	ok, err := fetchJSON("https://registry.hub.docker.com/v2/repositories/"+repo, req{ua: apiUA}, &data)
	if err != nil || !ok {
		return nil, err
	}
	body := dim(fmt.Sprintf("%d stars, %d pulls", data.Stars, data.Pulls)) + "\n\n"
	if data.Full != "" {
		body += renderMD(data.Full, web, width)
	} else {
		body += data.Description
	}
	return &Page{repo, web, body}, nil
}

const docWords = "docs doc documentation reference guide how do i to use using in with a an the for"

func init() {
	sites = append(sites,
		&Site{ID: "tldr", Name: "tldr pages", Get: tldrGet, Noise: noise("tldr example examples of for cheatsheet")},
		&Site{ID: "docsrs", Name: "docs.rs", Get: docsrsGet, Noise: noise("docs.rs docs doc documentation rust"), Explicit: true},
		&Site{ID: "github", Name: "GitHub", Get: githubGet, Noise: noise("github gh repo readme"), Explicit: true},
		&Site{ID: "arxiv", Name: "arXiv", Get: arxivGet, Noise: noise("arxiv paper papers on about"), Explicit: true},
		&Site{ID: "rfc", Name: "RFC Editor", Get: rfcGet, Explicit: true},
		&Site{ID: "etymology", Name: "Wiktionary", Get: etymologyGet, Noise: noise("etymology origin of the word")},
		&Site{ID: "currency", Name: "Frankfurter", Get: currencyGet, Explicit: true},
		&Site{ID: "caniuse", Name: "caniuse", Get: caniuseGet, Noise: noise("can i use caniuse browser support for"), Explicit: true},
		&Site{ID: "archwiki", Name: "ArchWiki", Get: archwikiGet, Noise: noise("arch archwiki linux wiki"), Explicit: true},
		&Site{ID: "quotes", Name: "Wikiquote", Get: func(t string, w int) (*Page, error) { return mediawiki("en.wikiquote.org", t, w) }, Noise: noise("quote quotes by from wikiquote"), Explicit: true},
		&Site{ID: "postgres", Name: "PostgreSQL docs", Get: postgresGet, Noise: noise("postgres postgresql psql docs doc documentation")},
		&Site{ID: "sqlite", Name: "SQLite docs", Get: sqliteGet, Noise: noise("sqlite sqlite3 docs doc documentation")},
		&Site{ID: "git", Name: "git-scm", Get: gitGet, Noise: noise("git docs doc documentation")},
		&Site{ID: "brew", Name: "Homebrew", Get: brewGet, Noise: noise("brew homebrew info formula cask install")},
		&Site{ID: "urban", Name: "Urban Dictionary", Get: urbanGet, Noise: noise("urban dictionary urbandictionary slang"), Explicit: true},
		&Site{ID: "node", Name: "Node.js docs", Get: nodeGet, Noise: noise("node nodejs node.js docs doc documentation module")},
		&Site{ID: "lua", Name: "Lua manual", Get: luaGet, Noise: noise("lua docs doc documentation manual")},
		&Site{ID: "oeis", Name: "OEIS", Get: oeisGet, Noise: noise("oeis sequence"), Explicit: true},
		&Site{ID: "hackernews", Name: "Hacker News", Get: hackernewsGet, Noise: noise("hn hacker news hackernews"), Explicit: true},
		&Site{ID: "books", Name: "Open Library", Get: booksGet, Noise: noise("book books by openlibrary"), Explicit: true},
		&Site{ID: "docker", Name: "Docker Hub", Get: dockerGet, Noise: noise("docker hub image"), Explicit: true},
		&Site{ID: "cloudflare", Name: "Cloudflare docs", Get: cloudflareGet, Noise: noise("cloudflare workers " + docWords), Explicit: true},
		&Site{ID: "claude", Name: "Claude docs", Get: llmsSite("https://platform.claude.com/llms.txt"), Noise: noise("claude anthropic api " + docWords), Explicit: true},
		&Site{ID: "stripe", Name: "Stripe docs", Get: llmsSite("https://docs.stripe.com/llms.txt"), Noise: noise("stripe " + docWords), Explicit: true},
		&Site{ID: "supabase", Name: "Supabase docs", Get: sitemapSite("https://supabase.com/docs/sitemap.xml"), Noise: noise("supabase " + docWords), Explicit: true},
		&Site{ID: "bun", Name: "Bun docs", Get: llmsSite("https://bun.com/llms.txt"), Noise: noise("bun " + docWords), Explicit: true},
		&Site{ID: "expo", Name: "Expo docs", Get: llmsSite("https://docs.expo.dev/llms.txt"), Noise: noise("expo " + docWords), Explicit: true},
		&Site{ID: "react", Name: "React docs", Get: llmsSite("https://react.dev/llms.txt"), Noise: noise("react " + docWords), Explicit: true},
		&Site{ID: "nextjs", Name: "Next.js docs", Get: llmsSite("https://nextjs.org/docs/llms.txt"), Noise: noise("nextjs next.js next " + docWords), Explicit: true},
	)
}
