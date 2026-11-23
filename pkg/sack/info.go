package sack

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/bacteriafield/btd/pkg/rpmdb"
)

// This file is `dnf5 info <spec>...` (dnf5/commands/list/list.cpp): the
// installed packages matching the specs, and the available ones that are
// not installed in that version or newer.

// More package attributes, for info.

// Epoch, Version and Release split the EVR; the epoch defaults to "0".
func (p Package) Epoch() string   { e, _, _ := SplitEVR(p.EVR()); return e }
func (p Package) Version() string { _, v, _ := SplitEVR(p.EVR()); return v }
func (p Package) Release() string { _, _, r := SplitEVR(p.EVR()); return r }

func (p Package) URL() string         { return p.lookup("solvable:url") }
func (p Package) License() string     { return p.lookup("solvable:license") }
func (p Package) Description() string { return p.lookup("solvable:description") }

// DownloadSize and InstallSize are in bytes.
func (p Package) DownloadSize() uint64 { return p.Repo.file.LookupNum(p.s, "solvable:downloadsize") }
func (p Package) InstallSize() uint64  { return p.Repo.file.LookupNum(p.s, "solvable:installsize") }

// SourceRPM is the source package's file name, as libsolv's
// solvable_lookup_sourcepkg builds it: a void sourcename or sourceevr means
// "same as the package's".
func (p Package) SourceRPM() string {
	f := p.Repo.file
	name := p.lookup("solvable:sourcename")
	if f.LookupVoid(p.s, "solvable:sourcename") {
		name = p.Name()
	}
	if name == "" {
		return ""
	}
	arch := p.lookup("solvable:sourcearch")
	evr := p.lookup("solvable:sourceevr")
	if f.LookupVoid(p.s, "solvable:sourceevr") {
		_, v, r := SplitEVR(p.EVR()) // without the epoch
		evr = v + "-" + r
	}
	if arch != "src" && arch != "nosrc" {
		return name
	}
	if evr != "" {
		name += "-" + evr
	}
	return name + "." + arch + ".rpm"
}

// Installed is an installed package.
type Installed struct {
	*rpmdb.Package
	EVR      string // "[epoch:]version-release", no epoch when 0, like Package.EVR
	FromRepo string // the repository it was installed from, per dnf5's system state
}

// InfoResult is what `dnf5 info` shows for some specs.
type InfoResult struct {
	Installed []Installed
	Available []Package
	Unmatched []string // specs that matched no package, sorted
}

// Info resolves package specs like `dnf5 info`: package names, possibly
// globs with * and ?, matched case-insensitively against installed and
// available packages. Other spec forms (name-version, name.arch, file
// paths...) are left to dnf5 (ErrUnsupported).
func (s *Sack) Info(specs []string) (*InfoResult, error) {
	matchers := make([]func(string) bool, len(specs))
	for i, spec := range specs {
		// dnf5 tries name.arch and [epoch:]name-version-release.arch forms
		// before the plain name; those need a dot or a colon.
		if spec == "" || strings.ContainsAny(spec, ".:/[]\\ \t") {
			return nil, ErrUnsupported
		}
		if strings.ContainsAny(spec, "*?") {
			pattern := strings.ToLower(spec)
			matchers[i] = func(name string) bool {
				ok, _ := path.Match(pattern, strings.ToLower(name))
				return ok
			}
		} else {
			matchers[i] = func(name string) bool { return strings.EqualFold(spec, name) }
		}
	}
	matched := make([]bool, len(specs))
	matchAny := func(name string) bool {
		found := false
		for i, m := range matchers {
			if m(name) {
				matched[i], found = true, true
			}
		}
		return found
	}

	res := &InfoResult{}
	hnums, err := s.db.Find("Name", matchAny)
	if err != nil {
		return nil, err
	}
	for _, h := range hnums {
		p, err := s.db.Package(h)
		if err != nil {
			return nil, err
		}
		if p.Name == "gpg-pubkey" { // libsolv leaves keys out of the installed packages
			continue
		}
		res.Installed = append(res.Installed, Installed{Package: p, EVR: installedEVR(p)})
	}

	var candidates []Package
	for p := range s.packages() {
		if matchAny(p.Name()) {
			candidates = append(candidates, p)
		}
	}

	for i, spec := range specs {
		if matched[i] {
			continue
		}
		// When the name matches nothing, dnf5 goes on to name-version forms
		// and globs over whole NEVRAs.
		if strings.ContainsAny(spec, "-*?") {
			return nil, ErrUnsupported
		}
		res.Unmatched = append(res.Unmatched, spec)
	}
	slices.Sort(res.Unmatched)
	res.Unmatched = slices.Compact(res.Unmatched)

	if len(res.Installed) > 0 {
		if err := s.fromRepos(res.Installed); err != nil {
			return nil, err
		}
	}
	res.Available = latestNotInstalled(candidates, res.Installed)

	slices.SortFunc(res.Installed, func(a, b Installed) int { return cmpNEVRA(a.Name, a.EVR, a.Arch, b.Name, b.EVR, b.Arch) })
	slices.SortFunc(res.Available, func(a, b Package) int {
		return cmpNEVRA(a.Name(), a.EVR(), a.Arch(), b.Name(), b.EVR(), b.Arch())
	})
	return res, nil
}

