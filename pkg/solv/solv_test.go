package solv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecompress(t *testing.T) {
	// "abc" as literals (0LLLLLLL), then a reference 3 bytes back, length 6
	// (1110llll <8o> <8o>: l = 6-3, o = 3-1): "abcabcabc".
	in := []byte{'a', 'b', 'c', 0xe3, 2, 0}
	out := make([]byte, pageSize)
	n, err := decompress(in, out)
	if err != nil || string(out[:n]) != "abcabcabc" {
		t.Fatalf("decompress = %q, %v", out[:n], err)
	}
	// A reference before the start of the output is corrupt, not a crash.
	if _, err := decompress([]byte{0xe3, 9, 0}, out); err == nil {
		t.Error("reference out of bounds: want error")
	}
}

// solvFiles returns dnf5's cached solv files; tests using them skip
// outside Fedora.
func solvFiles(tb testing.TB) []string {
	files, _ := filepath.Glob("/var/cache/libdnf5/*/solv/*.solv")
	if len(files) == 0 {
		tb.Skip("no dnf5 solv cache")
	}
	return files
}

// TestParseCache parses dnf5's own cache: every package must have a name,
// a version and an architecture.
func TestParseCache(t *testing.T) {
	for _, file := range solvFiles(t) {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for i := range f.Solvables {
			s := &f.Solvables[i]
			if f.Str(s.Name) == "" || f.Str(s.EVR) == "" || f.Str(s.Arch) == "" {
				t.Fatalf("%s: package %d has no name, evr or arch", file, i)
			}
		}
		t.Logf("%s: %d packages", file, len(f.Solvables))
	}
}

func BenchmarkParse(b *testing.B) {
	files := solvFiles(b)
	var data [][]byte
	for _, file := range files {
		d, err := os.ReadFile(file)
		if err != nil {
			b.Fatal(err)
		}
		data = append(data, d)
	}
	for b.Loop() {
		for _, d := range data {
			if _, err := Parse(d); err != nil {
				b.Fatal(err)
			}
		}
	}
}
