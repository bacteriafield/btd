package benchmark

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

// btd answers info, search and no-op installs from dnf5's cache instead of
// running dnf5. These tests run both on the same queries: the answers must
// match, and btd must not have run dnf5 to get its own.

// infoSpecs cover names, globs, case-insensitive names, unmatched specs,
// installed and available packages, and non-ASCII descriptions (glibc's
// Norwegian Bokmål langpack).
var infoSpecs = [][]string{
	{"neovim"},
	{"bash"},
	{"neovim", "bash"},
	{"python3-neo*"},
	{"BASH"},
	{"nosuchpackage"},
	{"neovim", "nosuchpackage"},
	{"glibc-langpack-n*"},
}

// TestInfoMatchesDNF5 compares `btd info` with `dnf5 info`: stdout, exit
// code and messages must be the same.
func TestInfoMatchesDNF5(t *testing.T) {
	btd := build(t)
	for _, specs := range infoSpecs {
		t.Run(strings.Join(specs, "+"), func(t *testing.T) {
			args := append([]string{"info"}, specs...)
			native(t, btd, args...)
			compare(t, run(t, btd, args...), run(t, "dnf5", args...))
		})
	}
}

// TestSearchMatchesRepoquery checks that btd -s lists one entry per package
// name that `dnf5 repoquery '*term*'` returns.
func TestSearchMatchesRepoquery(t *testing.T) {
	btd := build(t)
	native(t, btd, "-s", term, noCopr)

	// With these flags each result is one line: "1 [repo] name evr - summary".
	var got []string
	for line := range strings.Lines(run(t, btd, "-s", term, noCopr, "--no-bottom-up", "--single-line-results").stdout) {
		if f := strings.Fields(line); len(f) >= 3 {
			got = append(got, f[2])
		}
	}
	slices.Sort(got)

	want := strings.Fields(run(t, "dnf5", "-q", "repoquery", "--qf", "%{name}\n", "*"+term+"*").stdout)
	slices.Sort(want)
	want = slices.Compact(want)

	if !slices.Equal(got, want) {
		t.Errorf("btd -s %s found %q\ndnf5 repoquery found %q", term, got, want)
	}
}

// TestInstallNothingToDo compares installing packages that are already
// installed, which btd answers without dnf5.
func TestInstallNothingToDo(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("dnf5 install needs root")
	}
	btd := build(t)
	for _, targets := range [][]string{{"bash"}, {"bash", "coreutils"}} {
		t.Run(strings.Join(targets, "+"), func(t *testing.T) {
			native(t, btd, append([]string{"-i"}, targets...)...)
			compare(t, run(t, btd, append([]string{"-i"}, targets...)...),
				run(t, "dnf5", append([]string{"install"}, targets...)...))
		})
	}
}

// output is what a command printed and returned.
type output struct {
	stdout, stderr string
	code           int
}

// noise is stderr that differs on purpose: dnf5's progress while loading
// repositories, and btd's warning when run as root.
var noise = []string{
	"Updating and loading repositories:",
	"Repositories loaded.",
	"warning: avoid running btd as root",
}

// run runs name with args and returns its output, minus the noise.
func run(t *testing.T, name string, args ...string) output {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var o output
	if err := cmd.Run(); err != nil {
		exitErr, ok := errors.AsType[*exec.ExitError](err)
		if !ok {
			t.Fatalf("%s: %v", name, err)
		}
		o.code = exitErr.ExitCode()
	}
	o.stdout = stdout.String()
	for line := range strings.Lines(stderr.String()) {
		if !slices.Contains(noise, strings.TrimSuffix(line, "\n")) {
			o.stderr += line
		}
	}
	return o
}

// compare fails the test if btd's output differs from dnf5's, showing the
// first line that does.
func compare(t *testing.T, btd, dnf5 output) {
	t.Helper()
	if btd.code != dnf5.code {
		t.Errorf("exit code: btd %d, dnf5 %d", btd.code, dnf5.code)
	}
	for _, s := range []struct{ name, got, want string }{
		{"stdout", btd.stdout, dnf5.stdout},
		{"stderr", btd.stderr, dnf5.stderr},
	} {
		if s.got == s.want {
			continue
		}
		got, want := strings.Split(s.got, "\n"), strings.Split(s.want, "\n")
		for i := range max(len(got), len(want)) {
			g, w := "", ""
			if i < len(got) {
				g = got[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if g != w {
				t.Errorf("%s line %d:\nbtd:  %q\ndnf5: %q", s.name, i+1, g, w)
				break
			}
		}
	}
}

// native fails the test if btd ran dnf5 instead of answering from its
// cache: with --debug, btd logs each command it runs and why it skipped the
// cache. Copr requests (GET) are expected.
func native(t *testing.T, btd string, args ...string) {
	t.Helper()
	for line := range strings.Lines(run(t, btd, append([]string{"--debug"}, args...)...).stderr) {
		if strings.HasPrefix(line, "debug: ") && !strings.HasPrefix(line, "debug: GET ") {
			t.Fatalf("btd %s ran dnf5 instead of reading its cache: %s", strings.Join(args, " "), line)
		}
	}
}
