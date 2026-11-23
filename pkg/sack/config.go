package sack

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/bacteriafield/btd/pkg/rpmdb"
)

// This file reads dnf5's configuration the way libdnf5 does, as far as btd
// needs it: main options, variables, and the enabled repositories with the
// directory where dnf5 caches each one. Paths and defaults come from
// libdnf5 (include/libdnf5/conf/const.hpp, conf/config_main.cpp).

// ErrUnsupported means the configuration uses something btd does not model,
// so its answer could differ from dnf5's: the caller should run dnf5.
var ErrUnsupported = errors.New("dnf5 configuration not supported by btd's fast path")

const (
	confFile       = "/etc/dnf/dnf.conf"
	systemCacheDir = "/var/cache/libdnf5"
	stateDir       = "/usr/lib/sysimage/libdnf5" // packages.toml, nevras.toml
)

var (
	confDirs         = []string{"/etc/dnf/libdnf5.conf.d", "/usr/share/dnf5/libdnf.conf.d"}
	defaultReposDirs = []string{"/etc/yum.repos.d", "/etc/distro.repos.d", "/usr/share/dnf5/repos.d"}
	repoOverrideDirs = []string{"/etc/dnf/repos.override.d", "/usr/share/dnf5/repos.override.d"}
	defaultVarsDirs  = []string{"/usr/share/dnf5/vars.d", "/etc/dnf/vars"} // later ones win
)

// options are the key=value pairs of a configuration section.
type options map[string]string

// section is one [name] of an ini file, in file order.
type section struct {
	name string
	opts options
}

// readINI parses a dnf configuration file: [section] headers, key=value
// lines, # and ; comments, and indented continuation lines, which extend
// the previous value (baseurl lists use them).
func readINI(file string) ([]section, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var secs []section
	var cur options
	last := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "" || trimmed[0] == '#' || trimmed[0] == ';':
		case trimmed[0] == '[':
			name, ok := strings.CutSuffix(trimmed[1:], "]")
			if !ok {
				return nil, fmt.Errorf("%s: bad section %q", file, trimmed)
			}
			cur = options{}
			secs = append(secs, section{name: strings.TrimSpace(name), opts: cur})
			last = ""
		case line[0] == ' ' || line[0] == '\t':
			if cur != nil && last != "" {
				cur[last] += "\n" + trimmed
			}
		default:
			k, v, ok := strings.Cut(trimmed, "=")
			if !ok || cur == nil {
				return nil, fmt.Errorf("%s: bad line %q", file, trimmed)
			}
			last = strings.TrimSpace(k)
			cur[last] = strings.TrimSpace(v)
		}
	}
	return secs, sc.Err()
}

// dropIns lists the files with ext in dirs, sorted by file name; a file in
// an earlier directory hides one of the same name in a later one
// (libdnf5's create_sorted_file_list).
func dropIns(dirs []string, ext string) []string {
	byName := map[string]string{}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if _, seen := byName[e.Name()]; !seen && e.Type().IsRegular() && filepath.Ext(e.Name()) == ext {
				byName[e.Name()] = filepath.Join(dir, e.Name())
			}
		}
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	slices.Sort(names)
	files := make([]string, len(names))
	for i, name := range names {
		files[i] = byName[name]
	}
	return files
}

// config is what btd needs from dnf5's configuration.
type config struct {
	main  options
	vars  map[string]string
	repos []*repoConfig
}

// repoConfig is an enabled repository's configuration.
type repoConfig struct {
	id       string
	file     string // the .repo file defining it, for check_config_file_age
	opts     options
	priority int
}

// mainUnsupported are main options that change which packages dnf5 sees.
var mainUnsupported = []string{"excludepkgs", "exclude", "includepkgs", "installroot", "cachedir", "system_cachedir"}

// repoUnsupported are the repository options that do.
var repoUnsupported = []string{"excludepkgs", "exclude", "includepkgs"}

// loadConfig reads the main configuration, the variables and the enabled
// repositories, in libdnf5's order.
func loadConfig(db *rpmdb.DB) (*config, error) {
	c := &config{main: options{}}

	// Main options: drop-ins first, then dnf.conf.
	files := append(dropIns(confDirs, ".conf"), confFile)
	var confSecs []section
	for _, file := range files {
		secs, err := readINI(file)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, s := range secs {
			if s.name == "main" {
				maps.Copy(c.main, s.opts)
			}
		}
		if file == confFile {
			confSecs = secs
		}
	}
	for _, opt := range mainUnsupported {
		if c.main[opt] != "" {
			return nil, ErrUnsupported
		}
	}

	vars, err := loadVars(c.main, db)
	if err != nil {
		return nil, err
	}
	c.vars = vars

	if err := c.loadRepos(confSecs); err != nil {
		return nil, err
	}
	return c, nil
}

