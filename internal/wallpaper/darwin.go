// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

//go:build darwin

// macOS wallpaper support.
//
// Apple publishes no API for setting the wallpaper on every Space. Since
// macOS 14 the wallpaper lives in a binary property list:
//
//	~/Library/Application Support/com.apple.wallpaper/Store/Index.plist
//
// Its shape was established by reading the file on macOS 27, not from any
// documentation, so every assumption about it is re-checked at runtime and
// the code falls back to AppleScript if anything looks unfamiliar. The
// original file is backed up to Index.plist.wallhaven-cli.bak before the
// first write.
//
// The older ~/Library/Application Support/Dock/desktoppicture.db that most
// scripts on the web still manipulate no longer exists on macOS 26 and later.
package wallpaper

import (
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"time"

	"howett.net/plist"
)

const (
	storeRelPath  = "Library/Application Support/com.apple.wallpaper/Store/Index.plist"
	backupSuffix  = ".wallhaven-cli.bak"
	imageProvider = "com.apple.wallpaper.choice.image"
	agentProcess  = "WallpaperAgent"
)

// Set applies absPath as the desktop wallpaper.
func Set(absPath string) (Result, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Result{}, err
	}
	store := filepath.Join(home, storeRelPath)

	res, err := setViaStore(store, absPath)
	if err == nil {
		return res, nil
	}

	// The store is missing, malformed, or shaped in a way this code does not
	// recognise. AppleScript still works; it just cannot reach other Spaces.
	res, ascriptErr := setViaAppleScript(absPath)
	if ascriptErr != nil {
		return Result{}, fmt.Errorf("%w (AppleScript fallback also failed: %v)", err, ascriptErr)
	}
	res.note(fmt.Sprintf("fell back to AppleScript: %v", err))
	return res, nil
}

// setViaStore rewrites the wallpaper store so one image applies to every
// Space and every display.
func setViaStore(store, absPath string) (Result, error) {
	raw, err := os.ReadFile(store)
	if err != nil {
		return Result{}, fmt.Errorf("reading the wallpaper store: %w", err)
	}

	var root map[string]any
	if _, err := plist.Unmarshal(raw, &root); err != nil {
		return Result{}, fmt.Errorf("decoding the wallpaper store: %w", err)
	}

	// Before changing anything, prove this encoder can reproduce the file it
	// just read. A binary plist is not byte-stable across encoders, so the
	// check is semantic: encode, decode, compare. If a future macOS stores
	// something this library cannot represent, this catches it while the
	// file on disk is still untouched.
	if err := checkRoundTrip(root); err != nil {
		return Result{}, err
	}

	config, err := imageConfiguration(absPath)
	if err != nil {
		return Result{}, err
	}

	// AllSpacesAndDisplays is what the Wallpaper settings pane calls "Show on
	// all spaces". SystemDefault is what a newly created Space inherits.
	// Both have to carry the image for the choice to survive.
	applied := 0
	for _, key := range []string{"AllSpacesAndDisplays", "SystemDefault"} {
		ok, err := applyImage(root, key, config)
		if err != nil {
			return Result{}, err
		}
		if ok {
			applied++
		}
	}
	if applied == 0 {
		return Result{}, fmt.Errorf("the wallpaper store has no recognisable desktop entry")
	}

	// Per-Space and per-display overrides take precedence over
	// AllSpacesAndDisplays, so leaving them in place is exactly what makes a
	// wallpaper change land on the current Space only.
	root["Spaces"] = map[string]any{}
	root["Displays"] = map[string]any{}

	if err := backupOnce(store, raw); err != nil {
		return Result{}, err
	}
	if err := writeStore(store, root); err != nil {
		return Result{}, err
	}

	res := Result{Backend: "macOS wallpaper store"}
	res.note("desktop set on all spaces and all displays")
	// There is no separate lock screen image on macOS. Since macOS 14 the
	// Lock Screen shows the desktop picture, so setting one sets the other.
	res.note("lock screen follows the desktop picture on macOS")

	// WallpaperAgent caches the store; it re-reads it on relaunch and launchd
	// restarts it immediately.
	if out, err := exec.Command("killall", agentProcess).CombinedOutput(); err != nil {
		res.note(fmt.Sprintf("could not restart %s (%v: %s); log out and back in if the wallpaper does not change", agentProcess, err, out))
	}
	return res, nil
}

