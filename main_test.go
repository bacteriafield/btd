package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/bacteriafield/btd/pkg/settings"
)

func TestParseFlags(t *testing.T) {
	cfg := settings.DefaultConfig("test")
	rest, err := parseFlags([]string{
		"-i", "obs-studio", "-y", "--sudo-bin", "doas", "--dnf-bin=/bin/dnf5",
		"--no-bottom-up", "--debug", "--save", "--refresh", "--", "--debug",
	}, cfg)
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"-i", "obs-studio", "--refresh", "--debug"}; !slices.Equal(rest, want) {
		t.Errorf("rest = %q, want %q", rest, want)
	}
	if cfg.SudoBin != "doas" || cfg.DnfBin != "/bin/dnf5" || cfg.BottomUp || !cfg.Debug || !cfg.SaveConfig || !settings.NoConfirm {
		t.Errorf("flags not applied: %+v, NoConfirm=%v", cfg, settings.NoConfirm)
	}

	if _, err := parseFlags([]string{"--sudo-bin"}, cfg); err == nil {
		t.Error("missing flag value: want error")
	}
}

func TestNeedsRoot(t *testing.T) {
	for args, want := range map[string]bool{
		"list --installed":    false,
		"--refresh repoquery": false,
		"copr list":           false,
		"history info 3":      false,
		"copr enable a/b":     true,
		"history undo 3":      true,
		"autoremove":          true,
		"--refresh":           true,
	} {
		if got := needsRoot(strings.Fields(args)); got != want {
			t.Errorf("needsRoot(%q) = %v, want %v", args, got, want)
		}
	}
}
