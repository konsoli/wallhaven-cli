# wallhaven-cli

Fetch a wallpaper from [wallhaven.cc](https://wallhaven.cc) and set it as your
desktop wallpaper. macOS and Linux, one binary, no runtime dependencies.

```
$ wallhaven-cli --toplist --dir ~/Pictures/walls
Saved     /Users/pme/Pictures/walls/wallhaven-vp2q78.jpg
Uploader  AndorSwallow
Category  anime
URL       https://wallhaven.cc/w/vp2q78
Size      3840x2160

macOS wallpaper store
  desktop set on all spaces and all displays
  lock screen follows the desktop picture on macOS
```

## Install

Homebrew:

```
brew install konsoli/tap/wallhaven-cli
```

From source:

```
go install github.com/konsoli/wallhaven-cli@latest
```

Or download a binary from the [releases page](https://github.com/konsoli/wallhaven-cli/releases).

## Usage

Running `wallhaven-cli` with no arguments prints the full help.

### Sources

Exactly one is required.

| Flag | What it picks |
| --- | --- |
| `--random` | a completely random wallpaper |
| `--latest` | the most recently uploaded wallpaper |
| `--toplist` | a random wallpaper from the toplist (last month) |
| `--hotlist` | a random wallpaper from the hotlist |
| `--id <ID>` | a specific wallpaper, e.g. `g8d9dl` for `wallhaven.cc/w/g8d9dl` |
| `--tag <TAGID>` | a random wallpaper with an exact tag id, e.g. `2321` for #pixel art |
| `--user <USERNAME>` | a random wallpaper uploaded by that user |
| `--search <PHRASE>` | a random wallpaper matching a search phrase |

`--latest` returns the single newest upload. Every other source picks at
random from the matching wallpapers.

### Purity

`--purity` takes the same three bits the wallhaven URL uses, in the order
`[sfw][sketchy][nsfw]`. The default is `100`.

| Bits | Meaning | Needs an API key |
| --- | --- | --- |
| `100` | sfw only (default) | no |
| `110` | sfw + sketchy | no |
| `010` | sketchy only | no |
| `111` | sfw + sketchy + nsfw | yes |
| `011` | sketchy + nsfw | yes |
| `001` | nsfw only | yes |
| `101` | sfw + nsfw | yes |

Note that `010` is sketchy, not nsfw. Anything with the third bit set needs an
API key from [your account settings](https://wallhaven.cc/settings/account):

```
export WALLHAVEN_API_KEY=...
wallhaven-cli --search "anime woman" --purity 111
```

Without a key, wallhaven answers `200 OK` and silently drops the nsfw results
rather than reporting an error, so `wallhaven-cli` refuses up front instead of
handing you a quietly filtered result.

### Output

```
--dir <PATH>     where to save, default: the current directory
--dl-only        download only, do not set the wallpaper
--quiet          print only the saved file path
```

`--dir` expands a leading `~` itself, so `--dir "~/Downloads/walls"` works
even when the shell never sees the tilde unquoted.

The file keeps its wallhaven name, `wallhaven-<id>.<ext>`. Re-fetching a
wallpaper you already have skips the download.

### Examples

```
wallhaven-cli --random
wallhaven-cli --latest
wallhaven-cli --toplist
wallhaven-cli --hotlist
wallhaven-cli --id g8d9dl
wallhaven-cli --tag 2321
wallhaven-cli --user helminuri
wallhaven-cli --search "yosemite sunset"
wallhaven-cli --search "anime woman" --purity 111
wallhaven-cli --random --dl-only
wallhaven-cli --random --dir "~/Downloads/walls"
wallhaven-cli --toplist --purity 110 --dir ~/Pictures/walls
```

Exit status is `0` on success, `2` for a bad command line, and `1` for
anything else.

## How the wallpaper gets set

### macOS

Apple publishes no API for setting the wallpaper on every Space, so
`wallhaven-cli` writes the store macOS keeps at

```
~/Library/Application Support/com.apple.wallpaper/Store/Index.plist
```

pointing both `AllSpacesAndDisplays` and `SystemDefault` at the new image,
clearing the per-Space and per-display overrides that would otherwise confine
the change to the current Space, and restarting `WallpaperAgent`. Your fill
and placement settings are left alone.

That file's layout is not documented by Apple. It was established by reading
it on macOS 27, so before every write `wallhaven-cli` checks that it can
decode and re-encode the file without losing anything, and backs the original
up to `Index.plist.wallhaven-cli.bak` the first time it runs. If the file ever
stops looking familiar, the tool falls back to AppleScript, which only reaches
the current Space of each display, and says so.

Setting the wallpaper the AppleScript way — which is what most scripts and
older tools do — makes macOS fill the store with per-Space entries and strip
the all-spaces desktop entry entirely. `wallhaven-cli` rebuilds that entry and
clears the overrides, so running it once is enough to undo the damage. To undo
everything instead:

```
cp ~/Library/Application\ Support/com.apple.wallpaper/Store/Index.plist.wallhaven-cli.bak \
   ~/Library/Application\ Support/com.apple.wallpaper/Store/Index.plist
killall WallpaperAgent
```

Since macOS 14 there is no separate lock screen image: the Lock Screen shows
the desktop picture, so setting one sets the other.

The `~/Library/Application Support/Dock/desktoppicture.db` that most scripts
on the web still manipulate no longer exists on macOS 26 and later.

### Linux

The session is detected and the matching tool is used.

| Desktop | Wallpaper | Lock screen |
| --- | --- | --- |
| GNOME, Budgie, Pantheon, Unity | `gsettings org.gnome.desktop.background` (light and dark) | `org.gnome.desktop.screensaver` |
| KDE Plasma | `plasma-apply-wallpaperimage` | `kwriteconfig6` on `kscreenlockerrc` |
| Cinnamon | `gsettings org.cinnamon.desktop.background` | `org.cinnamon.desktop.screensaver` |
| MATE | `gsettings org.mate.desktop.background` | `org.mate.desktop.screensaver` |
| Xfce | `xfconf-query`, every workspace's backdrop | not supported |
| LXQt | `pcmanfm-qt --set-wallpaper` | not supported |
| Sway | `swaymsg output '*' bg` | configure in swaylock |
| Hyprland | `hyprctl hyprpaper reload` | configure in hyprlock |
| bare X11 | `feh`, `xwallpaper` or `nitrogen` | not supported |

The KDE lock screen picks up the change the next time it opens. Where a
desktop has no lock screen mechanism, `wallhaven-cli` says the lock screen was
not set rather than claiming success.

The Linux backends are covered by tests that assert the exact command line
each desktop produces, but have not yet been exercised on real hardware for
every desktop in the table. Reports welcome.

## Using the wallhaven API politely

- The API is limited to 45 requests per minute. `wallhaven-cli` makes at most
  three per run and backs off on a `429`.
- Your API key is sent in the `X-API-Key` header, never in the URL, so it
  stays out of shell history and proxy logs.
- Every run prints the uploader, the category and the wallpaper's page URL.
  Wallhaven's [rules](https://wallhaven.cc/rules) ask that the author be
  attributed wherever possible, and those three fields are what make that
  possible once the file is on your disk.

`wallhaven-cli` is not affiliated with, endorsed by, or connected to
wallhaven.cc. All images remain the property of their original owners.

## License

GPL-2.0-only. See [COPYING](COPYING). Every source file carries an
`SPDX-License-Identifier` tag; full license texts are in
[LICENSES/](LICENSES/).
