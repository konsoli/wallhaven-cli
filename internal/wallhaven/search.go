// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

package wallhaven

import (
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// maxRandomPage caps how deep a random pick will reach. The API documents
// pagination as "not actually infinite", and deep pages of a 500k-result
// random listing add nothing over shallow ones.
const maxRandomPage = 20

// PurityDefault is the SFW-only purity, matching wallhaven's own default.
const PurityDefault = "100"

var purityPattern = regexp.MustCompile(`^[01]{3}$`)

// ValidatePurity checks a purity bit string and reports whether it needs an
// API key.
//
// The bits are [sfw][sketchy][nsfw], the same order as the toggles on the
// site. Note that 010 is sketchy, not nsfw. Any pattern with the third bit
// set needs a key: without one the API answers 200 and silently drops the
// nsfw results rather than reporting an error.
func ValidatePurity(bits string) (needsKey bool, err error) {
	if !purityPattern.MatchString(bits) {
		return false, fmt.Errorf("purity must be three bits, [sfw][sketchy][nsfw], for example 100 or 111 (got %q)", bits)
	}
	if bits == "000" {
		return false, errors.New("purity 000 excludes everything; turn on at least one of sfw, sketchy or nsfw")
	}
	return bits[2] == '1', nil
}

// PurityLabel renders a purity bit string in words, for help and errors.
func PurityLabel(bits string) string {
	if !purityPattern.MatchString(bits) {
		return bits
	}
	names := [3]string{"sfw", "sketchy", "nsfw"}
	var on []string
	for i, n := range names {
		if bits[i] == '1' {
			on = append(on, n)
		}
	}
	if len(on) == 0 {
		return "nothing"
	}
	return strings.Join(on, " + ")
}

// Source is one way of choosing a wallpaper, corresponding to one CLI flag.
type Source struct {
	// Flag is the user-facing flag name, without dashes.
	Flag string
	// Arg is the flag's value, for the sources that take one.
	Arg string
}

// listing reports whether this source is a /search listing rather than a
// direct /w/<id> lookup.
func (s Source) listing() bool { return s.Flag != "id" }

// params builds the query string for a listing source.
func (s Source) params(purity string) (url.Values, error) {
	v := url.Values{}
	v.Set("purity", purity)

	switch s.Flag {
	case "random":
		v.Set("sorting", "random")
	case "latest":
		v.Set("sorting", "date_added")
		v.Set("order", "desc")
	case "toplist":
		v.Set("sorting", "toplist")
		v.Set("topRange", "1M")
	case "hotlist":
		// Undocumented in the API parameter table but supported by the
		// endpoint, and the only way to reach wallhaven.cc/hot.
		v.Set("sorting", "hot")
	case "tag":
		if _, err := strconv.Atoi(s.Arg); err != nil {
			return nil, fmt.Errorf("--tag takes a numeric tag id, for example 2321 for #pixel art (got %q)", s.Arg)
		}
		v.Set("q", "id:"+s.Arg)
		v.Set("sorting", "random")
	case "user":
		name := strings.TrimPrefix(strings.TrimSpace(s.Arg), "@")
		if name == "" {
			return nil, errors.New("--user takes a wallhaven username, for example helminuri")
		}
		v.Set("q", "@"+name)
		v.Set("sorting", "random")
	case "search":
		if strings.TrimSpace(s.Arg) == "" {
			return nil, errors.New("--search takes a phrase, for example \"yosemite sunset\"")
		}
		v.Set("q", s.Arg)
		v.Set("sorting", "random")
	default:
		return nil, fmt.Errorf("unknown source %q", s.Flag)
	}
	return v, nil
}

// noResults explains an empty listing in terms of the flag the user typed.
func (s Source) noResults(purity string) error {
	switch s.Flag {
	case "user":
		return fmt.Errorf("no uploads found for user %q at purity %s (%s)",
			strings.TrimPrefix(s.Arg, "@"), purity, PurityLabel(purity))
	case "tag":
		return fmt.Errorf("no wallpapers found for tag id %s at purity %s (%s)",
			s.Arg, purity, PurityLabel(purity))
	case "search":
		return fmt.Errorf("no wallpapers found for %q at purity %s (%s)",
			s.Arg, purity, PurityLabel(purity))
	default:
		return fmt.Errorf("wallhaven returned no wallpapers for --%s at purity %s (%s)",
			s.Flag, purity, PurityLabel(purity))
	}
}

// Pick chooses one wallpaper for the given source.
//
// --latest returns the single newest upload. Every other listing source picks
// uniformly at random: fetch page 1, then if there is more than one page,
// fetch a random page and take a random item from it. rnd may be nil, in
// which case the global source is used.
//
// The returned string is a human-readable note about the pick, such as the
// tag name wallhaven resolved, or "" when there is nothing to add.
func (c *Client) Pick(s Source, purity string, rnd *rand.Rand) (Wallpaper, string, error) {
	if !s.listing() {
		w, err := c.ByID(s.Arg)
		return w, "", err
	}

	params, err := s.params(purity)
	if err != nil {
		return Wallpaper{}, "", err
	}

	first, err := c.Search(params)
	if err != nil {
		return Wallpaper{}, "", err
	}
	// Trust meta.total, not meta.query: for a user that exists the API
	// returns an empty query string, while an unknown username is echoed
	// back verbatim.
	if first.Meta.Total == 0 || len(first.Data) == 0 {
		return Wallpaper{}, "", s.noResults(purity)
	}

	note := ""
	if tag, ok := first.Meta.ResolvedTag(); ok {
		note = "#" + tag
	}

	if s.Flag == "latest" {
		return first.Data[0], note, nil
	}

	page := first
	if last := min(first.Meta.LastPage, maxRandomPage); last > 1 {
		n := randIntn(rnd, last) + 1
		if n > 1 {
			// A random listing paginates by seed; carrying it forward keeps
			// the pages consistent with each other.
			if first.Meta.Seed != "" {
				params.Set("seed", first.Meta.Seed)
			}
			params.Set("page", strconv.Itoa(n))
			if p, err := c.Search(params); err == nil && len(p.Data) > 0 {
				page = p
			}
			// A failed or empty deeper page is not fatal: page 1 is already
			// in hand and is a perfectly good pool to pick from.
		}
	}

	return page.Data[randIntn(rnd, len(page.Data))], note, nil
}

func randIntn(rnd *rand.Rand, n int) int {
	if n <= 1 {
		return 0
	}
	if rnd == nil {
		return rand.Intn(n)
	}
	return rnd.Intn(n)
}
