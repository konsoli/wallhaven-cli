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
	return Result{}, fmt.Errorf("%w: %s has no supported wallpaper mechanism", ErrUnsupported, runtime.GOOS)
}
