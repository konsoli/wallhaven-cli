// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

// Linux desktop backends.
//
// Every desktop environment has its own mechanism, so the environment is
// detected first and the matching commands are run. Where a desktop has a
// real lock screen setting it is set too; where it does not, the result says
// so rather than claiming success.
//
// This file carries no build tag on purpose: detection is pure argument
// building, and keeping it portable means the table-driven tests below run
// on a macOS development machine too, not only in Linux CI.
package wallpaper

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
)

// command is one external command to run, with its arguments already split.
// Nothing goes through a shell, so paths containing spaces are safe.
type command struct {
	name string
	args []string
	// optional marks a command whose failure is not fatal, used for keys
	// that only exist on newer versions of a desktop.
	optional bool
	// lock marks a command that configures the lock screen.
	lock bool
}

// backend is one desktop environment's way of setting a wallpaper.
type backend struct {
	name     string
	commands []command
	// noLockScreen explains why the lock screen was left alone, when there
	// is no supported way to set it.
	noLockScreen string
}

func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// detect picks a backend from the session environment.
//
// The Wayland compositors are checked first because they set
// XDG_CURRENT_DESKTOP too, then the desktop environment, then whatever
// standalone X11 setter is installed.
func detect(absPath string, getenv func(string) string, has func(string) bool) (backend, error) {
	fileURI := (&url.URL{Scheme: "file", Path: absPath}).String()

	if getenv("SWAYSOCK") != "" {
		return swayBackend(absPath), nil
	}
	if getenv("HYPRLAND_INSTANCE_SIGNATURE") != "" {
		return hyprlandBackend(absPath), nil
	}

	// XDG_CURRENT_DESKTOP is colon-separated and inconsistently cased,
	// e.g. "ubuntu:GNOME" or "X-Cinnamon".
	desktops := strings.Split(strings.ToLower(getenv("XDG_CURRENT_DESKTOP")), ":")
	desktops = append(desktops, strings.ToLower(getenv("DESKTOP_SESSION")))

	for _, d := range desktops {
		switch {
		case d == "":
			continue
		case strings.Contains(d, "gnome"), strings.Contains(d, "unity"),
			strings.Contains(d, "pantheon"), strings.Contains(d, "budgie"):
			return gnomeBackend(fileURI), nil
		case strings.Contains(d, "cinnamon"):
			return cinnamonBackend(fileURI), nil
		case strings.Contains(d, "mate"):
			return mateBackend(absPath, fileURI), nil
		case strings.Contains(d, "kde"), strings.Contains(d, "plasma"):
			return kdeBackend(absPath, fileURI, has), nil
		case strings.Contains(d, "xfce"):
			return xfceBackend(absPath)
		case strings.Contains(d, "lxqt"):
			return lxqtBackend(absPath), nil
		}
	}

	return x11Backend(absPath, has)
}

func gnomeBackend(fileURI string) backend {
	return backend{
		name: "GNOME (gsettings)",
		commands: []command{
			{name: "gsettings", args: []string{"set", "org.gnome.desktop.background", "picture-uri", fileURI}},
			// picture-uri-dark only exists from GNOME 42 on.
			{name: "gsettings", args: []string{"set", "org.gnome.desktop.background", "picture-uri-dark", fileURI}, optional: true},
			{name: "gsettings", args: []string{"set", "org.gnome.desktop.background", "picture-options", "zoom"}, optional: true},
			{name: "gsettings", args: []string{"set", "org.gnome.desktop.screensaver", "picture-uri", fileURI}, lock: true},
			{name: "gsettings", args: []string{"set", "org.gnome.desktop.screensaver", "picture-options", "zoom"}, optional: true, lock: true},
		},
	}
}

func cinnamonBackend(fileURI string) backend {
	return backend{
		name: "Cinnamon (gsettings)",
		commands: []command{
			{name: "gsettings", args: []string{"set", "org.cinnamon.desktop.background", "picture-uri", fileURI}},
			{name: "gsettings", args: []string{"set", "org.cinnamon.desktop.background", "picture-options", "zoom"}, optional: true},
			{name: "gsettings", args: []string{"set", "org.cinnamon.desktop.screensaver", "picture-uri", fileURI}, optional: true, lock: true},
		},
	}
}

func mateBackend(absPath, fileURI string) backend {
	return backend{
		name: "MATE (gsettings)",
		commands: []command{
			// MATE stores a plain path here, not a URI.
			{name: "gsettings", args: []string{"set", "org.mate.desktop.background", "picture-filename", absPath}},
			{name: "gsettings", args: []string{"set", "org.mate.desktop.background", "picture-options", "zoom"}, optional: true},
			{name: "gsettings", args: []string{"set", "org.mate.desktop.screensaver", "picture-uri", fileURI}, optional: true, lock: true},
		},
	}
}

