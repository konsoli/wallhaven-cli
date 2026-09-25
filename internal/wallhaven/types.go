// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

// Package wallhaven is a minimal client for the wallhaven.cc API v1.
//
// Reference: https://wallhaven.cc/help/api
package wallhaven

// Wallpaper is one wallpaper as returned by either /search or /w/<id>.
//
// The two endpoints do not return the same fields: search result items carry
// no Uploader and no Tags, so those are only populated by ByID.
type Wallpaper struct {
	ID         string   `json:"id"`
	URL        string   `json:"url"`
	ShortURL   string   `json:"short_url"`
	Uploader   *User    `json:"uploader"`
	Views      int      `json:"views"`
	Favorites  int      `json:"favorites"`
	Source     string   `json:"source"`
	Purity     string   `json:"purity"`
	Category   string   `json:"category"`
	DimensionX int      `json:"dimension_x"`
	DimensionY int      `json:"dimension_y"`
	Resolution string   `json:"resolution"`
	Ratio      string   `json:"ratio"`
	FileSize   int64    `json:"file_size"`
	FileType   string   `json:"file_type"`
	CreatedAt  string   `json:"created_at"`
	Colors     []string `json:"colors"`
	Path       string   `json:"path"`
	Tags       []Tag    `json:"tags"`
}

// UploaderName returns the uploader's username, or "" when the endpoint did
// not provide one. Wallhaven reports removed accounts as the literal
// "deleted", which is passed through unchanged.
func (w Wallpaper) UploaderName() string {
	if w.Uploader == nil {
		return ""
	}
	return w.Uploader.Username
}

// User is the uploader block of a /w/<id> response.
type User struct {
	Username string `json:"username"`
	Group    string `json:"group"`
}

// Tag is a wallhaven tag, returned inline by /w/<id> and by /tag/<id>.
type Tag struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Alias    string `json:"alias"`
	Category string `json:"category"`
	Purity   string `json:"purity"`
}

// Meta is the pagination block attached to every /search response.
//
// Query is deliberately left as raw JSON: the API returns a string for a
// plain search and an object for an exact tag search (q=id:N).
type Meta struct {
	CurrentPage int    `json:"current_page"`
	LastPage    int    `json:"last_page"`
	PerPage     int    `json:"per_page"`
	Total       int    `json:"total"`
	Seed        string `json:"seed"`
	Query       any    `json:"query"`
}

// ResolvedTag returns the tag name wallhaven resolved an exact tag search to,
// and whether the response contained one.
func (m Meta) ResolvedTag() (string, bool) {
	q, ok := m.Query.(map[string]any)
	if !ok {
		return "", false
	}
	name, ok := q["tag"].(string)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

// SearchResult is a full /search response.
type SearchResult struct {
	Data []Wallpaper `json:"data"`
	Meta Meta        `json:"meta"`
}

type wallpaperResponse struct {
	Data Wallpaper `json:"data"`
}
