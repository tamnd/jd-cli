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

package jd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const searchFixture = `<!DOCTYPE html>
<html>
<body>
<ul class="gl-warp clearfix" id="J_goodsList">
  <li class="gl-item" data-sku="100012043978" data-spu="100012043978">
    <div class="gl-i-wrap j-sku-item">
      <div class="p-img">
        <a href="//item.jd.com/100012043978.html" title="Test Laptop">
          <img src="//img14.360buyimg.com/thumb.jpg" alt="Test Laptop">
        </a>
      </div>
      <div class="p-price">
        <strong id="J_100012043978"><em>&#165;</em><i>3299.00</i></strong>
      </div>
      <div class="p-name p-name-type-2">
        <a href="//item.jd.com/100012043978.html" title="Test Laptop Pro i7 16GB">
          <em>Test Laptop Pro</em>
        </a>
      </div>
      <div class="p-shop">
        <span data-selfware="1"><a title="JD Direct">JD Direct</a></span>
      </div>
      <div class="p-commit">
        <strong><a>12,345` + "\xe6\x9d\xa1\xe8\xaf\x84\xe4\xbb\xb7" + `</a></strong>
      </div>
    </div>
  </li>
</ul>
</body>
</html>`

func TestSearch_ParsesProducts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(searchFixture))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SearchURL = srv.URL
	cfg.Rate = 0

	c := NewClient(cfg)
	products, err := c.Search(context.Background(), "laptop", 10, "relevance")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("want 1 product, got %d", len(products))
	}

	p := products[0]
	if p.ID != "100012043978" {
		t.Errorf("ID: want 100012043978, got %q", p.ID)
	}
	if p.Title != "Test Laptop Pro i7 16GB" {
		t.Errorf("Title: want %q, got %q", "Test Laptop Pro i7 16GB", p.Title)
	}
	if p.PriceFen != 329900 {
		t.Errorf("PriceFen: want 329900, got %d", p.PriceFen)
	}
	if p.PriceYuan != 3299.00 {
		t.Errorf("PriceYuan: want 3299.00, got %f", p.PriceYuan)
	}
	if p.Currency != "CNY" {
		t.Errorf("Currency: want CNY, got %q", p.Currency)
	}
	if p.Shop != "JD Direct" {
		t.Errorf("Shop: want %q, got %q", "JD Direct", p.Shop)
	}
	if p.ReviewCount != 12345 {
		t.Errorf("ReviewCount: want 12345, got %d", p.ReviewCount)
	}
	if p.URL != "https://item.jd.com/100012043978.html" {
		t.Errorf("URL: want %q, got %q", "https://item.jd.com/100012043978.html", p.URL)
	}
	if p.Image != "https://img14.360buyimg.com/thumb.jpg" {
		t.Errorf("Image: want %q, got %q", "https://img14.360buyimg.com/thumb.jpg", p.Image)
	}
}

func TestSearch_RiskRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://cfe.m.jd.com/privatedomain/risk_handler/03101900/?returnurl=test", http.StatusFound)
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SearchURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 1

	c := NewClient(cfg)
	_, err := c.Search(context.Background(), "laptop", 10, "relevance")
	if err == nil {
		t.Fatal("expected error for risk redirect, got nil")
	}
	if err != ErrBlocked {
		t.Errorf("want ErrBlocked, got %v", err)
	}
}

func TestSearch_EmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<html><body><ul id="J_goodsList"></ul></body></html>`))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SearchURL = srv.URL
	cfg.Rate = 0

	c := NewClient(cfg)
	products, err := c.Search(context.Background(), "zzznoresults", 10, "relevance")
	if err != nil {
		t.Fatalf("unexpected error for empty results: %v", err)
	}
	if len(products) != 0 {
		t.Errorf("want 0 products, got %d", len(products))
	}
}

func TestSearch_RetriesOn429(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(searchFixture))
	}))
	defer srv.Close()

	cfg := DefaultConfig()
	cfg.SearchURL = srv.URL
	cfg.Rate = 0
	cfg.Retries = 3

	c := NewClient(cfg)
	products, err := c.Search(context.Background(), "laptop", 10, "relevance")
	if err != nil {
		t.Fatalf("unexpected error after retry: %v", err)
	}
	if len(products) == 0 {
		t.Error("expected products after retry, got none")
	}
}

func TestAbsURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"//item.jd.com/123.html", "https://item.jd.com/123.html"},
		{"https://item.jd.com/123.html", "https://item.jd.com/123.html"},
		{"/path/to/page", "https://www.jd.com/path/to/page"},
	}
	for _, tc := range cases {
		got := absURL(tc.in)
		if got != tc.want {
			t.Errorf("absURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParsePrice(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"3299.00", 329900},
		{"1,290.50", 129050},
		{"100", 10000},
	}
	for _, tc := range cases {
		got := parsePrice(tc.in)
		if got != tc.want {
			t.Errorf("parsePrice(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestSortMap(t *testing.T) {
	keys := []string{"relevance", "price-asc", "price-desc", "popularity", "new"}
	for _, k := range keys {
		if _, ok := SortMap[k]; !ok {
			t.Errorf("SortMap missing key %q", k)
		}
	}
}
