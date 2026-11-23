// Package sack answers btd's package queries from the same data dnf5 uses,
// without running dnf5: the enabled repositories from dnf5's own solv cache
// and the installed packages from the rpmdb.
//
// dnf5 spends most of a query starting up, loading plugins, checking
// every repository and building libsolv's pool before it looks at a single
// package. btd only reads the files, in parallel, and only decodes the
// attributes of the packages a query matches.
//
// The fast path is only taken when it sees what dnf5 would: every enabled
// repository has a valid, unexpired cache, and the configuration uses
// nothing btd does not model (excludes, modules, versionlock...). Otherwise
// Load returns an error and the caller runs dnf5, which also refreshes the
// cache for the next time.
package sack

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/bacteriafield/btd/pkg/rpmdb"
	"github.com/bacteriafield/btd/pkg/solv"
)

// Sack is the loaded package data.
type Sack struct {
	Repos []*Repo
	DB    *rpmdb.DB // the installed packages
	cfg   *config
}

// Repo is an enabled repository.
type Repo struct {
	ID       string
	Priority int // lower wins, like dnf's priority option
	file     *solv.File
}

// ErrStale means a repository's cache is missing, expired or out of date:
// dnf5 would download metadata first.
var ErrStale = errors.New("repository cache needs a refresh")

