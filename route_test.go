package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var heldOut = []example{
	{"antonyms for lazy", "thesaurus"}, {"a different word for smart", "thesaurus"}, {"thesaurus cold", "thesaurus"},
	{"define obsequious", "dictionary"}, {"what does sycophant mean", "dictionary"}, {"verbose meaning", "dictionary"}, {"etymology of window", "etymology"},
	{"std::string", "cppreference"}, {"c++ variant", "cppreference"}, {"make_unique", "cppreference"}, {"std::array docs", "cppreference"}, {"lower_bound", "cppreference"},
	{"python enumerate", "python"}, {"datetime.timedelta", "python"}, {"python argparse", "python"}, {"shutil.copy", "python"}, {"collections.Counter", "python"},
	{"rust HashSet", "rust"}, {"Option::map", "rust"}, {"rust Rc", "rust"}, {"Iterator::collect", "rust"}, {"std::fs::read_to_string", "rust"},
	{"Array.prototype.filter", "mdn"}, {"css transform", "mdn"}, {"html canvas element", "mdn"}, {"Promise.race", "mdn"}, {"javascript set", "mdn"}, {"css media queries", "mdn"},
	{"swiftui TabView", "apple"}, {"NavigationSplitView", "apple"}, {"UICollectionView", "apple"}, {"swiftui sheet", "apple"},
	{"who is ada lovelace", "wikipedia"}, {"roman empire", "wikipedia"}, {"capital of peru", "wikipedia"}, {"what is entropy", "wikipedia"}, {"napoleon bonaparte", "wikipedia"}, {"great wall of china", "wikipedia"},
	{"how to merge two dicts in python", "stackoverflow"}, {"why does git say detached head", "stackoverflow"}, {"how do i squash commits", "stackoverflow"}, {"fix cors error in express", "stackoverflow"},
	{"npm chalk", "npm"}, {"npm package axios", "npm"}, {"pypi flask", "pypi"}, {"pip install rich", "pypi"}, {"crates rand", "crates"}, {"cargo add axum", "crates"},
	{"man awk", "man"}, {"grep man page", "man"}, {"tar options", "man"}, {"weather vancouver", "weather"}, {"forecast berlin", "weather"}, {"my notes on cloudflare", "notes"},
}

var fresh = []example{
	{"words like angry", "thesaurus"}, {"meaning of serendipity", "dictionary"}, {"std::map", "cppreference"}, {"c++ std::span", "cppreference"}, {"static_assert", "cppreference"},
	{"python zip", "python"}, {"itertools.chain", "python"}, {"os.environ", "python"}, {"rust Cow", "rust"}, {"Vec::with_capacity", "rust"}, {"HashMap::entry", "rust"},
	{"css animation", "mdn"}, {"Object.keys", "mdn"}, {"html details element", "mdn"}, {"swiftui Form", "apple"}, {"UIButton", "apple"}, {"URLRequest", "apple"},
	{"cleopatra", "wikipedia"}, {"ottoman empire", "wikipedia"}, {"who invented the telephone", "wikipedia"}, {"pythagorean theorem", "wikipedia"}, {"amazon river", "wikipedia"},
	{"how to parse json in go", "stackoverflow"}, {"why is my docker container exiting", "stackoverflow"}, {"npm dayjs", "npm"}, {"pypi pydantic", "pypi"}, {"crates regex", "crates"},
	{"man sed", "man"}, {"weather in tokyo", "weather"}, {"notes about taxes", "notes"},
}

var last = []example{
	{"synonyms for tenacious", "thesaurus"}, {"define recalcitrant", "dictionary"}, {"std::deque", "cppreference"}, {"condition_variable", "cppreference"},
	{"python heapq", "python"}, {"logging.getLogger", "python"}, {"rust BTreeSet", "rust"}, {"Option::unwrap_or", "rust"}, {"Math.floor", "mdn"}, {"css z-index", "mdn"},
	{"javascript async await", "mdn"}, {"swiftui ScrollView", "apple"}, {"UILabel", "apple"}, {"genghis khan", "wikipedia"}, {"mitochondria", "wikipedia"},
	{"when did the berlin wall fall", "wikipedia"}, {"how to kill a process on a port", "stackoverflow"}, {"npm prettier", "npm"}, {"man ssh", "man"}, {"weather chicago", "weather"},
}

var goQueries = []example{
	{"strings.Split", "go"}, {"context.WithTimeout", "go"}, {"http.NewRequest", "go"}, {"json.Marshal", "go"}, {"go time", "go"}, {"golang sync mutex", "go"}, {"go slices", "go"},
	{"pathlib.Path", "python"}, {"subprocess.Popen", "python"}, {"how to read a file in go", "stackoverflow"},
}

func TestGo(t *testing.T) { score(t, goQueries) }

