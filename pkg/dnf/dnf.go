// Package dnf runs dnf5: queries as the user, transactions as root.
// Queries are answered from dnf5's cache directly when possible (see
// package sack), which is several times faster than running dnf5.
package dnf

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"strings"
	"sync"

	"github.com/bacteriafield/btd/pkg/sack"
	"github.com/bacteriafield/btd/pkg/settings"
)

var (
	sackOnce   sync.Once
	nativeSack *sack.Sack
)

// Native returns dnf5's data read directly, or nil when dnf5 has to be run
// instead: stale cache, unsupported configuration... It is loaded once.
func Native(cfg *settings.Configuration) *sack.Sack {
	sackOnce.Do(func() {
		s, err := sack.Load()
		if err != nil {
			cfg.Debugf("not reading dnf5's cache directly: %v", err)
			return
		}
		nativeSack = s
	})
	return nativeSack
}

// Package is a package available in the enabled repositories.
type Package struct {
	Name, EVR, Repo, Summary string
}

// queryFormat must match parsePackages; dnf5 adds no newline on its own.
const queryFormat = "%{name}\t%{evr}\t%{repoid}\t%{summary}\n"

// Search lists the latest available packages whose name contains any term.
func Search(cfg *settings.Configuration, terms []string) ([]Package, error) {
	if s := Native(cfg); s != nil {
		var pkgs []Package
		for _, p := range s.Search(terms) {
			pkgs = append(pkgs, Package{Name: p.Name(), EVR: p.EVR(), Repo: p.Repo.ID, Summary: p.Summary()})
		}
		return pkgs, nil
	}

	args := []string{"-q", "repoquery", "--latest-limit=1", "--qf", queryFormat}
	for _, term := range terms {
		args = append(args, "*"+term+"*")
	}
	out, err := query(cfg, args...)
	return parsePackages(out), err
}

// Provided reports whether an enabled repository provides spec, which covers
// package names and capabilities such as pkgconfig(gtk4).
func Provided(cfg *settings.Configuration, spec string) (bool, error) {
	if s := Native(cfg); s != nil {
		if ok, err := s.Provided(spec); !errors.Is(err, sack.ErrUnsupported) {
			return ok, err
		}
	}
	out, err := query(cfg, "-q", "repoquery", "--whatprovides", spec, "--qf", "%{name}\n")
	return strings.TrimSpace(out) != "", err
}

func query(cfg *settings.Configuration, args ...string) (string, error) {
	cfg.Debugf("%s %s", cfg.DnfBin, strings.Join(args, " "))
	out, err := exec.Command(cfg.DnfBin, args...).Output()
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		err = fmt.Errorf("%s: %s", cfg.DnfBin, strings.TrimSpace(string(exitErr.Stderr)))
	}
	return string(out), err
}

// parsePackages reads queryFormat lines, keeping one entry per name so
// multilib (i686) duplicates collapse.
func parsePackages(out string) []Package {
	var pkgs []Package
	seen := map[string]bool{}
	for line := range strings.Lines(out) {
		f := strings.SplitN(strings.TrimSuffix(line, "\n"), "\t", 4)
		if len(f) != 4 || seen[f[0]] {
			continue
		}
		seen[f[0]] = true
		pkgs = append(pkgs, Package{Name: f[0], EVR: f[1], Repo: f[2], Summary: f[3]})
	}
	return pkgs
}

// Run runs dnf attached to the terminal, through the privilege elevator
// when root is set and btd is not root already.
func Run(cfg *settings.Configuration, root bool, args ...string) error {
	if settings.NoConfirm {
		args = append(slices.Clip(args), "-y")
	}

	name, argv := cfg.DnfBin, args
	if root && os.Geteuid() != 0 {
		if err := cfg.SetPrivilege(); err != nil {
			return err
		}
		name = cfg.SudoBin
		argv = append(append(strings.Fields(cfg.SudoFlags), cfg.DnfBin), args...)
	}
	cfg.Debugf("%s %s", name, strings.Join(argv, " "))

	// No exec.CommandContext: killing dnf mid-transaction can break the rpmdb.
	// dnf gets the terminal's Ctrl+C itself; btd just waits for it to exit.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	defer signal.Stop(sig)

	cmd := exec.Command(name, argv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
