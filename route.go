package main

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const learnedWeight = 8

type strength int

const (
	strong strength = iota
	soft
	weak
)

type rule struct {
	re   *regexp.Regexp
	not  *regexp.Regexp
	site string
	kind strength
}

func r(pattern, site string) rule { return rule{re: regexp.MustCompile(pattern), site: site} }
func s(pattern, site string) rule {
	return rule{re: regexp.MustCompile(pattern), site: site, kind: soft}
}
func w(pattern, site string) rule {
	return rule{re: regexp.MustCompile(pattern), site: site, kind: weak}
}

var rules = []rule{
	r(`(?i)\b(my notes|my vault)\b`, "notes"),
	w(`(?i)\bnotes (on|about)\b`, "notes"),
	r(`(?i)\betymology\b|^origin of the word\b`, "etymology"),
	r(`(?i)^[\d.,]*\s*(usd|cad|eur|gbp|jpy|cny|aud|chf|inr|krw|hkd|sgd|nzd|mxn|brl|sek|nok|dkk|php|thb)\s+(in|to)\s+(usd|cad|eur|gbp|jpy|cny|aud|chf|inr|krw|hkd|sgd|nzd|mxn|brl|sek|nok|dkk|php|thb)$`, "currency"),
	r(`^\d+(\s*,\s*\d+){2,}$|(?i)\boeis\b`, "oeis"),
	r(`(?i)^rfc[\s-]*\d+$`, "rfc"),
	r(`(?i)\b(can ?i ?use|browser support)\b`, "caniuse"),
	r(`(?i)^(urban ?dictionary|slang)\s`, "urban"),
	r(`(?i)\b(hn|hacker ?news)\b`, "hackernews"),
	r(`(?i)\barxiv\b`, "arxiv"),
	w(`(?i)^papers? (on|about)\b`, "arxiv"),
	s(`(?i)\b(stocks?|shares?) (price|quote)s?\b|\bstock$|\bstock\s+\S+$|^(current |latest |live )?price (of |for )?\S|\S price$|\bprice of\b|\bticker\b|^\$[A-Za-z.]{1,6}$`, "stocks"),
	r(`(?i)\bwikiquote\b|^quotes? (by|from|about)\s`, "quotes"),
	w(`(?i)\S quotes$`, "quotes"),
	r(`(?i)^openlibrary\s|^books by\s`, "books"),
	w(`(?i)^book\s|\sbook$`, "books"),
	r(`(?i)^(tldr)\s|\s(examples?|cheatsheet)$|^examples? (of|for)\s`, "tldr"),
	r(`(?i)^(github|gh|repo)\s+\S+/\S+$|^https?://(www\.)?github\.com/`, "github"),
	r(`^@[\w-]+/[\w.-]+$`, "npm"),
	r(`^[\w-]+\.[a-z]+/\S+`, "go"),
	r(`(?i)^git[ -][a-z-]+( docs?)?$`, "git"),
	s(`(?i)^(brew|homebrew)\s`, "brew"),
	r(`(?i)\bdocker (hub|image)\b`, "docker"),
	r(`(?i)\barch ?(linux|wiki)\b`, "archwiki"),
	r(`(?i)\b(postgres|postgresql|psql)\b`, "postgres"),
	r(`(?i)\bsqlite3?\b`, "sqlite"),
	r(`(?i)\blua\b`, "lua"),
	r(`(?i)\bdocs\.rs\b`, "docsrs"),
	r(`(?i)\b(cloudflare|wrangler|durable objects?)\b`, "cloudflare"),
	s(`(?i)\b(claude|anthropic)\b`, "claude"),
	s(`(?i)\bstripe\b`, "stripe"),
	r(`(?i)\bsupabase\b`, "supabase"),
	s(`(?i)\bbun\b`, "bun"),
	s(`(?i)\bexpo\b`, "expo"),
	r(`(?i)\bnext\.?js\b`, "nextjs"),
	r(`^(fs|child_process|worker_threads|readline)\.\w+`, "node"),
	s(`(?i)\bnode(\.?js)?\b`, "node"),
	r(`^use[A-Z]\w+$`, "react"),
	s(`(?i)\breact\b`, "react"),
	r(`(?i)\b(synonyms?|antonyms?|thesaurus|another word|other words?|opposite of|words? like|ways to say)\b`, "thesaurus"),
	r(`(?i)^(define|definition of|dictionary)\s|\bwhat does \S+ mean\b|\bmeaning$`, "dictionary"),
	w(`(?i)^meaning of\b`, "dictionary"),
	r(`(?i)^manual for\s|\bman ?page\b`, "man"),
	w(`(?i)^man\s`, "man"),
	r(`(?i)\b(weather|forecast|will it rain)\b`, "weather"),
	{re: regexp.MustCompile(`(?i)\btemperature in\b`), not: regexp.MustCompile(`(?i)\b(kelvin|celsius|fahrenheit)\b`), site: "weather"},
	r(`\bstd::(collections|fs|io|sync|vec|env|net|rc|cell)\b`, "rust"),
	r(`(?i)\bcppreference\b|(^|\s)(c\+\+\d*|cpp)(\s|$)|\bstd::\w+`, "cppreference"),
	r(`^\w+::\w+`, "rust"),
	r(`(?i)\bmdn\b|^(css|html|javascript|js)\s`, "mdn"),
	r(`^(Array|Object|Promise|String|Number|Math|JSON|Map|Set|Date|Intl|Reflect)\.\w+`, "mdn"),
	r(`(?i)\b(pypi|pip install)\b`, "pypi"),
	r(`(?i)\b(npm|npx)\b`, "npm"),
	r(`(?i)\bcrates\.io\b|\bcargo add\b|\brust crate\b|^crates?\s`, "crates"),
	r(`(?i)\bstack ?overflow\b|^(how (do|can|to|should)|why (is|does|do|am|won't|can't))\b`, "stackoverflow"),
	r(`(?i)\bwikipedia\b|^(who|when|where|what (is|was|are))\b`, "wikipedia"),
	r(`(?i)\bgolang\b|\bpkg\.go\.dev\b`, "go"),
	w(`(?i)^go\s+\S`, "go"),
	r(`\b[Ss]wiftUI\b|\bswiftui\b|\bapple docs\b|^(UI|NS|HK|SK|URL)[A-Z]?\w+$`, "apple"),
}

