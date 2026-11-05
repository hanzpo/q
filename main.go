package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/term"
)

const usage = `usage: q [--via site] [--plain] <query>
       q --sites

--via site   use this site, and remember the choice for this query
--plain      print without a pager`

func main() {
	var via string
	var plain bool
	var words []string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--via":
			i++
			if i < len(args) {
				via = args[i]
			}
		case "--plain":
			plain = true
		case "--sites":
			for _, s := range sites {
				fmt.Printf("%-14s%s\n", s.ID, s.Name)
			}
			return
		case "-h", "--help":
			fmt.Println(usage)
			return
		default:
			words = append(words, a)
		}
	}
	query := strings.TrimSpace(strings.Join(words, " "))
	if query == "" {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	if via != "" && siteByID(via) == nil {
		fmt.Fprintf(os.Stderr, "unknown site: %s. see q --sites\n", via)
		os.Exit(2)
	}

	width := 100
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width = min(w, 110) - 2
	}

	var cancel context.CancelFunc
	runCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	order := route(query)
	if via != "" {
		order = []string{via}
	}
	var found *Page
	var used string
	var missed []string
	for _, id := range order {
		s := siteByID(id)
		p, err := s.Get(s.term(query), width)
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintln(os.Stderr, dim(s.Name+" timed out"))
		} else if err != nil {
			fmt.Fprintln(os.Stderr, dim(s.Name+": "+err.Error()))
		}
		if p != nil && p.Body != "" {
			found, used = p, s.Name
			break
		}
		missed = append(missed, s.Name)
	}
	if found == nil {
		fmt.Fprintf(os.Stderr, "nothing found for %q\n", query)
		os.Exit(1)
	}
	if via != "" {
		if err := learn(query, via); err != nil {
			fmt.Fprintln(os.Stderr, dim(err.Error()))
		}
	}

	if strings.HasPrefix(found.URL, "https://docs.rs/") {
		used = "docs.rs"
	}

	var b strings.Builder
	if len(missed) > 0 {
		b.WriteString(dim("not on "+strings.Join(missed, " or ")+", showing "+used) + "\n")
	}
	shown := found.URL
	if strings.HasPrefix(shown, "http") {
		shown = hyperlink(shown, shown)
	}
	fmt.Fprintf(&b, "%s  %s\n%s\n\n%s\n", bold(found.Title), dim(used), dim(shown), found.Body)

	if !plain && term.IsTerminal(int(os.Stdout.Fd())) {
		less := exec.Command("less", "-RFX")
		less.Stdin = strings.NewReader(b.String())
		less.Stdout = os.Stdout
		less.Stderr = os.Stderr
		if less.Run() == nil {
			return
		}
	}
	fmt.Print(b.String())
}
