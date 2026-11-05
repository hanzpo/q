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

var (
	brandTickers = map[string]string{
		"google": "GOOGL", "youtube": "GOOGL", "facebook": "META", "instagram": "META", "whatsapp": "META", "snapchat": "SNAP",
		"coke": "KO", "coca cola": "KO", "coca-cola": "KO", "pepsi": "PEP", "mcdonalds": "MCD", "mcdonald's": "MCD",
		"chase": "JPM", "jp morgan": "JPM", "jpmorgan": "JPM", "jpmorgan chase": "JPM", "goldman": "GS", "goldman sachs": "GS",
		"morgan stanley": "MS", "bank of america": "BAC", "wells fargo": "WFC", "berkshire": "BRK.B", "berkshire hathaway": "BRK.B",
		"johnson & johnson": "JNJ", "johnson and johnson": "JNJ", "j&j": "JNJ", "at&t": "T", "att": "T", "exxon": "XOM", "exxonmobil": "XOM", "exxon mobil": "XOM", "tsmc": "TSM", "taiwan semiconductor": "TSM",
	}
	notCommon = regexp.MustCompile(`(?i)\b(warrants?|units?|rights?|preferred|notes?|debentures?)\b`)
)

func pickSymbol(hits []nasdaqHit, term string) *nasdaqHit {
	var stocks, funds []*nasdaqHit
	for i := range hits {
		h := &hits[i]
		switch {
		case h.Asset == "ETF":
			funds = append(funds, h)
		case h.Asset == "STOCKS" && !strings.Contains(h.Symbol, ".PR") && !notCommon.MatchString(h.Name):
			stocks = append(stocks, h)
		}
	}
	all := append(append([]*nasdaqHit{}, stocks...), funds...)
	first := func(list []*nasdaqHit, match func(*nasdaqHit) bool) *nasdaqHit {
		for _, h := range list {
			if match(h) {
				return h
			}
		}
		return nil
	}
	bySymbol := func(h *nasdaqHit) bool { return strings.EqualFold(h.Symbol, term) }
	quoted := regexp.QuoteMeta(term)
	prefix := regexp.MustCompile(`(?i)^(the )?` + quoted + `\b`)
	anywhere := regexp.MustCompile(`(?i)\b` + quoted + `\b`)
	byPrefix := func(h *nasdaqHit) bool { return prefix.MatchString(h.Name) }
	byWord := func(h *nasdaqHit) bool { return anywhere.MatchString(h.Name) }
	if term == strings.ToUpper(term) {
		if h := first(all, bySymbol); h != nil {
			return h
		}
	}
	if h := first(stocks, byPrefix); h != nil {
		return h
	}
	if h := first(all, bySymbol); h != nil {
		return h
	}
	return first(stocks, byWord)
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
	key := strings.ToLower(lookup)
	if sym, ok := cryptoNames[key]; ok {
		return cryptoGet(sym)
	}
	if m, ok := marketSymbols[key]; ok {
		return marketGet(m)
	}
	var q nasdaqQuote
	class := "stocks"
	brand, isBrand := brandTickers[key]
	if isBrand {
		lookup = brand
	}
	if isBrand || tickerShape.MatchString(lookup) {
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

type market struct{ symbol, unit string }

var marketSymbols = func() map[string]market {
	out := map[string]market{}
	for names, m := range map[string]market{
		"oil|crude|crude oil|wti|wti crude":   {"@CL.1", "USD per barrel, front-month futures"},
		"brent|brent crude|brent oil":         {"@LCO.1", "USD per barrel, front-month futures"},
		"gold":                                {"@GC.1", "USD per troy ounce, front-month futures"},
		"silver":                              {"@SI.1", "USD per troy ounce, front-month futures"},
		"platinum":                            {"@PL.1", "USD per troy ounce, front-month futures"},
		"copper":                              {"@HG.1", "USD per pound, front-month futures"},
		"natural gas|nat gas":                 {"@NG.1", "USD per MMBtu, front-month futures"},
		"gasoline|gas|rbob":                   {"@RB.1", "USD per gallon, front-month futures"},
		"wheat":                               {"@W.1", "US cents per bushel, front-month futures"},
		"corn":                                {"@C.1", "US cents per bushel, front-month futures"},
		"soybeans|soybean":                    {"@S.1", "US cents per bushel, front-month futures"},
		"coffee":                              {"@KC.1", "US cents per pound, front-month futures"},
		"s&p 500|s&p500|s&p|sp500|sp 500|spx": {".SPX", ""},
		"dow|dow jones|djia":                  {".DJI", ""},
		"nasdaq|nasdaq composite":             {".IXIC", ""},
		"russell 2000|russell":                {".RUT", ""},
		"vix":                                 {".VIX", ""},
		"2 year treasury|2 year|2y|2yr":       {"US2Y", "yield"},
		"10 year treasury|10 year|10y|10yr|treasury": {"US10Y", "yield"},
		"30 year treasury|30 year|30y|30yr":          {"US30Y", "yield"},
	} {
		for _, n := range strings.Split(names, "|") {
			out[n] = m
		}
	}
	return out
}()

func marketGet(m market) (*Page, error) {
	var data struct {
		Result struct {
			Quotes []struct {
				Name     string `json:"name"`
				Last     string `json:"last"`
				Change   string `json:"change"`
				Percent  string `json:"change_pct"`
				Time     string `json:"last_timedate"`
				High     string `json:"high"`
				Low      string `json:"low"`
				YearHigh string `json:"yrhiprice"`
				YearLow  string `json:"yrloprice"`
			} `json:"FormattedQuote"`
		} `json:"FormattedQuoteResult"`
	}
	u := "https://quote.cnbc.com/quote-html-webservice/restQuote/symbolType/symbol?requestMethod=itv&noform=1&partnerId=2&fund=1&exthrs=1&output=json&symbols=" + url.QueryEscape(m.symbol)
	ok, err := fetchJSON(u, req{ttl: time.Minute}, &data)
	if err != nil || !ok || len(data.Result.Quotes) == 0 || data.Result.Quotes[0].Last == "" {
		return nil, err
	}
	q := data.Result.Quotes[0]
	out := []string{bold(fmt.Sprintf("%s  %s (%s)", q.Last, q.Change, q.Percent))}
	if m.unit != "" {
		out = append(out, dim(m.unit))
	}
	out = append(out, dim("Last update "+q.Time), "")
	if q.High != "" && q.High != "0.00" {
		out = append(out, fmt.Sprintf("%-18s%s - %s", "Day range:", q.Low, q.High))
	}
	if q.YearHigh != "" {
		out = append(out, fmt.Sprintf("%-18s%s - %s", "52 week range:", q.YearLow, q.YearHigh))
	}
	return &Page{q.Name, "https://www.cnbc.com/quotes/" + m.symbol, strings.TrimRight(strings.Join(out, "\n"), "\n")}, nil
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
		Noise: noise("stock stocks share shares price prices quote current latest live spot ticker symbol index yield rate of for the today now what's whats what is how much market value a an in usd")})
}