// Load reads the configuration, the rpmdb and the cache of every enabled
// repository. Any error means the caller should use dnf5 instead.
func Load() (*Sack, error) {
	db, err := rpmdb.Open(rpmdb.Path)
	if err != nil {
		return nil, err
	}
	s, err := load(db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func load(db *rpmdb.DB) (*Sack, error) {
	cfg, err := loadConfig(db)
	if err != nil {
		return nil, err
	}
	// Locked versions hide packages from dnf5's queries.
	if st, err := os.Stat("/etc/dnf/versionlock.toml"); err == nil && st.Size() > 0 {
		return nil, ErrUnsupported
	}

	s := &Sack{DB: db, cfg: cfg, Repos: make([]*Repo, len(cfg.repos))}
	errs := make([]error, len(cfg.repos))
	var wg sync.WaitGroup
	for i, rc := range cfg.repos {
		wg.Go(func() {
			s.Repos[i], errs[i] = cfg.loadRepo(rc)
		})
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return s, nil
}

// Close closes the rpmdb.
func (s *Sack) Close() error { return s.DB.Close() }

// loadRepo finds a usable cache of repository rc: the user's own first,
// then the system one, as dnf5 does (it clones a fresh system cache).
func (c *config) loadRepo(rc *repoConfig) (*Repo, error) {
	name, err := c.cacheDirName(rc)
	if err != nil {
		return nil, err
	}
	expire, err := c.metadataExpire(rc)
	if err != nil {
		return nil, err
	}
	checkAge, err := parseBool(c.main["check_config_file_age"], true)
	if err != nil {
		return nil, err
	}

	var errs []error
	for _, dir := range cacheDirs() {
		f, err := openCache(filepath.Join(dir, name), rc, expire, checkAge)
		if err == nil {
			return &Repo{ID: rc.id, Priority: rc.priority, file: f}, nil
		}
		errs = append(errs, err)
	}
	return nil, fmt.Errorf("%s: %w", rc.id, errors.Join(errs...))
}

// openCache checks a repository cache directory the way libdnf5 does
// (Repo::recompute_expired, SolvRepo::can_use_solvfile_cache) and parses
// its solv file.
func openCache(dir string, rc *repoConfig, expire int64, checkAge bool) (*solv.File, error) {
	repomd, err := os.ReadFile(filepath.Join(dir, "repodata", "repomd.xml"))
	if err != nil {
		return nil, ErrStale
	}
	// Modular repositories need module filtering, which btd does not do.
	if bytes.Contains(repomd, []byte(`type="modules"`)) {
		return nil, ErrUnsupported
	}

	// `dnf clean expire-cache` marks caches expired with this file.
	if _, err := os.Stat(filepath.Join(dir, "attrs", "expired")); err == nil {
		return nil, ErrStale
	}
	// The metadata age is the primary file's: dnf5 touches it whenever it
	// finds the cache still matches the repository.
	primary, _ := filepath.Glob(filepath.Join(dir, "repodata", "*primary.xml*"))
	if len(primary) != 1 {
		return nil, ErrStale
	}
	st, err := os.Stat(primary[0])
	if err != nil {
		return nil, ErrStale
	}
	if expire >= 0 && time.Since(st.ModTime()) >= time.Duration(expire)*time.Second {
		return nil, ErrStale
	}
	if repoFile, err := os.Stat(rc.file); checkAge && err == nil && repoFile.ModTime().After(st.ModTime()) {
		return nil, ErrStale
	}

	data, err := mmap(filepath.Join(dir, "solv", rc.id+".solv"))
	if err != nil {
		return nil, ErrStale
	}
	f, err := solv.Parse(data)
	if err != nil {
		return nil, err
	}
	// dnf5 stores in the solv file the checksum of the repomd.xml it was made
	// from: "\0dnf", "\0001.0", the libsolv version (8 bytes), then
	// SHA-256("H000" + repomd.xml).
	sum := sha256.Sum256(append([]byte("H000"), repomd...))
	u := f.Userdata
	if len(u) != 48 || string(u[:8]) != "\x00dnf\x001.0" || !bytes.Equal(u[16:], sum[:]) {
		return nil, ErrStale
	}
	return f, nil
}

// mmap maps a file read-only. dnf5 replaces cache files by renaming new
// ones over them, so the mapping never changes under btd.
// ponytail: never unmapped, the process is short-lived and strings point
// into it.
func mmap(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() == 0 {
		return nil, errors.New("empty file")
	}
	return syscall.Mmap(int(f.Fd()), 0, int(st.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
}

// Package is a package of an enabled repository.
type Package struct {
	Repo *Repo
	s    *solv.Solvable
}

func (p Package) str(id solv.Id) string    { return p.Repo.file.Str(id) }
func (p Package) lookup(key string) string { return p.Repo.file.LookupStr(p.s, key) }

// Name, EVR ("[epoch:]version-release", no epoch when 0), Arch, Vendor and
// Summary are the package's attributes.
func (p Package) Name() string    { return p.str(p.s.Name) }
func (p Package) EVR() string     { return p.str(p.s.EVR) }
func (p Package) Arch() string    { return p.str(p.s.Arch) }
func (p Package) Vendor() string  { return p.str(p.s.Vendor) }
func (p Package) Summary() string { return p.lookup("solvable:summary") }

// isSource reports source packages, which dnf5 queries leave out unless
// asked for (they only come from source repositories anyway).
func (p Package) isSource() bool {
	a := p.Arch()
	return a == "src" || a == "nosrc"
}

// packages calls fn with every package of every enabled repository.
func (s *Sack) packages(fn func(Package)) {
	for _, r := range s.Repos {
		for i := range r.file.Solvables {
			fn(Package{Repo: r, s: &r.file.Solvables[i]})
		}
	}
}

// Search returns the packages whose name contains one of terms (a glob when
// it has glob characters), in their latest version, one per name, sorted by
// name: what `dnf5 repoquery --latest-limit=1 '*term*'` lists, minus
// architecture duplicates.
func (s *Sack) Search(terms []string) []Package {
	match := func(name string) bool {
		for _, t := range terms {
			if strings.ContainsAny(t, "*?[") {
				if ok, _ := path.Match("*"+t+"*", name); ok {
					return true
				}
			} else if strings.Contains(name, t) {
				return true
			}
		}
		return false
	}

	best := map[string]Package{}
	s.packages(func(p Package) {
		name := p.Name()
		if p.isSource() || !match(name) {
			return
		}
		if cur, ok := best[name]; !ok || better(p, cur, s.cfg.vars["basearch"]) {
			best[name] = p
		}
	})

	out := make([]Package, 0, len(best))
	for _, p := range best {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Package) int { return strings.Compare(a.Name(), b.Name()) })
	return out
}

// better reports whether p beats cur as the one entry shown for a name:
// the higher version, then the system's architecture over noarch over
// others (multilib), then the higher priority repository.
func better(p, cur Package, basearch string) bool {
	if c := EVRCmp(p.EVR(), cur.EVR()); c != 0 {
		return c > 0
	}
	rank := func(arch string) int {
		switch arch {
		case basearch:
			return 0
		case "noarch":
			return 1
		}
		return 2
	}
	if a, b := rank(p.Arch()), rank(cur.Arch()); a != b {
		return a < b
	}
	return p.Repo.Priority < cur.Repo.Priority
}

// Provided reports whether an enabled repository has a package providing
// spec, a name or capability such as pkgconfig(gtk4), like
// `dnf5 repoquery --whatprovides spec`. Specs with versions or globs are
// left to dnf5 (ErrUnsupported).
func (s *Sack) Provided(spec string) (bool, error) {
	if spec == "" || strings.ContainsAny(spec, "*?[] <>=") {
		return false, ErrUnsupported
	}
	// Every package provides its own name: check names first, it is cheap.
	found := false
	s.packages(func(p Package) {
		found = found || !p.isSource() && p.Name() == spec
	})
	if found {
		return true, nil
	}
	for _, r := range s.Repos {
		f := r.file
		for i := range f.Solvables {
			sv := &f.Solvables[i]
			if a := f.Str(sv.Arch); a == "src" || a == "nosrc" {
				continue
			}
			for _, dep := range f.LookupDeps(sv, "solvable:provides") {
				if f.DepName(dep) == spec {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
