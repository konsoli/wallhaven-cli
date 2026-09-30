// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// The --dir example in the help text is quoted, so the shell never expands
// the tilde and this code has to.
func TestExpandDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		in   string
		want string
	}{
		{"~/Downloads/walls", filepath.Join(home, "Downloads", "walls")},
		{"~", home},
		{"", cwd},
		{"/tmp/walls", "/tmp/walls"},
		{"walls", filepath.Join(cwd, "walls")},
		// Only a leading ~ is a home reference; a literal one stays put.
		{"/tmp/~walls", "/tmp/~walls"},
	}
	for _, tt := range tests {
		got, err := ExpandDir(tt.in)
		if err != nil {
			t.Errorf("ExpandDir(%q) error = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ExpandDir(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFetchWritesFileAndReuses(t *testing.T) {
	body := []byte("not really a jpeg, but the right number of bytes")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write(body)
	}))
	defer srv.Close()

	dir := t.TempDir()
	src := srv.URL + "/full/g8/wallhaven-g8d9dl.jpg"

	res, err := Fetch(srv.Client(), src, "test", dir, int64(len(body)))
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	want := filepath.Join(dir, "wallhaven-g8d9dl.jpg")
	if res.Path != want {
		t.Errorf("Path = %q, want %q", res.Path, want)
	}
	if res.Reused {
		t.Error("Reused = true on a first download")
	}
	got, err := os.ReadFile(res.Path)
	if err != nil || string(got) != string(body) {
		t.Errorf("file contents = %q (err %v), want %q", got, err, body)
	}

	// A second run with a matching size must not re-download.
	res2, err := Fetch(srv.Client(), src, "test", dir, int64(len(body)))
	if err != nil {
		t.Fatalf("second Fetch() error = %v", err)
	}
	if !res2.Reused {
		t.Error("Reused = false when the file was already present")
	}
	if hits != 1 {
		t.Errorf("server hits = %d, want 1", hits)
	}
}

// A truncated response must not leave a partial image behind for the
// wallpaper setter to pick up.
func TestFetchRejectsShortBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("short"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	if _, err := Fetch(srv.Client(), srv.URL+"/wallhaven-x.jpg", "test", dir, 9999); err == nil {
		t.Fatal("Fetch() = nil error for a truncated body, want an error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("directory holds %d leftover files, want 0", len(entries))
	}
}

func TestFetchReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	if _, err := Fetch(srv.Client(), srv.URL+"/wallhaven-x.jpg", "test", t.TempDir(), 0); err == nil {
		t.Fatal("Fetch() = nil error for a 404, want an error")
	}
}

// --fork pipes the image, so the bytes have to arrive unchanged and nothing
// may be left on disk for the user to clean up afterwards.
func TestStreamWritesToTheWriterAndKeepsNoFile(t *testing.T) {
	body := []byte("not really a jpeg, but the right number of bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body)
	}))
	defer srv.Close()

	// Run from an empty directory, so anything written relative to the
	// working directory would show up in the check below.
	dir := t.TempDir()
	back, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(back)

	var out bytes.Buffer
	if err := Stream(srv.Client(), srv.URL+"/full/g8/wallhaven-g8d9dl.jpg", "test", &out, int64(len(body))); err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	if !bytes.Equal(out.Bytes(), body) {
		t.Errorf("wrote %q, want %q", out.Bytes(), body)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("directory holds %d files, want 0", len(entries))
	}
}

// The bytes are already gone by the time the length is known, so the only
// honest thing left is to fail loudly rather than let the pipeline treat a
// truncated image as a whole one.
func TestStreamRejectsShortBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("short"))
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := Stream(srv.Client(), srv.URL+"/wallhaven-x.jpg", "test", &out, 9999); err == nil {
		t.Fatal("Stream() = nil error for a truncated body, want an error")
	}
}

func TestStreamReportsAnHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer srv.Close()

	var out bytes.Buffer
	if err := Stream(srv.Client(), srv.URL+"/wallhaven-x.jpg", "test", &out, 0); err == nil {
		t.Fatal("Stream() = nil error for a 404, want an error")
	}
	if out.Len() != 0 {
		t.Errorf("wrote %d bytes for a failed request, want 0", out.Len())
	}
}
