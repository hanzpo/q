package main

import (
	"bytes"
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
	apiUA     = "q-cli/0.1 (https://github.com/hanzpo/q)"
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

func cachePath(url string) (string, error) {
	dir := cacheDir()
	sum := sha1.Sum([]byte(url))
	return filepath.Join(dir, hex.EncodeToString(sum[:])), os.MkdirAll(dir, 0o755)
}

func forget(url string) {
	if path, err := cachePath(url); err == nil {
		os.Remove(path)
	}
}

func save(path string, body []byte) {
	tmp, err := os.CreateTemp(filepath.Dir(path), "tmp")
	if err != nil {
		return
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(body)
	if tmp.Close() == nil && err == nil {
		os.Rename(tmp.Name(), path)
	}
}

func blocked(body []byte) bool {
	if len(bytes.TrimSpace(body)) == 0 {
		return true
	}
	head := bytes.ToLower(body[:min(len(body), 4096)])
	for _, mark := range []string{"<title>just a moment", "attention required! | cloudflare", "enable javascript and cookies to continue"} {
		if bytes.Contains(head, []byte(mark)) {
			return true
		}
	}
	return false
}

func download(url string, o req) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(runCtx, o.timeout)
	defer cancel()
	for attempt := 0; ; attempt++ {
		r, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return 0, nil, err
		}
		r.Header.Set("User-Agent", o.ua)
		res, err := client.Do(r)
		if err != nil {
			return 0, nil, err
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			return 0, nil, err
		}
		if (res.StatusCode == 429 || res.StatusCode == 503) && attempt == 0 {
			select {
			case <-time.After(1500 * time.Millisecond):
				continue
			case <-ctx.Done():
				return 0, nil, ctx.Err()
			}
		}
		return res.StatusCode, body, nil
	}
}

func fetch(url string, o req) ([]byte, error) {
	if o.ttl == 0 {
		o.ttl = day
	}
	if o.ua == "" {
		o.ua = browserUA
	}
	if o.timeout == 0 {
		o.timeout = 8 * time.Second
	}
	path, dirErr := cachePath(url)
	if dirErr == nil {
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < o.ttl {
			return os.ReadFile(path)
		}
		if st, err := os.Stat(path + ".404"); err == nil && time.Since(st.ModTime()) < day {
			return nil, nil
		}
	}
	defer waitingFor(url)()
	code, body, err := download(url, o)
	if err == nil && code == 404 {
		if dirErr == nil {
			save(path+".404", []byte("404"))
		}
		return nil, nil
	}
	if err == nil && code != 200 {
		err = fmt.Errorf("%s returned %d", url, code)
	}
	if err == nil && blocked(body) {
		err = fmt.Errorf("%s returned a block page", url)
	}
	if err != nil {
		if stale, readErr := os.ReadFile(path); dirErr == nil && readErr == nil {
			return stale, nil
		}
		return nil, err
	}
	if dirErr == nil {
		save(path, body)
	}
	return body, nil
}

func fetchAll(urls []string, o req) [][]byte {
	out := make([][]byte, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i], _ = fetch(u, o)
		}()
	}
	wg.Wait()
	return out
}

func fetchJSON(url string, o req, into any) (bool, error) {
	b, err := fetch(url, o)
	if err != nil || b == nil {
		return false, err
	}
	if err := json.Unmarshal(b, into); err != nil {
		forget(url)
		return false, err
	}
	return true, nil
}
