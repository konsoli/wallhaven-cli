// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

//go:build !darwin && !linux

package wallpaper

import (
	"fmt"
	"runtime"
)

// Set is not implemented on this platform.
func Set(absPath string) (Result, error) {
	return Result{}, fmt.Errorf("setting the wallpaper is not supported on %s; use --dl-only", runtime.GOOS)
}