var codeShape = regexp.MustCompile(`::|[._]|[a-z][A-Z]|^[A-Z]{2,}[a-z]`)

var namedTools = set("cppreference", "go", "postgres", "sqlite", "git", "lua", "node", "man", "brew")

var genericWords = regexp.MustCompile(`(?i)\b(docs?|documentation|reference)\b`)

var splitWords = regexp.MustCompile(`[^\p{L}\p{N}_+:.#]+`)

func features(q string) []string {
	s := strings.TrimSpace(strings.ToLower(genericWords.ReplaceAllString(q, " ")))
	var words []string
	for _, w := range splitWords.Split(s, -1) {
		if w != "" {
			words = append(words, w)
		}
	}
	var out []string
	for i, w := range words {
		out = append(out, "w:"+w)
		if i > 0 {
			out = append(out, "b:"+words[i-1]+" "+w)
		}
	}
	p := []rune(" " + s + " ")
	for i := 0; i+3 <= len(p); i++ {
		out = append(out, "c:"+string(p[i:i+3]))
	}
	return out
}

type example struct {
	Query string `json:"query"`
	Site  string `json:"site"`
}

func learnedPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "q", "learned.jsonl")
}

func learned() []example {
	f, err := os.Open(learnedPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []example
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e example
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out
}

func learn(query, site string) error {
	path := learnedPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(example{query, site})
	_, err = f.Write(append(b, '\n'))
	return err
}

func classify(query string, extra []example) []string {
	counts := map[string]map[string]float64{}
	totals := map[string]float64{}
	vocab := map[string]bool{}
	add := func(label, q string, w float64) {
		if counts[label] == nil {
			counts[label] = map[string]float64{}
		}
		for _, f := range features(q) {
			vocab[f] = true
			counts[label][f] += w
			totals[label] += w
		}
	}
	for label, qs := range examples {
		for _, q := range qs {
			add(label, q, 1)
		}
	}
	for _, e := range extra {
		add(e.Site, e.Query, learnedWeight)
	}
	const alpha = 0.1
	var fs []string
	knownWord := false
	for _, f := range features(query) {
		if vocab[f] {
			fs = append(fs, f)
			knownWord = knownWord || strings.HasPrefix(f, "w:")
		}
	}
	scores := map[string]float64{}
	labels := make([]string, 0, len(counts))
	for label, table := range counts {
		labels = append(labels, label)
		denom := totals[label] + alpha*float64(len(vocab))
		for _, f := range fs {
			scores[label] += math.Log((table[f] + alpha) / denom)
		}
	}
	sort.Slice(labels, func(i, j int) bool {
		if scores[labels[i]] != scores[labels[j]] {
			return scores[labels[i]] > scores[labels[j]]
		}
		return labels[i] < labels[j]
	})
	if !knownWord && !codeShape.MatchString(query) {
		return promote("wikipedia", labels)
	}
	return labels
}

func promote(first string, labels []string) []string {
	out := []string{first}
	for _, l := range labels {
		if l != first {
			out = append(out, l)
		}
	}
	return out
}

var cppMember = regexp.MustCompile(`^(\w+)(<.*>)?::\w+`)

func symbolSite(query string) string {
	if strings.ContainsAny(query, " \t") {
		return ""
	}
	if m := cppMember.FindStringSubmatch(query); m != nil {
		if idx, err := cppIndex(""); err == nil && idx[m[1]] != "" {
			return "cppreference"
		}
		return ""
	}
	if strings.Count(query, "/") >= 1 {
		path, _, _ := strings.Cut(query, ".")
		if idx, err := goStd(); err == nil && idx[path] != "" {
			return "go"
		}
		return ""
	}
	if strings.Contains(query, ".") {
		if idx, err := pyIndex(); err == nil && idx[query] != "" {
			return "python"
		}
		if goExported.MatchString(query) {
			pkg, _, _ := strings.Cut(query, ".")
			if idx, err := goStd(); err == nil && idx[pkg] != "" {
				return "go"
			}
		}
	}
	if strings.Contains(query, "_") {
		if idx, err := cppIndex(""); err == nil && idx[query] != "" {
			return "cppreference"
		}
	}
	return ""
}

var (
	goWord  = regexp.MustCompile(`(?i)^go\s+([\w/]+)`)
	manWord = regexp.MustCompile(`(?i)^man\s+(?:\d\s+)?(\S+)$`)
)

func confirmed(site, query string) bool {
	switch site {
	case "go":
		if m := goWord.FindStringSubmatch(query); m != nil {
			idx, err := goStd()
			return err == nil && idx[strings.ToLower(m[1])] != ""
		}
	case "man":
		if m := manWord.FindStringSubmatch(query); m != nil {
			return exec.Command("man", "-w", m[1]).Run() == nil
		}
	}
	return false
}

func routeWith(query string, extra []example) []string {
	for i := len(extra) - 1; i >= 0; i-- {
		if strings.EqualFold(extra[i].Query, query) {
			return []string{extra[i].Site}
		}
	}
	code := codeShape.MatchString(query) && !strings.HasPrefix(query, ".")
	if sym := symbolSite(query); sym != "" {
		return []string{sym}
	}
	var hits []rule
	for _, ru := range rules {
		if ru.re.MatchString(query) && (ru.not == nil || !ru.not.MatchString(query)) {
			hits = append(hits, ru)
		}
	}
	if len(hits) > 0 {
		head := hits[0]
		var order []string
		if head.kind == weak && !confirmed(head.site, query) || head.site == "wikipedia" {
			order = append(order, "wikititle")
		}
		for _, h := range hits {
			order = append(order, h.site)
		}
		if head.kind == soft {
			order = append(order, "wikititle")
		}
		order = unique(order)
		for _, h := range hits {
			if h.site == "stackoverflow" && !code {
				order = unique(append(order, "wikipedia"))
			}
		}
		if !siteByID(head.site).Explicit && !namedTools[head.site] && head.kind == strong && !code && head.site != "wikipedia" {
			order = append(order, "wikipedia")
		}
		return order
	}
	if strings.Count(query, "/") == 1 && !strings.Contains(query, " ") {
		return []string{"wikititle", "github", "wikipedia"}
	}
	if !code && !strings.Contains(query, " ") {
		if idx, err := pyIndex(); err == nil && idx[query] != "" {
			return []string{"wikititle", "python", "wikipedia"}
		}
	}
	corrected := map[string]bool{}
	for _, e := range extra {
		corrected[e.Site] = true
	}
	var ranked []string
	for _, l := range classify(query, extra) {
		if !siteByID(l).Explicit || corrected[l] {
			ranked = append(ranked, l)
		}
	}
	picked := ranked[:1]
	for _, l := range ranked[1:] {
		if len(picked) < 2 && l != "wikipedia" && !siteByID(l).Explicit && !siteByID(l).Fuzzy {
			picked = append(picked, l)
		}
	}
	if code {
		return picked
	}
	if picked[0] != "wikipedia" {
		picked = append(picked, "wikipedia")
	}
	return append([]string{"wikititle"}, picked...)
}

func route(query string) []string { return routeWith(query, learned()) }