func kdeBackend(absPath, fileURI string, has func(string) bool) backend {
	kwriteconfig := "kwriteconfig6"
	if !has(kwriteconfig) && has("kwriteconfig5") {
		kwriteconfig = "kwriteconfig5"
	}
	return backend{
		name: "KDE Plasma",
		commands: []command{
			{name: "plasma-apply-wallpaperimage", args: []string{absPath}},
			// The greeter only re-reads this when the lock screen next opens.
			{name: kwriteconfig, args: []string{
				"--file", "kscreenlockerrc",
				"--group", "Greeter", "--group", "Wallpaper",
				"--group", "org.kde.image", "--group", "General",
				"--key", "Image", fileURI,
			}, lock: true},
		},
	}
}

// xfceBackend sets every workspace's backdrop property. Xfce keeps one
// property per screen, monitor and workspace, so writing them all is what
// makes the wallpaper apply everywhere rather than on the current workspace.
func xfceBackend(absPath string) (backend, error) {
	out, err := exec.Command("xfconf-query", "-c", "xfce4-desktop", "-l").Output()
	if err != nil {
		return backend{}, fmt.Errorf("listing xfce4-desktop properties: %w", err)
	}

	b := backend{name: "Xfce (xfconf-query)", noLockScreen: "Xfce has no standard lock screen image setting"}
	for _, prop := range strings.Fields(string(out)) {
		if !strings.HasPrefix(prop, "/backdrop/") {
			continue
		}
		switch {
		case strings.HasSuffix(prop, "/last-image"):
			b.commands = append(b.commands, command{
				name: "xfconf-query",
				args: []string{"-c", "xfce4-desktop", "-p", prop, "-s", absPath},
			})
		case strings.HasSuffix(prop, "/image-style"):
			// 5 is "zoomed".
			b.commands = append(b.commands, command{
				name:     "xfconf-query",
				args:     []string{"-c", "xfce4-desktop", "-p", prop, "-s", "5"},
				optional: true,
			})
		}
	}
	if len(b.commands) == 0 {
		return backend{}, errors.New("xfce4-desktop exposes no /backdrop/.../last-image property to set")
	}
	return b, nil
}

func lxqtBackend(absPath string) backend {
	return backend{
		name:         "LXQt (pcmanfm-qt)",
		commands:     []command{{name: "pcmanfm-qt", args: []string{"--set-wallpaper", absPath, "--wallpaper-mode=fit"}}},
		noLockScreen: "LXQt has no standard lock screen image setting",
	}
}

func swayBackend(absPath string) backend {
	return backend{
		name:         "Sway (swaymsg)",
		commands:     []command{{name: "swaymsg", args: []string{"output", "*", "bg", absPath, "fill"}}},
		noLockScreen: "set the lock screen separately in your swaylock configuration",
	}
}

func hyprlandBackend(absPath string) backend {
	return backend{
		name: "Hyprland (hyprpaper)",
		// reload does preload, apply and unload in one step, so repeated
		// runs do not leak preloaded images.
		commands:     []command{{name: "hyprctl", args: []string{"hyprpaper", "reload", "," + absPath}}},
		noLockScreen: "set the lock screen separately in your hyprlock configuration",
	}
}

// x11Backend is the last resort: whichever standalone setter is installed.
func x11Backend(absPath string, has func(string) bool) (backend, error) {
	candidates := []command{
		{name: "feh", args: []string{"--bg-fill", absPath}},
		{name: "xwallpaper", args: []string{"--zoom", absPath}},
		{name: "nitrogen", args: []string{"--set-zoom-fill", "--save", absPath}},
	}
	for _, c := range candidates {
		if has(c.name) {
			return backend{
				name:         c.name,
				commands:     []command{c},
				noLockScreen: "no lock screen mechanism for a bare X11 session",
			}, nil
		}
	}
	return backend{}, errors.New(
		"could not detect a supported desktop environment. " +
			"Set XDG_CURRENT_DESKTOP, or install one of feh, xwallpaper or nitrogen, " +
			"or use --dl-only and set the wallpaper yourself")
}

func (b backend) run() (Result, error) {
	res := Result{Backend: b.name}
	lockSet := false
	var firstErr error

	for _, c := range b.commands {
		out, err := exec.Command(c.name, c.args...).CombinedOutput()
		if err != nil {
			if c.optional || c.lock {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %v: %s", c.name, err, strings.TrimSpace(string(out)))
			}
			continue
		}
		if c.lock {
			lockSet = true
		}
	}
	if firstErr != nil {
		return Result{}, firstErr
	}

	res.note("desktop set on all workspaces")
	switch {
	case lockSet:
		res.note("lock screen set")
	case b.noLockScreen != "":
		res.note("lock screen not set: " + b.noLockScreen)
	default:
		res.note("lock screen not set: the desktop's lock screen setting could not be written")
	}
	return res, nil
}
