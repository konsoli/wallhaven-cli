// SPDX-License-Identifier: GPL-2.0-only
// Copyright (c) 2026 Paul Merisalu

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/konsoli/wallhaven-cli/internal/wallhaven"
)

// runArgs exercises the command line without touching the network. Every
// case here must fail (or print help) before any request is made.
func runArgs(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	err = run(args, &out, &errb)
	return out.String(), errb.String(), err
}

func isUsage(err error) bool {
	var ue *usageError
	return errors.As(err, &ue)
}

func TestNoArgumentsPrintsHelp(t *testing.T) {
	stdout, _, err := runArgs(t)
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	for _, want := range []string{
		"USAGE", "SOURCE (exactly one)", "--random", "--purity", "EXAMPLES",
		"not affiliated with wallhaven.cc",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help output is missing %q", want)
		}
	}
}

// The help text is the only place the purity bits are documented, so every
// combination has to be listed there.
func TestHelpListsEveryPurityCombination(t *testing.T) {
	stdout, _, err := runArgs(t, "--help")
	if err != nil {
		t.Fatalf("run() error = %v, want nil", err)
	}
	for _, bits := range []string{"100", "110", "111", "010", "011", "001", "101"} {
		if !strings.Contains(stdout, bits) {
			t.Errorf("help does not document purity %s", bits)
		}
	}
	if !strings.Contains(stdout, "WALLHAVEN_API_KEY") {
		t.Error("help does not mention WALLHAVEN_API_KEY")
	}
}

func TestHelpListsEverySource(t *testing.T) {
	stdout, _, _ := runArgs(t, "--help")
	for _, flag := range []string{
		"--random", "--latest", "--toplist", "--hotlist",
		"--id", "--tag", "--user", "--search", "--dl-only", "--dir",
	} {
		if !strings.Contains(stdout, flag) {
			t.Errorf("help does not document %s", flag)
		}
	}
	// Every source needs a worked example, not just a mention.
	for _, example := range []string{
		"wallhaven-cli --id g8d9dl",
		"wallhaven-cli --tag 2321",
		"wallhaven-cli --user helminuri",
		`wallhaven-cli --search "yosemite sunset"`,
		`--dir "~/Downloads/walls"`,
	} {
		if !strings.Contains(stdout, example) {
			t.Errorf("help is missing the example %q", example)
		}
	}
}

func TestVersion(t *testing.T) {
	stdout, _, err := runArgs(t, "--version")
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.HasPrefix(stdout, "wallhaven-cli ") {
		t.Errorf("version output = %q", stdout)
	}
}

func TestNoSourceIsUsageError(t *testing.T) {
	_, _, err := runArgs(t, "--purity", "110")
	if !isUsage(err) {
		t.Fatalf("run() error = %v, want a usage error", err)
	}
	if !strings.Contains(err.Error(), "--random") {
		t.Errorf("error should list the available sources, got: %v", err)
	}
}

func TestMultipleSourcesIsUsageError(t *testing.T) {
	tests := [][]string{
		{"--random", "--latest"},
		{"--toplist", "--hotlist"},
		{"--id", "g8d9dl", "--random"},
		{"--tag", "2321", "--search", "sunset"},
		{"--user", "helminuri", "--tag", "2321"},
		{"--random", "--latest", "--toplist"},
	}
	for _, args := range tests {
		_, _, err := runArgs(t, args...)
		if !isUsage(err) {
			t.Errorf("run(%v) error = %v, want a usage error", args, err)
			continue
		}
		if !strings.Contains(err.Error(), "pick one source") {
			t.Errorf("run(%v) error = %v, want it to say only one source is allowed", args, err)
		}
	}
}

func TestBadPurityIsUsageError(t *testing.T) {
	for _, bits := range []string{"000", "1001", "abc", "11", "222", "sfw"} {
		_, _, err := runArgs(t, "--random", "--purity", bits)
		if !isUsage(err) {
			t.Errorf("--purity %s error = %v, want a usage error", bits, err)
		}
	}
}

// Without a key wallhaven answers 200 and silently drops nsfw results, so
// the tool has to refuse before it makes the request.
func TestNSFWPurityWithoutKeyIsUsageError(t *testing.T) {
	t.Setenv("WALLHAVEN_API_KEY", "")
	for _, bits := range []string{"001", "011", "101", "111"} {
		_, _, err := runArgs(t, "--random", "--purity", bits)
		if !isUsage(err) {
			t.Fatalf("--purity %s error = %v, want a usage error", bits, err)
		}
		if !strings.Contains(err.Error(), "API key") {
			t.Errorf("--purity %s error = %v, want it to mention the API key", bits, err)
		}
	}
}

func TestSafePurityNeedsNoKey(t *testing.T) {
	t.Setenv("WALLHAVEN_API_KEY", "")
	for _, bits := range []string{"100", "110", "010"} {
		needsKey, err := wallhaven.ValidatePurity(bits)
		if err != nil || needsKey {
			t.Errorf("purity %s: needsKey = %v, err = %v; want false, nil", bits, needsKey, err)
		}
	}
}

func TestStraySeparateArgumentIsUsageError(t *testing.T) {
	_, _, err := runArgs(t, "--random", "sunset")
	if !isUsage(err) {
		t.Fatalf("run() error = %v, want a usage error", err)
	}
}

func TestSelectSource(t *testing.T) {
	s, err := selectSource(
		map[string]bool{"random": false, "latest": false, "toplist": true, "hotlist": false},
		map[string]string{"id": "", "tag": "", "user": "", "search": ""},
	)
	if err != nil {
		t.Fatalf("selectSource() error = %v", err)
	}
	if s.Flag != "toplist" || s.Arg != "" {
		t.Errorf("selectSource() = %+v, want {toplist }", s)
	}

	s, err = selectSource(
		map[string]bool{},
		map[string]string{"user": "helminuri"},
	)
	if err != nil {
		t.Fatalf("selectSource() error = %v", err)
	}
	if s.Flag != "user" || s.Arg != "helminuri" {
		t.Errorf("selectSource() = %+v, want {user helminuri}", s)
	}
}
