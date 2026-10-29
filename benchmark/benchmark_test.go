// Package benchmark times btd against dnf5 doing the same job. It needs
// Fedora with dnf5 and network access; elsewhere the benchmarks skip.
//
//	make bench          # or: go test -run '^$' -bench . ./benchmark
//	make docker-bench   # the same in a Fedora container, as root
//
// -short skips the Copr search, which takes 10-17s on Copr's side.
package benchmark

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// term is in the Fedora repositories and has Copr projects too.
const term = "neovim"

// noCopr points btd's Copr search at a closed port, so it fails at once
// and btd -s searches the enabled repositories only.
const noCopr = "--copr-url=http://127.0.0.1:1"

// BenchmarkSearch: dnf5 matches names and summaries; btd matches names,
// then adds the Copr projects.
func BenchmarkSearch(b *testing.B) {
	btd := build(b)
	b.Run("dnf5", func(b *testing.B) { bench(b, "dnf5", "search", term) })
	b.Run("btd", func(b *testing.B) { bench(b, btd, "-s", term, noCopr) })
	b.Run("btd+copr", func(b *testing.B) {
		if testing.Short() {
			b.Skip("Copr search skipped in -short mode")
		}
		bench(b, btd, "-s", term)
	})
}

// BenchmarkPassthrough is btd's own overhead: it hands info to dnf5 as is.
func BenchmarkPassthrough(b *testing.B) {
	btd := build(b)
	b.Run("dnf5", func(b *testing.B) { bench(b, "dnf5", "info", term) })
	b.Run("btd", func(b *testing.B) { bench(b, btd, "info", term) })
}

// BenchmarkInstall installs bash, which is always installed: dnf5 resolves
// it to nothing to do, while btd first asks the repositories whether they
// provide it. (--assumeno would exit 1 and fail the benchmark.)
func BenchmarkInstall(b *testing.B) {
	if os.Geteuid() != 0 {
		b.Skip("dnf5 install needs root")
	}
	btd := build(b)
	b.Run("dnf5", func(b *testing.B) { bench(b, "dnf5", "install", "bash") })
	b.Run("btd", func(b *testing.B) { bench(b, btd, "-i", "bash") })
}

// build compiles btd from this checkout, so the benchmark times the code
// next to it rather than the installed btd.
func build(b *testing.B) string {
	if _, err := exec.LookPath("dnf5"); err != nil {
		b.Skip("dnf5 not found: btd runs on Fedora only")
	}
	bin := b.TempDir() + "/btd"
	if out, err := exec.CommandContext(b.Context(), "go", "build", "-o", bin, "..").CombinedOutput(); err != nil {
		b.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

// bench times name with args. One untimed run first warms dnf's metadata
// cache and fails the benchmark if the command fails, instead of timing it.
func bench(b *testing.B, name string, args ...string) {
	if out, err := exec.CommandContext(b.Context(), name, args...).CombinedOutput(); err != nil {
		b.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	for b.Loop() {
		if err := exec.CommandContext(b.Context(), name, args...).Run(); err != nil {
			b.Fatal(err)
		}
	}
}
