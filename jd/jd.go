// Copyright 2026 Duc-Tam Nguyen
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package jd is the library behind the jd command line: the HTTP client,
// HTML parsers, and typed data models for JD.com (京东).
//
// JD.com is China's second largest e-commerce platform. The search endpoint
// at search.jd.com serves server-rendered HTML. Requests from datacenter IPs
// often receive a 302 redirect to a risk handler; the client detects this and
// returns ErrBlocked (exit code 5).
package jd

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Host is the primary search hostname.
const Host = "jd.com"

// SearchHost is the hostname used for search requests.
const SearchHost = "search.jd.com"

// DefaultUserAgent is the browser User-Agent sent on every request.
const DefaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// ErrBlocked is returned when the JD risk handler redirects the request.
// Callers should map this to exit code 5.
var ErrBlocked = errors.New("jd: request redirected to risk handler; try a residential IP or --proxy")

// ErrNotFound is returned when a resource does not exist (HTTP 404).
var ErrNotFound = errors.New("jd: not found")

// Config holds constructor parameters for Client.
type Config struct {
	SearchURL string
	UserAgent string
	Rate      time.Duration
	Retries   int
	Timeout   time.Duration
}

// DefaultConfig returns sensible defaults for jd.com.
func DefaultConfig() Config {
	return Config{
		SearchURL: "https://search.jd.com",
		UserAgent: DefaultUserAgent,
		Rate:      1 * time.Second,
		Retries:   3,
		Timeout:   30 * time.Second,
	}
}

// errBlockedRedirect is a sentinel used by CheckRedirect.
var errBlockedRedirect = errors.New("jd: risk handler redirect detected")

// Client is a rate-limited HTTP client for JD.com.
type Client struct {
	cfg  Config
	http *http.Client
	mu   sync.Mutex
	last time.Time
}

// NewClient returns a Client configured with cfg.
func NewClient(cfg Config) *Client {
	transport := &http.Transport{
		MaxIdleConns:        16,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}
	httpClient := &http.Client{
		Timeout:   cfg.Timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if strings.Contains(req.URL.String(), "risk_handler") ||
				strings.Contains(req.URL.Host, "cfe.m.jd.com") {
				return errBlockedRedirect
			}
			if len(via) > 5 {
				return errors.New("jd: too many redirects")
			}
			return nil
		},
	}
	return &Client{
		cfg:  cfg,
		http: httpClient,
	}
}

// Product is one search result from JD.com.
type Product struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	PriceFen    int64   `json:"price_fen"`   // price in fen (1/100 RMB); 0 if unknown
	PriceYuan   float64 `json:"price_yuan"`  // price in yuan for display
	Currency    string  `json:"currency"`    // always "CNY"
	Shop        string  `json:"shop"`
	ReviewCount int     `json:"review_count"`
	URL         string  `json:"url"`
	Image       string  `json:"image"`
}

// SortMap maps user-friendly sort names to JD sort parameters.
var SortMap = map[string]string{
	"relevance":  "",
	"price-asc":  "price%23asc",
	"price-desc": "price%23desc",
	"popularity": "sale%23desc",
	"new":        "newc%23desc",
}

