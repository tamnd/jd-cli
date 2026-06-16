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
	"errors"
	"fmt"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

func init() { kit.Register(Domain{}) }

// Domain is the JD.com driver for the kit framework.
type Domain struct{}

// Info describes the scheme and identity used by both the standalone binary
// and multi-domain hosts.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme:  "jd",
		Aliases: []string{"jingdong"},
		Hosts:   []string{Host, "search.jd.com", "item.jd.com"},
		Identity: kit.Identity{
			Binary: "jd",
			Short:  "Fetch public JD.com product data from the command line",
			Long: `jd turns jd.com into a fast, scriptable command line.

Search products from China's second largest e-commerce platform.
No API key required: this CLI reads the same public pages your browser sees.

Note: JD.com applies risk-based protection. Requests from datacenter IPs
may be redirected to a verification page (exit 5). A residential IP helps.

Quick start:
  jd search "laptop"
  jd search "笔记本电脑" --sort price-asc
  jd search "手机" -n 60
  jd search "耳机" -o jsonl`,
			Site: "jd.com",
			Repo: "https://github.com/tamnd/jd-cli",
		},
	}
}

// Register installs the client factory and operations onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name:    "search",
		Group:   "products",
		Summary: "Search JD.com products",
		Args:    []kit.Arg{{Name: "query", Help: "search query (Chinese or English)"}},
	}, searchProducts)
}

func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := DefaultConfig()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.Timeout = cfg.Timeout
	}
	return NewClient(c), nil
}

type searchInput struct {
	Query  string  `kit:"arg"  help:"search query"`
	Limit  int     `kit:"flag" help:"max results" default:"30"`
	Sort   string  `kit:"flag" help:"sort: relevance|price-asc|price-desc|popularity|new" default:"relevance"`
	Client *Client `kit:"inject"`
}

func searchProducts(ctx context.Context, in searchInput, emit func(Product) error) error {
	if strings.TrimSpace(in.Query) == "" {
		return errs.Usage("query is required")
	}
	products, err := in.Client.Search(ctx, in.Query, in.Limit, in.Sort)
	if err != nil {
		return mapErr(err)
	}
	for _, p := range products {
		if err := emit(p); err != nil {
			return err
		}
	}
	return nil
}

// Classify turns a JD product URL or SKU ID into (type, id).
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty input")
	}
	// https://item.jd.com/1234567890.html
	if strings.Contains(input, "item.jd.com/") {
		parts := strings.Split(input, "item.jd.com/")
		if len(parts) > 1 {
			idPart := strings.TrimSuffix(parts[1], ".html")
			idPart = strings.Split(idPart, "?")[0]
			if idPart != "" {
				return "product", idPart, nil
			}
		}
	}
	// bare numeric SKU
	if isNumeric(input) {
		return "product", input, nil
	}
	return "", "", errs.Usage("jd: unrecognized reference: %q", input)
}

// Locate returns the canonical JD URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "product":
		return fmt.Sprintf("https://item.jd.com/%s.html", id), nil
	default:
		return "", errs.Usage("jd has no resource type %q", uriType)
	}
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrBlocked) {
		return errs.RateLimited("%s", err.Error())
	}
	if errors.Is(err, ErrNotFound) {
		return errs.NotFound("%s", err.Error())
	}
	return err
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
