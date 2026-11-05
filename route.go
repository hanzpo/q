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
	r(`(?i)\b(synonyms?|antonyms?|thesaurus|another word|other words?|opposite of|words? like|ways to say)\b`, "thesaurus"),
	r(`(?i)^(define|definition of|meaning of|etymology of)\b|\bwhat does \S+ mean\b|\bmeaning$`, "dictionary"),
	r(`(?i)^(man|manual for)\s|\bman ?page\b`, "man"),
	r(`(?i)\b(weather|forecast)\b`, "weather"),
	r(`(?i)\b(my notes|my vault|notes (on|about))\b`, "notes"),
	r(`\bstd::(collections|fs|io|sync|vec|env|net|rc|cell)\b`, "rust"),
	r(`(?i)\bcppreference\b|(^|\s)(c\+\+\d*|cpp)(\s|$)|\bstd::\w+`, "cppreference"),
	r(`^\w+::\w+`, "rust"),
	r(`(?i)\bmdn\b`, "mdn"),
	r(`^(Array|Object|Promise|String|Number|Math|JSON|Map|Set|Date|Intl|Reflect)\.\w+`, "mdn"),
	r(`(?i)\b(pypi|pip install)\b`, "pypi"),
	r(`(?i)\b(npm|npx)\b`, "npm"),
	r(`(?i)\b(crates?(\.io)?|cargo add)\b`, "crates"),
	r(`(?i)\bstack ?overflow\b|^(how (do|can|to|should)|why (is|does|do|am|won't|can't))\b`, "stackoverflow"),
	r(`(?i)\bwikipedia\b|^(who|when|where|what (is|was|are))\b`, "wikipedia"),
	r(`\b[Ss]wiftUI\b|\bswiftui\b|\bapple docs\b|^(UI|NS|HK|SK|URL)[A-Z]?\w+$`, "apple"),
}

var codeShape = regexp.MustCompile(`::|[._]|[a-z][A-Z]|^[A-Z]{2,}[a-z]`)

var splitWords = regexp.MustCompile(`[^\p{L}\p{N}_+:.#]+`)

func features(q string) []string {
	s := strings.ToLower(q)
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
	if strings.Contains(query, ".") {
		if idx, err := pyIndex(); err == nil && idx[query] != "" {
			return "python"
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
	order := classify(query, extra)
	if first != "" {
		order = promote(first, order)
	}
	picked := order[:1]
	for _, l := range order[1:] {
		if len(picked) < 2 && l != "weather" && l != "notes" && l != "wikipedia" {
			picked = append(picked, l)
		}
	}
	if picked[0] != "wikipedia" && !codeShape.MatchString(query) {
		picked = append(picked, "wikipedia")
	}
	return picked
}
