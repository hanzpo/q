# q

Natural language lookups in the terminal. `q` picks one site and prints that site's own text. No model reads or rewrites the page.

```
q synonyms for admonish
q C++ unique_ptr documentation
q Vec::push
q how do i undo the last git commit
```

Run `q --sites` for the list. Each site has its own parser in `sites.go` and `sites_more.go`. Sites that publish an `llms.txt` or sitemap share one parser.

## Routing

1. A query you corrected before goes to the same site.
2. Regex rules in `route.go`.
3. Exact symbol matches against the Python, C++ and Go indexes.
4. A small naive Bayes classifier trained on `examples.go` and your corrections. It runs locally in under a millisecond.

If the first site has no page, `q` tries the next one and says so. A query that names a product, like `cloudflare` or `stripe`, stays on that site.

## Corrections

`q --via wikipedia rust` shows that site and remembers it in `~/.local/share/q/learned.jsonl`. Later queries learn from it too.

## Install

```
go build -o ~/.local/bin/q .
```

In zsh, add `alias q='noglob q'` so `?` in a query isn't treated as a glob.
