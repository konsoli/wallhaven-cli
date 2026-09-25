// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

//go:build darwin

package wallpaper

import (
	"testing"
	"time"

	"howett.net/plist"
)

// A binary plist keeps dates as float64 seconds since 2001, so a timestamp
// macOS wrote can come back a few nanoseconds off. Treating that as a
// structural change pushed the tool onto the AppleScript fallback for no
// reason, so the round-trip check has to tolerate it.
func TestCheckRoundTripToleratesDateRounding(t *testing.T) {
	// This is a real LastSet value read from a macOS 27 wallpaper store.
	root := map[string]any{
		"AllSpacesAndDisplays": map[string]any{
			"Type":    "individual",
			"LastSet": time.Date(2026, 9, 22, 8, 15, 23, 498843000, time.UTC),
		},
		"Spaces":   map[string]any{},
		"Displays": map[string]any{},
	}
	if err := checkRoundTrip(root); err != nil {
		t.Fatalf("checkRoundTrip() = %v, want nil for a sub-second timestamp", err)
	}
}

func TestEquivalent(t *testing.T) {
	base := time.Date(2026, 9, 22, 8, 15, 23, 498843000, time.UTC)

	tests := []struct {
		name string
		a, b any
		want bool
	}{
		{"identical maps", map[string]any{"a": "b"}, map[string]any{"a": "b"}, true},
		{"different values", map[string]any{"a": "b"}, map[string]any{"a": "c"}, false},
		{"missing key", map[string]any{"a": "b", "c": "d"}, map[string]any{"a": "b"}, false},
		{"extra key", map[string]any{"a": "b"}, map[string]any{"a": "b", "c": "d"}, false},
		{"dates within tolerance", base, base.Add(-46 * time.Nanosecond), true},
		{"dates beyond tolerance", base, base.Add(2 * time.Second), false},
		{"date vs string", base, "2026-09-22", false},
		{"nested date drift",
			map[string]any{"x": []any{map[string]any{"t": base}}},
			map[string]any{"x": []any{map[string]any{"t": base.Add(50 * time.Nanosecond)}}},
			true},
		{"slice length differs", []any{1, 2}, []any{1}, false},
		{"byte slices equal", []byte{1, 2, 3}, []byte{1, 2, 3}, true},
		{"byte slices differ", []byte{1, 2, 3}, []byte{1, 2, 4}, false},
		{"empty containers", map[string]any{}, map[string]any{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := equivalent(tt.a, tt.b); got != tt.want {
				t.Errorf("equivalent() = %v, want %v", got, tt.want)
			}
		})
	}
}

// A store whose desktop entry macOS has removed must be rebuilt, not skipped.
// After the AppleScript fallback runs, macOS rewrites AllSpacesAndDisplays
// with no Desktop key at all and a Type of "idle".
func TestApplyImageRebuildsMissingDesktop(t *testing.T) {
	options := []byte("encoded-option-values")
	root := map[string]any{
		"AllSpacesAndDisplays": map[string]any{
			"Type": "idle",
			"Idle": map[string]any{"Content": map[string]any{}},
		},
		"SystemDefault": map[string]any{
			"Type": "individual",
			"Desktop": map[string]any{
				"Content": map[string]any{
					"Choices":             []any{map[string]any{"Provider": "old"}},
					"EncodedOptionValues": options,
				},
			},
		},
	}

	config, err := imageConfiguration("/Users/pme/Pictures/wallhaven-g8d9dl.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if got := existingOptionValues(root); string(got) != string(options) {
		t.Fatalf("existingOptionValues() = %q, want the SystemDefault options", got)
	}
	for _, key := range []string{"AllSpacesAndDisplays", "SystemDefault"} {
		if err := applyImage(root, key, config, options); err != nil {
			t.Fatalf("applyImage(%s) error = %v", key, err)
		}
	}

	for _, key := range []string{"AllSpacesAndDisplays", "SystemDefault"} {
		section := root[key].(map[string]any)
		if section["Type"] != "individual" {
			t.Errorf("%s Type = %v, want individual", key, section["Type"])
		}
		desktop, ok := section["Desktop"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no Desktop entry", key)
		}
		content := desktop["Content"].(map[string]any)
		if string(content["EncodedOptionValues"].([]byte)) != string(options) {
			t.Errorf("%s lost the user's placement options", key)
		}
		choices := content["Choices"].([]any)
		if len(choices) != 1 {
			t.Fatalf("%s has %d choices, want 1", key, len(choices))
		}
		choice := choices[0].(map[string]any)
		if choice["Provider"] != imageProvider {
			t.Errorf("%s Provider = %v, want %v", key, choice["Provider"], imageProvider)
		}
		var cfg map[string]any
		if _, err := plist.Unmarshal(choice["Configuration"].([]byte), &cfg); err != nil {
			t.Fatal(err)
		}
		got := cfg["url"].(map[string]any)["relative"]
		if got != "file:///Users/pme/Pictures/wallhaven-g8d9dl.jpg" {
			t.Errorf("%s image URL = %v", key, got)
		}
	}
	// The Idle entry, which is the screen saver, must survive untouched.
	if _, ok := root["AllSpacesAndDisplays"].(map[string]any)["Idle"]; !ok {
		t.Error("the screen saver entry was dropped")
	}
}

// Paths with spaces have to be percent-encoded in the file URL.
func TestImageConfigurationEncodesPath(t *testing.T) {
	config, err := imageConfiguration("/Users/pme/My Walls/wallhaven-g8d9dl.jpg")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if _, err := plist.Unmarshal(config, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["type"] != "imageFile" {
		t.Errorf("type = %v, want imageFile", cfg["type"])
	}
	want := "file:///Users/pme/My%20Walls/wallhaven-g8d9dl.jpg"
	if got := cfg["url"].(map[string]any)["relative"]; got != want {
		t.Errorf("url = %v, want %v", got, want)
	}
}
