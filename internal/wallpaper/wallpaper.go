// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

// Package wallpaper sets the desktop wallpaper on macOS and Linux.
package wallpaper

// Result describes what a Set call actually did, so the caller can report it
// honestly rather than printing a blanket success message.
type Result struct {
	// Backend names the mechanism used, e.g. "macOS wallpaper store" or
	// "GNOME (gsettings)".
	Backend string
	// Notes are extra lines worth showing the user: caveats, what the lock
	// screen did, which fallback was taken.
	Notes []string
}

func (r *Result) note(s string) { r.Notes = append(r.Notes, s) }
