package main

import (
	"slices"
	"testing"
)

func TestFormatSize(t *testing.T) {
	// Values from dnf5 info output.
	for n, want := range map[uint64]string{
		8655897:  "8.3 MiB",
		34748757: "33.1 MiB",
		512:      "512.0   B",
		1000:     "1.0 KiB",
	} {
		if got := formatSize(n); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestSafeEncode(t *testing.T) {
	for _, tt := range []struct {
		in        string
		utf8, out string
	}{
		{"Bokmål", "Bokmål", `Bokm\xc3\xa5l`},
		{"a\tb", `a\x09b`, `a\x09b`},
		{`C:\x`, `C:\x5cx`, `C:\x5cx`},
		{"bad\xffbyte", `bad\xffbyte`, `bad\xffbyte`},
	} {
		if got := safeEncode(tt.in, true); got != tt.utf8 {
			t.Errorf("safeEncode(%q, utf8) = %q, want %q", tt.in, got, tt.utf8)
		}
		if got := safeEncode(tt.in, false); got != tt.out {
			t.Errorf("safeEncode(%q, ascii) = %q, want %q", tt.in, got, tt.out)
		}
	}
}

func TestChunk(t *testing.T) {
	if got := chunk("Vim-fork focused on extensibility and agility", 33); !slices.Equal(got,
		[]string{"Vim-fork focused on extensibility", " and agility"}) {
		t.Errorf("chunk = %q", got)
	}
}
