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
	"testing"
)

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "jd" {
		t.Errorf("Scheme = %q, want jd", info.Scheme)
	}
	if info.Identity.Binary != "jd" {
		t.Errorf("Identity.Binary = %q, want jd", info.Identity.Binary)
	}
	if info.Identity.Site != "jd.com" {
		t.Errorf("Identity.Site = %q, want jd.com", info.Identity.Site)
	}
}

func TestClassify_ProductURL(t *testing.T) {
	typ, id, err := Domain{}.Classify("https://item.jd.com/100012043978.html")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "product" {
		t.Errorf("type = %q, want product", typ)
	}
	if id != "100012043978" {
		t.Errorf("id = %q, want 100012043978", id)
	}
}

func TestClassify_NumericSKU(t *testing.T) {
	typ, id, err := Domain{}.Classify("100012043978")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if typ != "product" {
		t.Errorf("type = %q, want product", typ)
	}
	if id != "100012043978" {
		t.Errorf("id = %q, want 100012043978", id)
	}
}

func TestClassify_Empty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("expected error for empty input, got nil")
	}
}

func TestLocate_Product(t *testing.T) {
	got, err := Domain{}.Locate("product", "100012043978")
	want := "https://item.jd.com/100012043978.html"
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestLocate_UnknownType(t *testing.T) {
	_, err := Domain{}.Locate("unknown", "123")
	if err == nil {
		t.Error("expected error for unknown type, got nil")
	}
}