var added = []example{
	{"strings.Split", "go"}, {"context.WithTimeout", "go"}, {"http.NewRequest", "go"}, {"json.Marshal", "go"}, {"go time", "go"}, {"golang sync mutex", "go"}, {"github.com/spf13/cobra", "go"},
	{"pathlib.Path", "python"}, {"subprocess.Popen", "python"}, {"how to read a file in go", "stackoverflow"},
	{"sed examples", "tldr"}, {"tokio::spawn", "rust"}, {"docs.rs reqwest", "docsrs"}, {"rust-lang/cargo", "github"}, {"net/http", "go"},
	{"arxiv mixture of experts", "arxiv"}, {"rfc 1149", "rfc"}, {"etymology of window", "etymology"}, {"250 eur to usd", "currency"},
	{"can i use flexbox gap", "caniuse"}, {"arch wiki bluetooth", "archwiki"}, {"mark twain quotes", "quotes"}, {"postgres upsert", "postgres"},
	{"sqlite window functions", "sqlite"}, {"git stash", "git"}, {"brew fzf", "brew"}, {"slang bussin", "urban"}, {"node crypto", "node"}, {"fs.mkdir", "node"},
	{"lua string.gsub", "lua"}, {"1,4,9,16,25", "oeis"}, {"hn zig", "hackernews"}, {"book foundation", "books"}, {"docker image alpine", "docker"},
	{"cloudflare queues", "cloudflare"}, {"how do i use cloudflare d1", "cloudflare"}, {"claude extended thinking", "claude"}, {"stripe connect", "stripe"},
	{"supabase realtime", "supabase"}, {"bun install", "bun"}, {"expo updates", "expo"}, {"useRef", "react"}, {"nextjs image", "nextjs"},
}

func TestAdded(t *testing.T)   { score(t, added) }
func TestLast(t *testing.T)    { score(t, last) }
func TestFresh(t *testing.T)   { score(t, fresh) }
func TestHeldOut(t *testing.T) { score(t, heldOut) }

func score(t *testing.T, set []example) {
	right := 0
	for _, e := range set {
		got := routeWith(e.Query, nil)
		if got[0] == e.Site {
			right++
		} else {
			t.Logf("%-40s want %-14s got %v", e.Query, e.Site, got)
		}
	}
	t.Logf("%d/%d", right, len(set))
	if right*100 < len(set)*90 {
		t.Fail()
	}
}

func TestTerm(t *testing.T) {
	for _, c := range []struct{ site, query, want string }{
		{"cppreference", "C++ unique_ptr documentation", "unique_ptr"},
		{"thesaurus", "synonyms for admonish", "admonish"},
		{"thesaurus", "synonyms for like", "like"},
		{"dictionary", "what does sycophant mean", "sycophant"},
		{"weather", "weather in new york", "new york"},
		{"weather", "weather", ""},
	} {
		if got := siteByID(c.site).term(c.query); got != c.want {
			t.Errorf("%s %q: got %q want %q", c.site, c.query, got, c.want)
		}
	}
}

func TestFallbacks(t *testing.T) {
	got := routeWith("rust", nil)
	for _, id := range got[1:] {
		if id == "weather" || id == "notes" {
			t.Fatalf("got %v", got)
		}
	}
	if got[len(got)-1] != "wikipedia" {
		t.Fatalf("got %v", got)
	}
}

func TestLearnedOverride(t *testing.T) {
	got := routeWith("rust", []example{{"rust", "wikipedia"}})
	if got[0] != "wikipedia" {
		t.Fatalf("got %v", got)
	}
}

func TestDocRanking(t *testing.T) {
	entries := []llmsEntry{
		{"Collect tax with Checkout", "https://docs.stripe.com/tax/checkout.md", ""},
		{"Build a Stripe-hosted checkout page", "https://docs.stripe.com/checkout/quickstart.md", ""},
		{"Test runner", "https://bun.com/docs/test/index.md", ""},
		{"DOM testing", "https://bun.com/docs/test/dom.md", ""},
	}
	for term, want := range map[string]string{"checkout": "Build a Stripe-hosted checkout page", "test": "Test runner"} {
		if got := llmsBest(entries, term); got == nil || got.title != want {
			t.Errorf("%s: got %v", term, got)
		}
	}
	if llmsBest(entries, "nonsensexyz") != nil {
		t.Error("expected no match")
	}
}

func TestNamedSitesDontFallBack(t *testing.T) {
	if got := routeWith("cloudflare nonsensexyz", nil); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestUnnamedProductsNeedARule(t *testing.T) {
	for _, q := range []string{"kv cache", "kv cache docs"} {
		got := routeWith(q, nil)
		for _, id := range got {
			if siteByID(id).Explicit {
				t.Errorf("%s: got %v", q, got)
			}
		}
		if got[len(got)-1] != "wikipedia" {
			t.Errorf("%s: got %v", q, got)
		}
	}
}

func TestCorrectionsReachNamedSites(t *testing.T) {
	got := classify("kv cache layers", []example{{"kv cache", "arxiv"}})
	if got[0] != "arxiv" {
		t.Fatalf("got %v", got)
	}
}

func TestWikipediaDropsDocWords(t *testing.T) {
	if got := siteByID("wikipedia").term("kv cache docs"); got != "kv cache" {
		t.Fatalf("got %q", got)
	}
}

func TestFuzzySitesAreNotFallbacks(t *testing.T) {
	for _, q := range []string{"flash attention", "kv cache", "raft consensus"} {
		got := routeWith(q, nil)
		for _, id := range got[1:] {
			if siteByID(id).Fuzzy {
				t.Errorf("%s: got %v", q, got)
			}
		}
	}
}

func TestNamedLanguageSkipsGuessing(t *testing.T) {
	got := routeWith("C++ enums", nil)
	if len(got) != 1 || got[0] != "cppreference" {
		t.Fatalf("got %v", got)
	}
}

func TestSlowHostTimesOut(t *testing.T) {
	stop := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-stop }))
	defer srv.Close()
	defer close(stop)
	start := time.Now()
	_, err := fetch(srv.URL+"/slow", req{timeout: 100 * time.Millisecond, ttl: time.Nanosecond})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("took %v", time.Since(start))
	}
}
