package main

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var cryptoNames = map[string]string{
	"bitcoin": "BTC", "btc": "BTC", "ethereum": "ETH", "ether": "ETH", "eth": "ETH", "solana": "SOL", "sol": "SOL",
	"dogecoin": "DOGE", "doge": "DOGE", "litecoin": "LTC", "ltc": "LTC", "xrp": "XRP", "ripple": "XRP", "cardano": "ADA", "ada": "ADA",
}

type nasdaqHit struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	Asset  string `json:"asset"`
}

func pickSymbol(hits []nasdaqHit, term string) *nasdaqHit {
	var tradable []*nasdaqHit
	for i := range hits {
		if hits[i].Asset == "STOCKS" || hits[i].Asset == "ETF" {
			tradable = append(tradable, &hits[i])
		}
	}
	bySymbol := func() *nasdaqHit {
		for _, h := range tradable {
			if strings.EqualFold(h.Symbol, term) {
				return h
			}
		}
		return nil
	}
	if term == strings.ToUpper(term) {
		if h := bySymbol(); h != nil {
			return h
		}
	}
	word := regexp.MustCompile(`(?i)^` + regexp.QuoteMeta(term) + `\b`)
	for _, h := range tradable {
		if word.MatchString(h.Name) && !strings.Contains(h.Symbol, ".") {
			return h
		}
	}
	return bySymbol()
}

type nasdaqLine struct {
	Price   string `json:"lastSalePrice"`
	Change  string `json:"netChange"`
	Percent string `json:"percentageChange"`
	Time    string `json:"lastTradeTimestamp"`
	Live    bool   `json:"isRealTime"`
	Volume  string `json:"volume"`
}

type nasdaqQuote struct {
	Data *struct {
		Symbol    string      `json:"symbol"`
		Name      string      `json:"companyName"`
		Primary   nasdaqLine  `json:"primaryData"`
		Secondary *nasdaqLine `json:"secondaryData"`
		Status    string      `json:"marketStatus"`
		KeyStats  map[string]struct {
			Label string `json:"label"`
			Value string `json:"value"`
		} `json:"keyStats"`
	} `json:"data"`
}

func quoteLine(l nasdaqLine) string {
	if l.Change == "" {
		return l.Price
	}
	return fmt.Sprintf("%s  %s (%s)", l.Price, l.Change, l.Percent)
}

var tickerShape = regexp.MustCompile(`^[A-Z]{1,5}$`)

func nasdaqQuoteURL(symbol, class string) string {
	return "https://api.nasdaq.com/api/quote/" + url.PathEscape(symbol) + "/info?assetclass=" + class
}

func stocksGet(term string, width int) (*Page, error) {
	lookup := strings.TrimPrefix(term, "$")
	if lookup == "" {
		return nil, nil
	}
	if sym, ok := cryptoNames[strings.ToLower(lookup)]; ok {
		return cryptoGet(sym)
	}
	var q nasdaqQuote
	class := "stocks"
	if tickerShape.MatchString(lookup) {
		if _, err := fetchJSON(nasdaqQuoteURL(lookup, class), req{ttl: time.Minute}, &q); err != nil {
			return nil, err
		}
	}
	if q.Data == nil {
		var found struct {
			Data []nasdaqHit `json:"data"`
		}
		ok, err := fetchJSON("https://api.nasdaq.com/api/autocomplete/slookup/10?search="+url.QueryEscape(lookup), req{}, &found)
		if err != nil || !ok {
			return nil, err
		}
		hit := pickSymbol(found.Data, lookup)
		if hit == nil {
			return nil, nil
		}
		if hit.Asset == "ETF" {
			class = "etf"
		}
		ok, err = fetchJSON(nasdaqQuoteURL(hit.Symbol, class), req{ttl: time.Minute}, &q)
		if err != nil || !ok || q.Data == nil {
			return nil, err
		}
	}
	d := q.Data
	timing := "delayed"
	if d.Primary.Live {
		timing = "real time"
	}
	out := []string{bold(quoteLine(d.Primary)), dim(fmt.Sprintf("Market %s, %s, %s", strings.ToLower(d.Status), d.Primary.Time, timing)), ""}
	if d.Secondary != nil {
		out = append(out, fmt.Sprintf("%-18s%s", "After hours:", quoteLine(*d.Secondary)))
	}
	if d.Primary.Volume != "" {
		out = append(out, fmt.Sprintf("%-18s%s", "Volume:", d.Primary.Volume))
	}
	for _, key := range []string{"dayrange", "fiftyTwoWeekHighLow"} {
		if s, ok := d.KeyStats[key]; ok && s.Value != "" && s.Value != "NA" {
			out = append(out, fmt.Sprintf("%-18s%s", s.Label, s.Value))
		}
	}
	web := "https://www.nasdaq.com/market-activity/" + class + "/" + strings.ToLower(d.Symbol)
	return &Page{d.Name + " (" + d.Symbol + ")", web, strings.TrimRight(strings.Join(out, "\n"), "\n")}, nil
}

func cryptoGet(sym string) (*Page, error) {
	var data struct {
		Data struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"data"`
	}
	u := "https://api.coinbase.com/v2/prices/" + sym + "-USD/spot"
	ok, err := fetchJSON(u, req{ttl: time.Minute}, &data)
	if err != nil || !ok || data.Data.Amount == "" {
		return nil, err
	}
	body := bold(data.Data.Amount+" "+data.Data.Currency) + "\n" + dim("Coinbase spot price")
	return &Page{sym, "https://www.coinbase.com/price/" + strings.ToLower(sym), body}, nil
}

func init() {
	sites = append(sites, &Site{ID: "stocks", Name: "Nasdaq", Get: stocksGet, Explicit: true, EmptyOK: true,
		Noise: noise("stock stocks share shares price prices quote current latest live ticker symbol of for the today now what's whats what is how much market value a an in usd")})
}
