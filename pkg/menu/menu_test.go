package menu

import (
	"slices"
	"strings"
	"testing"
)

func TestParseSelection(t *testing.T) {
	for input, want := range map[string][]int{
		"":          nil,
		"1":         {0},
		"1 3 2-4":   {0, 2, 1, 3},
		"5,1  1\t2": {4, 0, 1},
	} {
		got, err := ParseSelection(input, 5)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("ParseSelection(%q) = %v, %v; want %v", input, got, err, want)
		}
	}

	for _, input := range []string{"0", "6", "4-2", "a", "1-", "-1"} {
		if _, err := ParseSelection(input, 5); err == nil {
			t.Errorf("ParseSelection(%q): want error", input)
		}
	}
}

func TestPrint(t *testing.T) {
	items := []Item{
		{Source: "fedora", Name: "neovim", Version: "0.11-1", Description: "Vim-fork"},
		{Source: "copr:owner/nvim", Description: "nightly"},
	}

	var b strings.Builder
	Print(&b, items, true, true)
	if want := "2 [copr:owner/nvim] - nightly\n1 [fedora] neovim 0.11-1 - Vim-fork\n"; b.String() != want {
		t.Errorf("bottom-up single-line:\n%s\nwant:\n%s", b.String(), want)
	}

	b.Reset()
	Print(&b, items[:1], false, false)
	if want := "1 [fedora] neovim 0.11-1\n    Vim-fork\n"; b.String() != want {
		t.Errorf("two-line:\n%s\nwant:\n%s", b.String(), want)
	}
}