// Search fetches products matching query. It paginates automatically to collect
// up to limit results.
func (c *Client) Search(ctx context.Context, query string, limit int, sort string) ([]Product, error) {
	if limit <= 0 {
		limit = 30
	}

	var all []Product
	for page := 1; len(all) < limit; page += 2 {
		items, err := c.searchPage(ctx, query, page, sort)
		if err != nil {
			if len(all) > 0 {
				break // already have results; stop silently
			}
			return nil, err
		}
		all = append(all, items...)
		if len(items) < 28 { // end of results (JD returns ~30 per page)
			break
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

// searchPage fetches one page of search results.
func (c *Client) searchPage(ctx context.Context, query string, page int, sort string) ([]Product, error) {
	sortParam, ok := SortMap[sort]
	if !ok {
		sortParam = ""
	}

	u := fmt.Sprintf("%s/Search?keyword=%s&enc=utf-8&wq=%s&page=%d&click=0",
		c.cfg.SearchURL,
		url.QueryEscape(query),
		url.QueryEscape(query),
		page,
	)
	if sortParam != "" {
		u += "&sort=" + sortParam
	}

	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	return parseSearch(body, "https://item.jd.com"), nil
}

var (
	itemBlockRE   = regexp.MustCompile(`(?s)<li[^>]+class="[^"]*gl-item[^"]*"[^>]+data-sku="(\d+)"[^>]*>(.*?)</li>`)
	priceRE       = regexp.MustCompile(`<i>([\d.,]+)</i>`)
	titleAttrRE   = regexp.MustCompile(`class="p-name[^"]*"[\s\S]{0,400}?title="([^"]+)"`)
	shopRE        = regexp.MustCompile(`class="p-shop"[\s\S]{0,600}?<a[^>]*>([^<]+)</a>`)
	reviewCountRE = regexp.MustCompile(`>([\d,]+)条评价<`)
	hrefRE        = regexp.MustCompile(`class="p-img"[\s\S]{0,400}?href="([^"]+)"`)
	imgSrcRE      = regexp.MustCompile(`class="p-img"[\s\S]{0,500}?<img[^>]+src="([^"]+)"`)
)

// parseSearch extracts products from JD search result HTML.
func parseSearch(body []byte, baseURL string) []Product {
	var products []Product
	for _, m := range itemBlockRE.FindAllSubmatch(body, -1) {
		id := string(m[1])
		block := m[2]

		p := Product{
			ID:       id,
			Currency: "CNY",
			URL:      "https://item.jd.com/" + id + ".html",
		}

		if pm := priceRE.FindSubmatch(block); len(pm) > 1 {
			p.PriceFen = parsePrice(string(pm[1]))
			p.PriceYuan = float64(p.PriceFen) / 100
		}
		if tm := titleAttrRE.FindSubmatch(block); len(tm) > 1 {
			p.Title = cleanTitle(string(tm[1]))
		}
		if sm := shopRE.FindSubmatch(block); len(sm) > 1 {
			p.Shop = strings.TrimSpace(string(sm[1]))
		}
		if rcm := reviewCountRE.FindSubmatch(block); len(rcm) > 1 {
			s := strings.ReplaceAll(string(rcm[1]), ",", "")
			p.ReviewCount, _ = strconv.Atoi(s)
		}
		if hm := hrefRE.FindSubmatch(block); len(hm) > 1 {
			p.URL = absURL(string(hm[1]))
		}
		if im := imgSrcRE.FindSubmatch(block); len(im) > 1 {
			p.Image = absURL(string(im[1]))
		}

		products = append(products, p)
	}
	return products
}

// parsePrice converts "3,299.00" into fen (int64 cents * 100).
func parsePrice(s string) int64 {
	s = strings.ReplaceAll(s, ",", "")
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return int64(f * 100)
}

var tagRE = regexp.MustCompile(`<[^>]+>`)

// cleanTitle strips HTML tags and entities from a title string.
func cleanTitle(s string) string {
	s = tagRE.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	return strings.TrimSpace(s)
}

// absURL converts a protocol-relative or relative URL to absolute HTTPS.
func absURL(href string) string {
	if strings.HasPrefix(href, "//") {
		return "https:" + href
	}
	if strings.HasPrefix(href, "http") {
		return href
	}
	return "https://www.jd.com" + href
}

// get fetches a URL with pacing and retry.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	attempts := c.cfg.Retries
	if attempts < 1 {
		attempts = 1
	}
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			wait := time.Duration(attempt-1) * 500 * time.Millisecond
			if wait > 10*time.Second {
				wait = 10 * time.Second
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}

		c.pace()
		body, code, err := c.do(rawURL)
		if err != nil {
			if errors.Is(err, errBlockedRedirect) {
				return nil, ErrBlocked
			}
			lastErr = err
			continue
		}
		if code == http.StatusNotFound {
			return nil, ErrNotFound
		}
		if code == http.StatusTooManyRequests || code >= 500 {
			lastErr = fmt.Errorf("http %d", code)
			continue
		}
		if code == http.StatusFound || code == http.StatusMovedPermanently {
			// A redirect we didn't intercept
			lastErr = fmt.Errorf("http %d (unexpected redirect)", code)
			continue
		}
		if code != http.StatusOK {
			return nil, fmt.Errorf("http %d", code)
		}
		return body, nil
	}
	return nil, fmt.Errorf("get %s after %d attempts: %w", rawURL, attempts, lastErr)
}

func (c *Client) do(rawURL string) ([]byte, int, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", c.cfg.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Referer", "https://www.jd.com/")

	resp, err := c.http.Do(req)
	if err != nil {
		// Detect blocked redirect via URL errors wrapping errBlockedRedirect
		var urlErr *url.Error
		if errors.As(err, &urlErr) && errors.Is(urlErr.Err, errBlockedRedirect) {
			return nil, 0, errBlockedRedirect
		}
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func (c *Client) pace() {
	if c.cfg.Rate <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := c.cfg.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}
