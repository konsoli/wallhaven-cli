// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

// Package download saves a wallpaper file to disk, or streams it to a writer.
package download

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ExpandDir resolves a user-supplied output directory to an absolute path.
//
// A leading ~ is expanded here rather than by the shell, because the flag is
// commonly quoted: --dir "~/Downloads/walls".
func ExpandDir(dir string) (string, error) {
	if dir == "" {
		return os.Getwd()
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("expanding ~ in %q: %w", dir, err)
		}
		dir = filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(dir, "~"), "/"))
	}
	return filepath.Abs(dir)
}

// Result describes where a wallpaper ended up.
type Result struct {
	// Path is the absolute path of the saved file.
	Path string
	// Reused is true when an identical file was already present and no
	// bytes were transferred.
	Reused bool
}

// Fetch downloads srcURL into dir and returns the absolute path of the file.
//
// The remote name is already wallhaven-<id>.<ext>, so it is used as-is. The
// download goes to a .part file that is renamed on success, so an interrupted
// run can never leave a truncated image behind for the wallpaper setter to
// pick up. expectedSize may be 0 when unknown.
func Fetch(client *http.Client, srcURL, userAgent, dir string, expectedSize int64) (Result, error) {
	outDir, err := ExpandDir(dir)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("creating %s: %w", outDir, err)
	}

	name := path.Base(srcURL)
	if name == "" || name == "." || name == "/" {
		return Result{}, fmt.Errorf("cannot derive a filename from %q", srcURL)
	}
	dest := filepath.Join(outDir, name)

	if expectedSize > 0 {
		if st, err := os.Stat(dest); err == nil && st.Size() == expectedSize {
			return Result{Path: dest, Reused: true}, nil
		}
	}

	resp, err := get(client, srcURL, userAgent)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	tmp, err := os.CreateTemp(outDir, name+".*.part")
	if err != nil {
		return Result{}, fmt.Errorf("creating temporary file in %s: %w", outDir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	written, err := io.Copy(tmp, resp.Body)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Result{}, fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if expectedSize > 0 && written != expectedSize {
		return Result{}, fmt.Errorf("incomplete download: got %d bytes, expected %d", written, expectedSize)
	}

	if err := os.Chmod(tmpName, 0o644); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return Result{}, fmt.Errorf("saving to %s: %w", dest, err)
	}
	return Result{Path: dest}, nil
}

// Stream copies the wallpaper to dst without touching the disk, so the tool
// can sit in a pipeline.
//
// Nothing is cached, so a repeated run downloads again, and a short download
// can only be reported once the bytes have already left: the consumer may be
// holding a truncated image, which is why that is an error and not a warning.
// expectedSize may be 0 when unknown.
func Stream(client *http.Client, srcURL, userAgent string, dst io.Writer, expectedSize int64) error {
	resp, err := get(client, srcURL, userAgent)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	written, err := io.Copy(dst, resp.Body)
	if err != nil {
		return fmt.Errorf("writing %s to stdout: %w", srcURL, err)
	}
	if expectedSize > 0 && written != expectedSize {
		return fmt.Errorf("incomplete download: wrote %d bytes, expected %d", written, expectedSize)
	}
	return nil
}

// get makes the download request. The caller closes the body.
func get(client *http.Client, srcURL, userAgent string) (*http.Response, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequest(http.MethodGet, srcURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("downloading %s: %w", srcURL, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("downloading %s: %s", srcURL, resp.Status)
	}
	return resp, nil
}
