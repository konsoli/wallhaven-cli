// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

package wallpaper

import (
	"strings"
	"testing"
)

// envFunc builds a getenv stand-in from a map, so detection can be exercised
// without a real session.
func envFunc(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func hasAll(string) bool  { return true }
func hasNone(string) bool { return false }

const wall = "/home/pme/Pictures/wallhaven-g8d9dl.jpg"

func TestDetectBackendName(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"gnome on ubuntu", map[string]string{"XDG_CURRENT_DESKTOP": "ubuntu:GNOME"}, "GNOME (gsettings)"},
		{"gnome plain", map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"}, "GNOME (gsettings)"},
		{"gnome via session", map[string]string{"DESKTOP_SESSION": "gnome-xorg"}, "GNOME (gsettings)"},
		{"budgie", map[string]string{"XDG_CURRENT_DESKTOP": "Budgie:GNOME"}, "GNOME (gsettings)"},
		{"kde", map[string]string{"XDG_CURRENT_DESKTOP": "KDE"}, "KDE Plasma"},
		{"plasma wayland", map[string]string{"XDG_CURRENT_DESKTOP": "KDE", "WAYLAND_DISPLAY": "wayland-0"}, "KDE Plasma"},
		{"cinnamon", map[string]string{"XDG_CURRENT_DESKTOP": "X-Cinnamon"}, "Cinnamon (gsettings)"},
		{"mate", map[string]string{"XDG_CURRENT_DESKTOP": "MATE"}, "MATE (gsettings)"},
		{"lxqt", map[string]string{"XDG_CURRENT_DESKTOP": "LXQt"}, "LXQt (pcmanfm-qt)"},
		// A compositor sets XDG_CURRENT_DESKTOP too, so its own marker has
		// to win.
		{"sway", map[string]string{"SWAYSOCK": "/run/user/1000/sway-ipc.sock", "XDG_CURRENT_DESKTOP": "sway"}, "Sway (swaymsg)"},
		{"hyprland", map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "abc123", "XDG_CURRENT_DESKTOP": "Hyprland"}, "Hyprland (hyprpaper)"},
		{"bare x11 falls back", map[string]string{}, "feh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := detect(wall, envFunc(tt.env), hasAll)
			if err != nil {
				t.Fatalf("detect() error = %v", err)
			}
			if b.name != tt.want {
				t.Errorf("backend = %q, want %q", b.name, tt.want)
			}
		})
	}
}

func TestDetectArgv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want [][]string
	}{
		{
			"gnome sets background, dark variant and lock screen",
			map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"},
			[][]string{
				{"gsettings", "set", "org.gnome.desktop.background", "picture-uri", "file://" + wall},
				{"gsettings", "set", "org.gnome.desktop.background", "picture-uri-dark", "file://" + wall},
				{"gsettings", "set", "org.gnome.desktop.background", "picture-options", "zoom"},
				{"gsettings", "set", "org.gnome.desktop.screensaver", "picture-uri", "file://" + wall},
				{"gsettings", "set", "org.gnome.desktop.screensaver", "picture-options", "zoom"},
			},
		},
		{
			"mate takes a plain path for the background",
			map[string]string{"XDG_CURRENT_DESKTOP": "MATE"},
			[][]string{
				{"gsettings", "set", "org.mate.desktop.background", "picture-filename", wall},
				{"gsettings", "set", "org.mate.desktop.background", "picture-options", "zoom"},
				{"gsettings", "set", "org.mate.desktop.screensaver", "picture-uri", "file://" + wall},
			},
		},
		{
			"kde uses plasma-apply-wallpaperimage and kscreenlockerrc",
			map[string]string{"XDG_CURRENT_DESKTOP": "KDE"},
			[][]string{
				{"plasma-apply-wallpaperimage", wall},
				{"kwriteconfig6", "--file", "kscreenlockerrc",
					"--group", "Greeter", "--group", "Wallpaper",
					"--group", "org.kde.image", "--group", "General",
					"--key", "Image", "file://" + wall},
			},
		},
		{
			"sway targets every output",
			map[string]string{"SWAYSOCK": "/run/sway"},
			[][]string{{"swaymsg", "output", "*", "bg", wall, "fill"}},
		},
		{
			"hyprland reloads in one step",
			map[string]string{"HYPRLAND_INSTANCE_SIGNATURE": "sig"},
			[][]string{{"hyprctl", "hyprpaper", "reload", "," + wall}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := detect(wall, envFunc(tt.env), hasAll)
			if err != nil {
				t.Fatalf("detect() error = %v", err)
			}
			got := argv(b)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d commands, want %d:\n got %v\nwant %v", len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if strings.Join(got[i], "\x00") != strings.Join(tt.want[i], "\x00") {
					t.Errorf("command %d = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// Plasma 5 systems only have kwriteconfig5.
func TestKDEFallsBackToKwriteconfig5(t *testing.T) {
	has := func(name string) bool { return name == "kwriteconfig5" }
	b, err := detect(wall, envFunc(map[string]string{"XDG_CURRENT_DESKTOP": "KDE"}), has)
	if err != nil {
		t.Fatal(err)
	}
	if got := argv(b)[1][0]; got != "kwriteconfig5" {
		t.Errorf("lock screen command = %q, want kwriteconfig5", got)
	}
}

func TestX11FallbackPrefersInstalledSetter(t *testing.T) {
	for _, want := range []string{"feh", "xwallpaper", "nitrogen"} {
		has := func(name string) bool { return name == want }
		b, err := detect(wall, envFunc(map[string]string{}), has)
		if err != nil {
			t.Fatalf("detect() with only %s installed: %v", want, err)
		}
		if b.name != want {
			t.Errorf("backend = %q, want %q", b.name, want)
		}
	}
}

func TestDetectFailsWithNothingInstalled(t *testing.T) {
	_, err := detect(wall, envFunc(map[string]string{}), hasNone)
	if err == nil {
		t.Fatal("detect() = nil error with no desktop and no setter, want an error")
	}
	if !strings.Contains(err.Error(), "--dl-only") {
		t.Errorf("error should point at --dl-only, got: %v", err)
	}
}

// A path with spaces must survive: nothing goes through a shell, and file
// URIs are percent-encoded.
func TestSpacesInPath(t *testing.T) {
	p := "/home/pme/My Walls/wallhaven-g8d9dl.jpg"
	b, err := detect(p, envFunc(map[string]string{"XDG_CURRENT_DESKTOP": "GNOME"}), hasAll)
	if err != nil {
		t.Fatal(err)
	}
	uri := argv(b)[0][4]
	if uri != "file:///home/pme/My%20Walls/wallhaven-g8d9dl.jpg" {
		t.Errorf("file URI = %q, want the space percent-encoded", uri)
	}

	b, err = detect(p, envFunc(map[string]string{"SWAYSOCK": "/run/sway"}), hasAll)
	if err != nil {
		t.Fatal(err)
	}
	if got := argv(b)[0][4]; got != p {
		t.Errorf("swaymsg path argument = %q, want the raw path %q", got, p)
	}
}

func argv(b backend) [][]string {
	out := make([][]string, 0, len(b.commands))
	for _, c := range b.commands {
		out = append(out, append([]string{c.name}, c.args...))
	}
	return out
}