// loadRepos creates the repositories from dnf.conf's other sections and the
// .repo files, applies repos.override.d, and keeps the enabled ones.
func (c *config) loadRepos(confSecs []section) error {
	var all []*repoConfig
	add := func(file string, secs []section) error {
		for _, s := range secs {
			if s.name == "main" {
				continue
			}
			id, err := c.substitute(s.name)
			if err != nil {
				return err
			}
			// libdnf5 keeps the first repository of an id and logs an error.
			if slices.ContainsFunc(all, func(r *repoConfig) bool { return r.id == id }) {
				continue
			}
			all = append(all, &repoConfig{id: id, file: file, opts: maps.Clone(s.opts)})
		}
		return nil
	}
	if err := add(confFile, confSecs); err != nil {
		return err
	}

	dirs := defaultReposDirs
	if v := c.main["reposdir"]; v != "" {
		dirs = splitList(v)
	}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir) // sorted by name, like libdnf5
		for _, e := range entries {
			if filepath.Ext(e.Name()) != ".repo" || !e.Type().IsRegular() {
				continue
			}
			file := filepath.Join(dir, e.Name())
			secs, err := readINI(file)
			if err != nil {
				return err
			}
			if err := add(file, secs); err != nil {
				return err
			}
		}
	}

	// Overrides: each section is a repository id glob whose options are
	// applied to every matching repository.
	for _, file := range dropIns(repoOverrideDirs, ".repo") {
		secs, err := readINI(file)
		if err != nil {
			return err
		}
		for _, s := range secs {
			pattern, err := c.substitute(s.name)
			if err != nil {
				return err
			}
			for _, r := range all {
				if ok, _ := path.Match(pattern, r.id); ok {
					maps.Copy(r.opts, s.opts)
				}
			}
		}
	}

	for _, r := range all {
		enabled, err := parseBool(r.opts["enabled"], true)
		if err != nil {
			return err
		}
		if !enabled {
			continue
		}
		for _, opt := range repoUnsupported {
			if r.opts[opt] != "" {
				return ErrUnsupported
			}
		}
		if t := r.opts["type"]; t != "" && t != "rpm-md" && t != "rpm" {
			return ErrUnsupported
		}
		r.priority = 99
		if v := r.opts["priority"]; v != "" {
			if r.priority, err = strconv.Atoi(v); err != nil {
				return fmt.Errorf("repository %s: bad priority %q", r.id, v)
			}
		}
		c.repos = append(c.repos, r)
	}
	return nil
}

// cacheDirName is the name of a repository's cache directory: its id and the
// start of the SHA-256 of its metalink, mirrorlist or first baseurl
// (libdnf5's ConfigRepo::get_unique_id).
func (c *config) cacheDirName(r *repoConfig) (string, error) {
	src := r.opts["metalink"]
	if src == "" {
		src = r.opts["mirrorlist"]
	}
	if src == "" {
		if urls := splitList(r.opts["baseurl"]); len(urls) > 0 {
			src = urls[0]
		}
	}
	src, err := c.substitute(src)
	if err != nil {
		return "", err
	}
	if src == "" {
		src = r.id
	}
	sum := sha256.Sum256([]byte(src))
	return r.id + "-" + hex.EncodeToString(sum[:8]), nil
}

// cacheDirs are where dnf5 caches repositories, in the order it uses them:
// root uses the system cache; users their own, cloned from the system cache
// when that one is fresh.
func cacheDirs() []string {
	if os.Geteuid() == 0 {
		return []string{systemCacheDir}
	}
	if dir, err := os.UserCacheDir(); err == nil {
		return []string{filepath.Join(dir, "libdnf5"), systemCacheDir}
	}
	return []string{systemCacheDir}
}

// metadataExpire returns how long a repository's metadata stays fresh, in
// seconds, -1 for never (the repo's metadata_expire, else main's, else 48h).
func (c *config) metadataExpire(r *repoConfig) (int64, error) {
	v := r.opts["metadata_expire"]
	if v == "" {
		v = c.main["metadata_expire"]
	}
	if v == "" {
		return 48 * 3600, nil
	}
	return parseSeconds(v)
}

// parseSeconds parses a time option: a number with an optional s, m, h or d
// unit, or -1 / "never" (libdnf5's OptionSeconds).
func parseSeconds(v string) (int64, error) {
	if v == "-1" || v == "never" {
		return -1, nil
	}
	mult := 1.0
	switch v[len(v)-1] {
	case 's', 'S':
		v = v[:len(v)-1]
	case 'm', 'M':
		mult, v = 60, v[:len(v)-1]
	case 'h', 'H':
		mult, v = 3600, v[:len(v)-1]
	case 'd', 'D':
		mult, v = 86400, v[:len(v)-1]
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad time value %q", v)
	}
	return int64(n * mult), nil
}

// parseBool parses a boolean option, def when empty.
func parseBool(v string, def bool) (bool, error) {
	switch strings.ToLower(v) {
	case "":
		return def, nil
	case "1", "yes", "true", "on":
		return true, nil
	case "0", "no", "false", "off":
		return false, nil
	}
	return false, fmt.Errorf("bad boolean value %q", v)
}

