// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

// Command wallhaven-cli fetches a wallpaper from wallhaven.cc and sets it as
// the desktop wallpaper.
//
// wallhaven-cli is not affiliated with wallhaven.cc.
// All images remain the property of their original owners.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/konsoli/wallhaven-cli/internal/download"
	"github.com/konsoli/wallhaven-cli/internal/wallhaven"
	"github.com/konsoli/wallhaven-cli/internal/wallpaper"
)

// Stamped by the linker at release time; see .goreleaser.yaml.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usage = `wallhaven-cli ` + "—" + ` fetch a wallpaper from wallhaven.cc and set it as your desktop wallpaper

USAGE
  wallhaven-cli <source> [filters] [output]

SOURCE (exactly one)
  --random              a completely random wallpaper
  --latest              the most recently uploaded wallpaper
  --toplist             a random wallpaper from the toplist (last month)
  --hotlist             a random wallpaper from the hotlist
  --id <ID>             a specific wallpaper by id
  --tag <TAGID>         a random wallpaper with an exact tag id
  --user <USERNAME>     a random wallpaper uploaded by a user
  --search <PHRASE>     a random wallpaper matching a search phrase

FILTERS
  --purity <bits>       [sfw][sketchy][nsfw], default 100

                          100  sfw only                  (default)
                          110  sfw + sketchy
                          111  sfw + sketchy + nsfw    *
                          010  sketchy only
                          011  sketchy + nsfw          *
                          001  nsfw only               *
                          101  sfw + nsfw              *

                        * needs an API key: set WALLHAVEN_API_KEY
                          or pass --api-key. Without one, wallhaven
                          silently drops nsfw results.

OUTPUT
  --dir <PATH>          where to save, default: current directory
  --dl-only             download only, do not set the wallpaper

OTHER
  --api-key <KEY>       overrides WALLHAVEN_API_KEY
  --quiet               only print the saved file path
  --version
  -h, --help

EXAMPLES
  wallhaven-cli --random
  wallhaven-cli --latest
  wallhaven-cli --toplist
  wallhaven-cli --hotlist
  wallhaven-cli --id g8d9dl                    https://wallhaven.cc/w/g8d9dl
  wallhaven-cli --tag 2321                     #pixel art
  wallhaven-cli --user helminuri               uploads by helminuri
  wallhaven-cli --search "yosemite sunset"
  wallhaven-cli --search "anime woman" --purity 111
  wallhaven-cli --random --dl-only
  wallhaven-cli --random --dir "~/Downloads/walls"
  wallhaven-cli --toplist --purity 110 --dir ~/Pictures/walls

