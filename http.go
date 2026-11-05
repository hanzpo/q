package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/term"
)

const (
	browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15"
	apiUA     = "q-cli/0.1"
	day       = 24 * time.Hour
	month     = 30 * day
)

var (
	client = &http.Client{}
	runCtx = context.Background()
)

func waitingFor(url string) func() {
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		return func() {}
	}
	host := url
	if u, err := neturl.Parse(url); err == nil {
		host = u.Host
	}
	var mu sync.Mutex
	shown := false
	timer := time.AfterFunc(time.Second, func() {
		mu.Lock()
		defer mu.Unlock()
		shown = true
		fmt.Fprint(os.Stderr, "\r\x1b[2K"+dim("waiting for "+host))
	})
	return func() {
		timer.Stop()
		mu.Lock()
		defer mu.Unlock()
		if shown {
			fmt.Fprint(os.Stderr, "\r\x1b[2K")
		}
	}
}

type req struct {
	ttl     time.Duration
	ua      string
	timeout time.Duration
}

func cacheDir() string {
	base, err := os.UserCacheDir()
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		base = v
	} else if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, "q")
}

func fetch(url string, o req) ([]byte, error) {
	if o.ttl == 0 {
		o.ttl = day
	}
	if o.ua == "" {
		o.ua = browserUA
	}
	dir := cacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	sum := sha1.Sum([]byte(url))
	path := filepath.Join(dir, hex.EncodeToString(sum[:]))
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < o.ttl {
		return os.ReadFile(path)
	}
	if o.timeout == 0 {
		o.timeout = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(runCtx, o.timeout)
	defer cancel()
	defer waitingFor(url)()
	r, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", o.ua)
	res, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == 404 {
		return nil, nil
	}
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("%s returned %d", url, res.StatusCode)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	return body, os.WriteFile(path, body, 0o644)
}

func fetchJSON(url string, o req, into any) (bool, error) {
	b, err := fetch(url, o)
	if err != nil || b == nil {
		return false, err
	}
	return true, json.Unmarshal(b, into)
}