// latestNotInstalled is list.cpp's filtering of the available packages:
// keep the highest priority repositories (filter_priority), then the
// latest version (filter_latest_evr), per name and arch; then drop those
// not newer than the installed package of the same name and arch.
func latestNotInstalled(candidates []Package, installed []Installed) []Package {
	type na struct{ name, arch string }
	bestPrio := map[na]int{}
	for _, p := range candidates {
		k := na{p.Name(), p.Arch()}
		if prio, ok := bestPrio[k]; !ok || p.Repo.Priority < prio {
			bestPrio[k] = p.Repo.Priority
		}
	}
	latest := map[na]string{}
	var kept []Package
	for _, p := range candidates {
		k := na{p.Name(), p.Arch()}
		if p.Repo.Priority != bestPrio[k] {
			continue
		}
		kept = append(kept, p)
		if evr, ok := latest[k]; !ok || EVRCmp(p.EVR(), evr) > 0 {
			latest[k] = p.EVR()
		}
	}
	installedLatest := map[na]string{}
	for _, p := range installed {
		k := na{p.Name, p.Arch}
		if evr, ok := installedLatest[k]; !ok || EVRCmp(p.EVR, evr) > 0 {
			installedLatest[k] = p.EVR
		}
	}

	var out []Package
	for _, p := range kept {
		k := na{p.Name(), p.Arch()}
		if EVRCmp(p.EVR(), latest[k]) != 0 {
			continue
		}
		if evr, ok := installedLatest[k]; ok && EVRCmp(p.EVR(), evr) <= 0 {
			continue
		}
		out = append(out, p)
	}
	return out
}

// cmpNEVRA orders packages like libdnf5's cmp_nevra: name, EVR, arch.
func cmpNEVRA(an, aevr, aarch, bn, bevr, barch string) int {
	return cmp.Or(strings.Compare(an, bn), EVRCmp(aevr, bevr), strings.Compare(aarch, barch))
}

// installedEVR builds an installed package's EVR like libsolv's headtoevr.
func installedEVR(p *rpmdb.Package) string {
	evr := p.Version + "-" + p.Release
	if p.Epoch != 0 {
		evr = fmt.Sprintf("%d:%s", p.Epoch, evr)
	}
	return evr
}

// NEVRA is how dnf5's system state names a package: name-[epoch:]version-release.arch.
func (p Installed) NEVRA() string { return p.Name + "-" + p.EVR + "." + p.Arch }

// fromRepos fills FromRepo from dnf5's system state, nevras.toml, whose
// lines look like: "bash-5.3.15-2.fc45.x86_64" = {from_repo = "updates"}.
// A package missing there shows as "<unknown>" in dnf5.
func (s *Sack) fromRepos(pkgs []Installed) error {
	repos, err := readState("nevras.toml", "from_repo")
	if err != nil {
		return err
	}
	for i := range pkgs {
		pkgs[i].FromRepo = "<unknown>"
		if repo, ok := repos[pkgs[i].NEVRA()]; ok {
			pkgs[i].FromRepo = repo
		}
	}
	return nil
}

// parseStateLine reads a line of dnf5's state files:
// "key" = {field = "value"}. Anything else returns false.
func parseStateLine(line, field string) (key, value string, ok bool) {
	rest, ok := strings.CutPrefix(line, `"`)
	if !ok {
		return "", "", false
	}
	key, rest, ok = strings.Cut(rest, `"`)
	if !ok {
		return "", "", false
	}
	_, rest, ok = strings.Cut(rest, "{"+field+` = "`)
	if !ok {
		return "", "", false
	}
	value, _, ok = strings.Cut(rest, `"`)
	return key, value, ok
}
