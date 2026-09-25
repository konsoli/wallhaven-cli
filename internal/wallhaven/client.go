// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

package wallhaven

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BaseURL is the root of the wallhaven API v1.
const BaseURL = "https://wallhaven.cc/api/v1"

// PerPage is the fixed page size of a /search response. The API does not let
// an unauthenticated client change it.
const PerPage = 24

// ErrUnauthorized is returned when wallhaven rejects the API key, or when a
// key is required and none was supplied.
var ErrUnauthorized = errors.New("wallhaven rejected the API key")

// ErrRateLimited is returned when the 45 requests/minute limit is exhausted
// and a retry did not clear it.
var ErrRateLimited = errors.New("wallhaven rate limit reached (45 requests/minute)")

// Client talks to the wallhaven API. The zero value is not usable; use New.
type Client struct {
	HTTP    *http.Client
	APIKey  string
	Version string
}

// New returns a Client. apiKey may be empty, in which case only SFW and
// sketchy content is reachable.
func New(apiKey, version string) *Client {
	return &Client{
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		APIKey:  apiKey,
		Version: version,
	}
}

// UserAgent identifies this tool to wallhaven. The API does not require a
// particular value, but sending an honest one is good manners for a client
// that runs against someone else's free service.
func (c *Client) UserAgent() string {
	v := c.Version
	if v == "" {
		v = "dev"
	}
	return "wallhaven-cli/" + v + " (+https://github.com/konsoli/wallhaven-cli)"
}

// get performs a GET and decodes the JSON body into out.
//
// A 429 is retried once after a short pause; anything past that is reported
// rather than hammered, since the whole tool needs at most three calls.
func (c *Client) get(endpoint string, params url.Values, out any) error {
	u := BaseURL + endpoint
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.UserAgent())
		req.Header.Set("Accept", "application/json")
		// The key goes in a header rather than the query string so it stays
		// out of shell history, proxy logs and crash reports.
		if c.APIKey != "" {
			req.Header.Set("X-API-Key", c.APIKey)
		}

		resp, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("contacting wallhaven: %w", err)
		}

		switch resp.StatusCode {
		case http.StatusOK:
			defer resp.Body.Close()
			if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
				return fmt.Errorf("decoding wallhaven response: %w", err)
			}
			return nil
		case http.StatusTooManyRequests:
			resp.Body.Close()
			if attempt == 0 {
				time.Sleep(2 * time.Second)
				continue
			}
			return ErrRateLimited
		case http.StatusUnauthorized:
			resp.Body.Close()
			return ErrUnauthorized
		case http.StatusNotFound:
			resp.Body.Close()
			return fmt.Errorf("wallhaven has no such wallpaper")
		default:
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			msg := strings.TrimSpace(string(body))
			if msg == "" {
				return fmt.Errorf("wallhaven returned %s", resp.Status)
			}
			return fmt.Errorf("wallhaven returned %s: %s", resp.Status, msg)
		}
	}
}

// ByID fetches a single wallpaper. This is the only endpoint that returns the
// uploader, so it doubles as the metadata lookup for search results.
func (c *Client) ByID(id string) (Wallpaper, error) {
	var r wallpaperResponse
	if err := c.get("/w/"+url.PathEscape(id), nil, &r); err != nil {
		return Wallpaper{}, err
	}
	return r.Data, nil
}

// Search runs a /search query.
func (c *Client) Search(params url.Values) (SearchResult, error) {
	var r SearchResult
	if err := c.get("/search", params, &r); err != nil {
		return SearchResult{}, err
	}
	return r, nil
}