// checkRoundTrip verifies that decoding and re-encoding root loses nothing.
func checkRoundTrip(root map[string]any) error {
	encoded, err := plist.Marshal(root, plist.BinaryFormat)
	if err != nil {
		return fmt.Errorf("the wallpaper store cannot be re-encoded safely: %w", err)
	}
	var again map[string]any
	if _, err := plist.Unmarshal(encoded, &again); err != nil {
		return fmt.Errorf("the wallpaper store cannot be re-encoded safely: %w", err)
	}
	if !reflect.DeepEqual(root, again) {
		return fmt.Errorf("the wallpaper store did not survive a round trip; refusing to rewrite it")
	}
	return nil
}

// imageConfiguration builds the nested binary plist macOS stores for an image
// choice: {type: "imageFile", url: {relative: "file:///..."}}.
func imageConfiguration(absPath string) ([]byte, error) {
	fileURL := (&url.URL{Scheme: "file", Path: absPath}).String()
	return plist.Marshal(map[string]any{
		"type": "imageFile",
		"url":  map[string]any{"relative": fileURL},
	}, plist.BinaryFormat)
}

// applyImage points root[key].Desktop at the given image configuration.
// It reports whether the entry existed and was updated.
func applyImage(root map[string]any, key string, config []byte) (bool, error) {
	section, ok := root[key].(map[string]any)
	if !ok {
		return false, nil
	}
	desktop, ok := section["Desktop"].(map[string]any)
	if !ok {
		return false, nil
	}
	content, ok := desktop["Content"].(map[string]any)
	if !ok {
		return false, fmt.Errorf("%s.Desktop has no Content dictionary", key)
	}

	choice := map[string]any{
		"Provider":      imageProvider,
		"Configuration": config,
		"Files":         []any{},
	}

	// Replace the first choice in place so any sibling keys the running
	// macOS version added, and the user's placement and fill colour in
	// EncodedOptionValues, are preserved.
	if choices, ok := content["Choices"].([]any); ok && len(choices) > 0 {
		if existing, ok := choices[0].(map[string]any); ok {
			for k, v := range choice {
				existing[k] = v
			}
			content["Choices"] = []any{existing}
		} else {
			content["Choices"] = []any{choice}
		}
	} else {
		content["Choices"] = []any{choice}
	}

	desktop["LastSet"] = time.Now()
	desktop["LastUse"] = time.Now()
	return true, nil
}

// backupOnce keeps the first store this tool ever saw, so there is always a
// known-good file to restore. Later runs do not overwrite it.
func backupOnce(store string, raw []byte) error {
	backup := store + backupSuffix
	if _, err := os.Stat(backup); err == nil {
		return nil
	}
	if err := os.WriteFile(backup, raw, 0o644); err != nil {
		return fmt.Errorf("backing up the wallpaper store: %w", err)
	}
	return nil
}

// writeStore replaces the store atomically, so a crash mid-write cannot leave
// macOS with a half-written wallpaper database.
func writeStore(store string, root map[string]any) error {
	encoded, err := plist.Marshal(root, plist.BinaryFormat)
	if err != nil {
		return fmt.Errorf("encoding the wallpaper store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(store), "Index.plist.*.tmp")
	if err != nil {
		return fmt.Errorf("writing the wallpaper store: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	_, err = tmp.Write(encoded)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("writing the wallpaper store: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, store)
}

// setViaAppleScript is the fallback for macOS versions without the wallpaper
// store, or a store this code does not understand.
//
// System Events only writes the active Space of each display, and on macOS 26
// and later doing so makes macOS create per-Space entries, which turns "Show
// on all spaces" off.
func setViaAppleScript(absPath string) (Result, error) {
	script := fmt.Sprintf(
		`tell application "System Events" to tell every desktop to set picture to POSIX file %q`,
		absPath)
	if out, err := exec.Command("osascript", "-e", script).CombinedOutput(); err != nil {
		return Result{}, fmt.Errorf("osascript: %v: %s", err, out)
	}
	res := Result{Backend: "macOS AppleScript"}
	res.note("desktop set on the current space of each display only")
	res.note("lock screen follows the desktop picture on macOS")
	return res, nil
}
