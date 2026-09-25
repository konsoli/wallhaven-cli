// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

//go:build linux

package wallpaper

import "os"

// Set applies absPath as the desktop wallpaper. The backends it dispatches to
// live in linuxdesktop.go.
func Set(absPath string) (Result, error) {
	b, err := detect(absPath, os.Getenv, lookPath)
	if err != nil {
		return Result{}, err
	}
	return b.run()
}
