// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

package wallhaven

import (
	"encoding/json"
	"testing"
)

func TestValidatePurity(t *testing.T) {
	tests := []struct {
		bits    string
		wantKey bool
		wantErr bool
	}{
		{"100", false, false},
		{"110", false, false},
		{"010", false, false},
		{"111", true, false},
		{"011", true, false},
		{"001", true, false},
		{"101", true, false},
		{"000", false, true},
		{"1001", false, true},
		{"abc", false, true},
		{"", false, true},
		{"1", false, true},
		{"sfw", false, true},
	}
	for _, tt := range tests {
		needsKey, err := ValidatePurity(tt.bits)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidatePurity(%q) error = %v, wantErr %v", tt.bits, err, tt.wantErr)
			continue
		}
		if err == nil && needsKey != tt.wantKey {
			t.Errorf("ValidatePurity(%q) needsKey = %v, want %v", tt.bits, needsKey, tt.wantKey)
		}
	}
}

func TestPurityLabel(t *testing.T) {
	for bits, want := range map[string]string{
		"100": "sfw",
		"110": "sfw + sketchy",
		"111": "sfw + sketchy + nsfw",
		"010": "sketchy",
		"001": "nsfw",
		"101": "sfw + nsfw",
	} {
		if got := PurityLabel(bits); got != want {
			t.Errorf("PurityLabel(%q) = %q, want %q", bits, got, want)
		}
	}
}

func TestSourceParams(t *testing.T) {
	tests := []struct {
		name   string
		source Source
		want   string
	}{
		{"random", Source{Flag: "random"}, "purity=100&sorting=random"},
		{"latest", Source{Flag: "latest"}, "order=desc&purity=100&sorting=date_added"},
		{"toplist", Source{Flag: "toplist"}, "purity=100&sorting=toplist&topRange=1M"},
		{"hotlist", Source{Flag: "hotlist"}, "purity=100&sorting=hot"},
		{"tag", Source{Flag: "tag", Arg: "2321"}, "purity=100&q=id%3A2321&sorting=random"},
		{"user", Source{Flag: "user", Arg: "helminuri"}, "purity=100&q=%40helminuri&sorting=random"},
		{"user with @", Source{Flag: "user", Arg: "@helminuri"}, "purity=100&q=%40helminuri&sorting=random"},
		{"search", Source{Flag: "search", Arg: "yosemite sunset"}, "purity=100&q=yosemite+sunset&sorting=random"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := tt.source.params(PurityDefault)
			if err != nil {
				t.Fatalf("params() error = %v", err)
			}
			if got := v.Encode(); got != tt.want {
				t.Errorf("params() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSourceParamsPurityPassthrough(t *testing.T) {
	s := Source{Flag: "search", Arg: "anime woman"}
	v, err := s.params("111")
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Get("purity"); got != "111" {
		t.Errorf("purity = %q, want 111", got)
	}
}

func TestSourceParamsRejectsBadInput(t *testing.T) {
	for _, s := range []Source{
		{Flag: "tag", Arg: "pixel art"},
		{Flag: "tag", Arg: ""},
		{Flag: "user", Arg: "@"},
		{Flag: "user", Arg: "  "},
		{Flag: "search", Arg: "   "},
		{Flag: "nope"},
	} {
		if _, err := s.params(PurityDefault); err == nil {
			t.Errorf("params() for %+v = nil error, want an error", s)
		}
	}
}

func TestSourceListing(t *testing.T) {
	byID := Source{Flag: "id", Arg: "g8d9dl"}
	if byID.listing() {
		t.Error("--id must not be treated as a listing source")
	}
	for _, f := range []string{"random", "latest", "toplist", "hotlist", "tag", "user", "search"} {
		s := Source{Flag: f}
		if !s.listing() {
			t.Errorf("--%s must be treated as a listing source", f)
		}
	}
}

// The API returns meta.query as a string for a plain search and as an object
// for an exact tag search, so the decoder has to tolerate both.
func TestMetaResolvedTag(t *testing.T) {
	var tagged SearchResult
	if err := json.Unmarshal([]byte(`{"data":[],"meta":{"total":1550,"query":{"id":2321,"tag":"pixel art"}}}`), &tagged); err != nil {
		t.Fatal(err)
	}
	name, ok := tagged.Meta.ResolvedTag()
	if !ok || name != "pixel art" {
		t.Errorf("ResolvedTag() = %q, %v; want \"pixel art\", true", name, ok)
	}

	var phrase SearchResult
	if err := json.Unmarshal([]byte(`{"data":[],"meta":{"total":45,"query":"yosemite sunset"}}`), &phrase); err != nil {
		t.Fatal(err)
	}
	if _, ok := phrase.Meta.ResolvedTag(); ok {
		t.Error("ResolvedTag() = true for a plain string query, want false")
	}

	var null SearchResult
	if err := json.Unmarshal([]byte(`{"data":[],"meta":{"total":0,"query":null}}`), &null); err != nil {
		t.Fatal(err)
	}
	if _, ok := null.Meta.ResolvedTag(); ok {
		t.Error("ResolvedTag() = true for a null query, want false")
	}
}