// splitList splits a list option on commas and whitespace.
func splitList(v string) []string {
	return strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
}

// loadVars collects dnf5's variables: files in the vars directories, then
// DNF_VAR_* and DNF0-9 from the environment, then arch, basearch and
// releasever detected from the system (libdnf5's Vars::load).
func loadVars(main options, db *rpmdb.DB) (map[string]string, error) {
	vars := map[string]string{}

	dirs := defaultVarsDirs
	if v := main["varsdir"]; v != "" {
		dirs = splitList(v)
	}
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				return nil, err
			}
			line, _, _ := strings.Cut(string(data), "\n")
			vars[e.Name()] = line
		}
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if name, ok := strings.CutPrefix(k, "DNF_VAR_"); ok {
			vars[name] = v
		} else if len(k) == 4 && strings.HasPrefix(k, "DNF") && isDigit(k[3]) {
			vars[k] = v
		}
	}

	if vars["arch"] == "" {
		var u syscall.Utsname
		if err := syscall.Uname(&u); err != nil {
			return nil, err
		}
		vars["arch"] = utsString(u.Machine[:])
	}
	if vars["basearch"] == "" {
		vars["basearch"] = baseArch(vars["arch"])
	}
	if vars["releasever"] == "" {
		rel, major, minor, err := detectReleasever(db)
		if err != nil {
			return nil, err
		}
		vars["releasever"] = rel
		if major != "" {
			vars["releasever_major"] = major
		}
		if minor != "" {
			vars["releasever_minor"] = minor
		}
	}
	if vars["releasever_major"] == "" {
		vars["releasever_major"], vars["releasever_minor"], _ = strings.Cut(vars["releasever"], ".")
	}
	return vars, nil
}

// utsString converts a NUL-terminated uname field; its element type is int8
// or uint8 depending on the architecture.
func utsString[T int8 | uint8](b []T) string {
	s := make([]byte, 0, len(b))
	for _, c := range b {
		if c == 0 {
			break
		}
		s = append(s, byte(c))
	}
	return string(s)
}

// baseArch maps a machine arch to its base arch, for the arches Fedora
// ships (libdnf5's rpm/arch.cpp has the full table).
func baseArch(arch string) string {
	switch arch {
	case "i386", "i486", "i586", "i686", "athlon", "geode", "pentium3", "pentium4":
		return "i386"
	case "armv7l", "armv7hl", "armv7hnl", "armv8l", "armv8hl":
		return "armhfp"
	case "amd64", "ia32e", "x86_64_v2", "x86_64_v3", "x86_64_v4":
		return "x86_64"
	}
	return arch // x86_64, aarch64, ppc64le, s390x, riscv64...
}

// detectReleasever finds the release version like libdnf5's
// detect_releasevers: the first installed package providing
// system-release(releasever), else system-release; its version, or the
// version of its "= ..." provide; plus the releasever_major/minor provides.
func detectReleasever(db *rpmdb.DB) (rel, major, minor string, err error) {
	for _, provide := range []string{"system-release(releasever)", "system-release"} {
		hnums, err := db.Find("Providename", func(key string) bool { return key == provide })
		if err != nil {
			return "", "", "", err
		}
		if len(hnums) == 0 {
			continue
		}
		slices.Sort(hnums) // rpm's iterator returns the lowest header number first
		pkg, err := db.Package(hnums[0])
		if err != nil {
			return "", "", "", err
		}
		rel = pkg.Version
		for _, d := range pkg.Provides {
			if d.Flags != rpmdb.Equal || d.Version == "" {
				continue
			}
			switch d.Name {
			case provide:
				rel = d.Version
			case "system-release(releasever_major)":
				major = d.Version
			case "system-release(releasever_minor)":
				minor = d.Version
			}
		}
		return rel, major, minor, nil
	}
	return "", "", "", errors.New("cannot detect the release version")
}

// substitute expands $var and ${var} in s. dnf5 also has ${var:-word} and
// ${var:+word}; those are left to dnf5.
func (c *config) substitute(s string) (string, error) {
	if !strings.Contains(s, "$") {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			b.WriteByte(s[i])
			continue
		}
		var name string
		start := i
		switch {
		case i+1 < len(s) && s[i+1] == '{':
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return "", ErrUnsupported
			}
			name = s[i+2 : i+end]
			i += end
		default:
			j := i + 1
			for j < len(s) && (isAlnum(s[j]) || s[j] == '_') {
				j++
			}
			name = s[i+1 : j]
			i = j - 1
		}
		if name == "" || strings.ContainsAny(name, ":-+") {
			return "", ErrUnsupported
		}
		v, ok := c.vars[name]
		if !ok {
			b.WriteString(s[start : i+1]) // dnf5 leaves unknown variables as they are
			continue
		}
		b.WriteString(v)
	}
	return b.String(), nil
}
