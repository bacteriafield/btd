package dnf

import (
	"slices"
	"testing"
)

func TestParsePackages(t *testing.T) {
	out := "glibc-devel\t2.43-4.fc44\tupdates\tObject files for development using standard C libraries\n" +
		"glibc-devel\t2.43-4.fc44\tupdates\tObject files for development using standard C libraries\n" +
		"broken line\n" +
		"vim-enhanced\t2:9.2.1202-1.fc44\tupdates\tA version of the VIM editor\twith a tab\n"

	want := []Package{
		{"glibc-devel", "2.43-4.fc44", "updates", "Object files for development using standard C libraries"},
		{"vim-enhanced", "2:9.2.1202-1.fc44", "updates", "A version of the VIM editor\twith a tab"},
	}
	if got := parsePackages(out); !slices.Equal(got, want) {
		t.Errorf("parsePackages() = %q, want %q", got, want)
	}
}