wallhaven-cli is not affiliated with wallhaven.cc.
All images remain the property of their original owners.
`

// exitUsage is the status for a malformed command line, distinct from the
// status for a request that was well formed but failed.
const exitUsage = 2

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		var ue *usageError
		if errors.As(err, &ue) {
			fmt.Fprintf(os.Stderr, "wallhaven-cli: %v\n\nRun wallhaven-cli --help for the full list of options.\n", ue.err)
			os.Exit(exitUsage)
		}
		fmt.Fprintf(os.Stderr, "wallhaven-cli: %v\n", err)
		os.Exit(1)
	}
}

type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

func usagef(format string, a ...any) error {
	return &usageError{fmt.Errorf(format, a...)}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("wallhaven-cli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }

	var (
		random  = fs.Bool("random", false, "")
		latest  = fs.Bool("latest", false, "")
		toplist = fs.Bool("toplist", false, "")
		hotlist = fs.Bool("hotlist", false, "")
		id      = fs.String("id", "", "")
		tag     = fs.String("tag", "", "")
		user    = fs.String("user", "", "")
		search  = fs.String("search", "", "")
		purity  = fs.String("purity", wallhaven.PurityDefault, "")
		dir     = fs.String("dir", "", "")
		dlOnly  = fs.Bool("dl-only", false, "")
		apiKey  = fs.String("api-key", "", "")
		quiet   = fs.Bool("quiet", false, "")
		showVer = fs.Bool("version", false, "")
	)

	// No arguments at all is a request for help, not an error.
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return nil
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(stdout, usage)
			return nil
		}
		return &usageError{err}
	}
	if *showVer {
		fmt.Fprintf(stdout, "wallhaven-cli %s (commit %s, built %s)\n", version, commit, date)
		return nil
	}
	if rest := fs.Args(); len(rest) > 0 {
		return usagef("unexpected argument %q; every option takes the form --flag or --flag value", rest[0])
	}

	source, err := selectSource(map[string]bool{
		"random": *random, "latest": *latest, "toplist": *toplist, "hotlist": *hotlist,
	}, map[string]string{
		"id": *id, "tag": *tag, "user": *user, "search": *search,
	})
	if err != nil {
		return err
	}

	key := *apiKey
	if key == "" {
		key = os.Getenv("WALLHAVEN_API_KEY")
	}

	needsKey, err := wallhaven.ValidatePurity(*purity)
	if err != nil {
		return &usageError{err}
	}
	if needsKey && key == "" {
		// Worth failing on rather than warning about: wallhaven answers 200
		// and quietly returns nothing (or only sfw) for an nsfw purity
		// without a key, so an unchecked run looks like "no results".
		return usagef("purity %s (%s) needs a wallhaven API key.\n"+
			"  Get one at https://wallhaven.cc/settings/account, then either\n"+
			"    export WALLHAVEN_API_KEY=...\n"+
			"  or pass --api-key <KEY>",
			*purity, wallhaven.PurityLabel(*purity))
	}

	client := wallhaven.New(key, version)
	rnd := rand.New(rand.NewSource(time.Now().UnixNano()))

	w, note, err := client.Pick(source, *purity, rnd)
	if err != nil {
		return err
	}
	if w.Path == "" {
		return fmt.Errorf("wallhaven returned a wallpaper with no download URL")
	}

	// Search results carry no uploader, so fill it in from /w/<id>. A
	// failure here is cosmetic; the download is still worth doing.
	if w.UploaderName() == "" {
		if full, ferr := client.ByID(w.ID); ferr == nil {
			w = full
		}
	}

	res, err := download.Fetch(&http.Client{Timeout: 5 * time.Minute},
		w.Path, client.UserAgent(), *dir, w.FileSize)
	if err != nil {
		return err
	}

	if *quiet {
		fmt.Fprintln(stdout, res.Path)
	} else {
		printWallpaper(stdout, w, res, note)
	}

	if *dlOnly {
		return nil
	}

	set, err := wallpaper.Set(res.Path)
	if err != nil {
		return fmt.Errorf("the wallpaper was saved to %s but could not be applied: %w", res.Path, err)
	}
	if !*quiet {
		fmt.Fprintf(stdout, "\n%s\n", set.Backend)
		for _, n := range set.Notes {
			fmt.Fprintf(stdout, "  %s\n", n)
		}
	}
	return nil
}

// selectSource enforces that exactly one source flag was given.
func selectSource(flags map[string]bool, values map[string]string) (wallhaven.Source, error) {
	var chosen []wallhaven.Source
	for name, on := range flags {
		if on {
			chosen = append(chosen, wallhaven.Source{Flag: name})
		}
	}
	for name, v := range values {
		if v != "" {
			chosen = append(chosen, wallhaven.Source{Flag: name, Arg: v})
		}
	}
	sort.Slice(chosen, func(i, j int) bool { return chosen[i].Flag < chosen[j].Flag })

	switch len(chosen) {
	case 1:
		return chosen[0], nil
	case 0:
		return wallhaven.Source{}, usagef("pick a source: one of --random, --latest, --toplist, --hotlist, --id, --tag, --user or --search")
	default:
		names := make([]string, len(chosen))
		for i, s := range chosen {
			names[i] = "--" + s.Flag
		}
		return wallhaven.Source{}, usagef("pick one source, not %s", strings.Join(names, " and "))
	}
}

// printWallpaper reports what was fetched. Uploader, category and URL are
// always shown: wallhaven's rules ask that the author be attributed wherever
// possible, and these are what make that possible at the point of use.
func printWallpaper(out io.Writer, w wallhaven.Wallpaper, res download.Result, note string) {
	uploader := w.UploaderName()
	if uploader == "" {
		uploader = "unknown"
	}

	label := "Saved"
	if res.Reused {
		label = "Have"
	}
	fmt.Fprintf(out, "%-9s %s\n", label, res.Path)
	fmt.Fprintf(out, "%-9s %s\n", "Uploader", uploader)
	fmt.Fprintf(out, "%-9s %s\n", "Category", w.Category)
	fmt.Fprintf(out, "%-9s %s\n", "URL", w.URL)
	if w.Resolution != "" {
		fmt.Fprintf(out, "%-9s %s\n", "Size", w.Resolution)
	}
	if w.Source != "" {
		fmt.Fprintf(out, "%-9s %s\n", "Source", w.Source)
	}
	if note != "" {
		fmt.Fprintf(out, "%-9s %s\n", "Tag", note)
	}
}
