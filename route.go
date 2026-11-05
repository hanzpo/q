package main

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const learnedWeight = 8

type rule struct {
	re   *regexp.Regexp
	site string
}

func r(pattern, site string) rule { return rule{regexp.MustCompile(pattern), site} }

var rules = []rule{
	r(`(?i)\b(my notes|my vault|notes (on|about))\b`, "notes"),
	r(`(?i)\betymology\b|^origin of the word\b`, "etymology"),
	r(`(?i)^[\d.,]*\s*(usd|cad|eur|gbp|jpy|cny|aud|chf|inr|krw|hkd|sgd|nzd|mxn|brl|sek|nok|dkk|php|thb)\s+(in|to)\s+(usd|cad|eur|gbp|jpy|cny|aud|chf|inr|krw|hkd|sgd|nzd|mxn|brl|sek|nok|dkk|php|thb)$`, "currency"),
	r(`^\d+(\s*,\s*\d+){2,}$|(?i)\boeis\b`, "oeis"),
	r(`(?i)^rfc[\s-]*\d+$`, "rfc"),
	r(`(?i)\b(can ?i ?use|browser support)\b`, "caniuse"),
	r(`(?i)\b(urban ?dictionary|slang)\b`, "urban"),
	r(`(?i)\b(hn|hacker ?news)\b`, "hackernews"),
	r(`(?i)\barxiv\b|\bpapers? (on|about)\b`, "arxiv"),
	r(`(?i)\bquotes?\b|\bwikiquote\b`, "quotes"),
	r(`(?i)^(books?|openlibrary)\s|\sbook$`, "books"),
	r(`(?i)^(tldr)\s|\s(examples?|cheatsheet)$|^examples? (of|for)\s`, "tldr"),
	r(`(?i)^(github|gh|repo)\s+\S+/\S+$|^https?://github\.com/`, "github"),
	r(`^[\w-]+\.[a-z]+/\S+`, "go"),
	r(`(?i)^git[ -][a-z-]+( docs?)?$`, "git"),
	r(`(?i)^(brew|homebrew)\s`, "brew"),
	r(`(?i)\bdocker (hub|image)\b`, "docker"),
	r(`(?i)\barch ?(linux|wiki)\b`, "archwiki"),
	r(`(?i)\b(postgres|postgresql|psql)\b`, "postgres"),
	r(`(?i)\bsqlite3?\b`, "sqlite"),
	r(`(?i)\blua\b`, "lua"),
	r(`(?i)\bdocs\.rs\b`, "docsrs"),
	r(`(?i)\b(cloudflare|wrangler|durable objects?)\b`, "cloudflare"),
	r(`(?i)\b(claude|anthropic)\b`, "claude"),
	r(`(?i)\bstripe\b`, "stripe"),
	r(`(?i)\bsupabase\b`, "supabase"),
	r(`(?i)\bbun\b`, "bun"),
	r(`(?i)\bexpo\b`, "expo"),
	r(`(?i)\bnext\.?js\b`, "nextjs"),
	r(`(?i)\bnode(\.?js)?\b|^(fs|child_process|worker_threads|readline)\.\w+`, "node"),
	r(`(?i)\breact\b|^use[A-Z]\w+$`, "react"),
	r(`(?i)\b(synonyms?|antonyms?|thesaurus|another word|other words?|opposite of|words? like|ways to say)\b`, "thesaurus"),
	r(`(?i)^(define|definition of|meaning of)\b|\bwhat does \S+ mean\b|\bmeaning$`, "dictionary"),
	r(`(?i)^(man|manual for)\s|\bman ?page\b`, "man"),
	r(`(?i)\b(weather|forecast|temperature in|will it rain)\b`, "weather"),
	r(`\bstd::(collections|fs|io|sync|vec|env|net|rc|cell)\b`, "rust"),
	r(`(?i)\bcppreference\b|(^|\s)(c\+\+\d*|cpp)(\s|$)|\bstd::\w+`, "cppreference"),
	r(`^\w+::\w+`, "rust"),
	r(`(?i)\bmdn\b|^(css|html|javascript|js)\s`, "mdn"),
	r(`^(Array|Object|Promise|String|Number|Math|JSON|Map|Set|Date|Intl|Reflect)\.\w+`, "mdn"),
	r(`(?i)\b(pypi|pip install)\b`, "pypi"),
	r(`(?i)\b(npm|npx)\b`, "npm"),
	r(`(?i)\b(crates?(\.io)?|cargo add)\b`, "crates"),
	r(`(?i)\bstack ?overflow\b|^(how (do|can|to|should)|why (is|does|do|am|won't|can't))\b`, "stackoverflow"),
	r(`(?i)\bwikipedia\b|^(who|when|where|what (is|was|are))\b`, "wikipedia"),
	r(`(?i)\bgolang\b|\bpkg\.go\.dev\b|^go\s+\S`, "go"),
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

func symbolSite(query string) string {
	if strings.ContainsAny(query, " \t") {
		return ""
	}
	if strings.Count(query, "/") == 1 {
		if idx, err := goStd(); err == nil && idx[query] != "" {
			return "go"
		}
		return "github"
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
		if idx, err := cppIndex(); err == nil && idx[query] != "" {
			return "cppreference"
		}
	}
	return ""
}

func route(query string) []string { return routeWith(query, learned()) }

func routeWith(query string, extra []example) []string {
	first := ""
	for _, e := range extra {
		if strings.EqualFold(e.Query, query) {
			first = e.Site
		}
	}
	if first == "" {
		for _, ru := range rules {
			if ru.re.MatchString(query) {
				first = ru.site
				break
			}
		}
	}
	if first == "" {
		first = symbolSite(query)
	}
	if first != "" && (siteByID(first).Explicit || namedTools[first]) {
		return []string{first}
	}
	corrected := map[string]bool{}
	for _, e := range extra {
		corrected[e.Site] = true
	}
	var order []string
	for _, l := range classify(query, extra) {
		if !siteByID(l).Explicit || corrected[l] {
			order = append(order, l)
		}
	}
	if first != "" {
		order = promote(first, order)
	}
	picked := order[:1]
	for _, l := range order[1:] {
		if first == "" && len(picked) < 2 && l != "wikipedia" && !siteByID(l).Explicit && !siteByID(l).Fuzzy {
			picked = append(picked, l)
		}
	}
	if picked[0] != "wikipedia" && !codeShape.MatchString(query) {
		picked = append(picked, "wikipedia")
	}
	return picked
}
