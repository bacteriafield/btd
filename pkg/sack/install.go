package sack

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// AlreadyInstalled checks whether `dnf5 install names...` would change
// nothing, and returns the installed packages dnf5 then reports as
// "already installed". That is the case (libdnf5's add_install_to_goal)
// when:
//
//   - every name is an installed package;
//   - best is off, as Fedora ships it: the solver keeps an installed
//     version even when a newer one is available (with best on, it
//     upgrades);
//   - dnf5's system state records why each package is installed; for a
//     package installed with plain rpm it does not, and dnf5 install then
//     adopts it as user-installed, which is a change.
//
// Anything else returns ErrUnsupported: dnf5 has to resolve it.
func (s *Sack) AlreadyInstalled(names []string) ([]Installed, error) {
	if best, err := parseBool(s.cfg.main["best"], true); err != nil || best {
		return nil, ErrUnsupported
	}
	reasons, err := readState("packages.toml", "reason")
	if err != nil {
		return nil, err
	}

	var out []Installed
	for _, name := range names {
		// Only plain names: dnf5 reads specs with a dot or colon as
		// name.arch or a NEVRA first, and handles globs, paths and rich
		// dependencies differently.
		if name == "" || strings.ContainsAny(name, ".:/*?[]()<>=, \t") {
			return nil, ErrUnsupported
		}
		hnums, err := s.DB.Find("Name", func(key string) bool { return key == name })
		if err != nil {
			return nil, err
		}
		if len(hnums) == 0 { // not installed: a real install
			return nil, ErrUnsupported
		}
		for _, h := range hnums {
			p, err := s.DB.Package(h)
			if err != nil {
				return nil, err
			}
			if reasons[p.Name+"."+p.Arch] == "" {
				return nil, ErrUnsupported
			}
			out = append(out, Installed{Package: p, EVR: installedEVR(p)})
		}
	}
	return out, nil
}

// readState reads field from every line of one of dnf5's system state files
// (stateDir), keyed by the line's key.
func readState(file, field string) (map[string]string, error) {
	f, err := os.Open(filepath.Join(stateDir, file))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := parseStateLine(sc.Text(), field); ok {
			m[k] = v
		}
	}
	return m, sc.Err()
}
