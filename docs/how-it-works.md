# How it works

| You run | btd does |
|---|---|
| `btd -s term`, `btd term` | searches package names in dnf5's cache and Copr at once, then lets you pick |
| `btd info spec...` | answers from dnf5's cache, printing exactly what `dnf5 info` prints |
| `btd -i pkg` | checks the cache for a provider, else Copr; skips dnf5 when everything is already installed; otherwise runs `dnf5 install` |
| `btd`, `-u`, `-r` | runs `dnf5 upgrade` / `remove` |
| any other dnf5 command | hands it to dnf5 as is |

- **Reads come from dnf5's own cache, writes go through dnf5.** btd never
  changes the system itself: installs, upgrades and removals are dnf5
  transactions. btd never kills dnf; Ctrl+C reaches dnf from the terminal
  and btd keeps dnf's exit code.
- **Copr is btd's AUR.** btd builds nothing: Copr projects ship ready
  RPMs, and btd enables them with `dnf copr enable`.
- **Official repositories first.** `btd install foo` uses the enabled
  repositories when anything provides `foo` (names and capabilities such
  as `pkgconfig(gtk4)`); only then does it search Copr.
- **Enabling a Copr project asks first, defaulting to no.** Once enabled,
  a third-party repository can replace any package on upgrades.
- **`-y` never picks a Copr project.** It answers yes/no questions; to
  install from Copr unattended, name the project: `owner/project/pkg`.
- **One transaction.** Copr projects are enabled first, then all targets
  go to a single `dnf install`.
- **Copr results** are projects built for this system's chroot
  (`fedora-<release>-<arch>`), ranked by exact name, then name containing
  the term. Copr's search API takes 10-17s, so it runs alongside the
  repository search.
- **Privileges**: only dnf transactions are elevated, with `BTD_SUDO`
  or `sudo_bin`, falling back to sudo, run0, doas and pkexec. A no-op
  install needs no elevation at all.

## Where the time goes

Before answering anything, dnf5 loads almost 70 shared libraries, rpm's
macros and its plugins, has librepo check every repository's metadata,
loads the whole rpmdb into libsolv, and reads each repository's solv
cache plus its updateinfo and comps extensions into libsolv's pool. Only
then does it look at the packages.

btd is a static Go binary that starts in about 6 ms. It reads the
configuration (2 ms), maps the solv files and parses them in parallel
(about 60 ms, nearly all of it Fedora's 31 MB file with 75,855 packages),
and decodes only what the query needs: name, version, arch and vendor
for every package, everything else for the packages that matched. From
the rpmdb it reads the small index tables and the headers of the
packages it needs, not the whole database.

## Reading dnf5's cache

Whenever dnf5 refreshes a repository, it stores the parsed metadata as a
libsolv file, `/var/cache/libdnf5/<repo>-<hash>/solv/<repo>.solv` (users
get their own copy under `~/.cache/libdnf5`). btd reads those files, the
rpmdb and dnf5's system state, and uses them only when they are exactly
what dnf5 would use, checked the way libdnf5 checks them:

| What | Where | Checked like |
|---|---|---|
| main options | `/usr/share/dnf5/libdnf.conf.d/*.conf`, `/etc/dnf/libdnf5.conf.d/*.conf`, `/etc/dnf/dnf.conf` | `Base::load_config`: drop-ins sorted by name, then dnf.conf |
| repositories | `/etc/yum.repos.d`, `/etc/distro.repos.d`, `/usr/share/dnf5/repos.d`, plus `repos.override.d` | `RepoSack`: first definition of an id wins, overrides by id glob |
| variables | `/usr/share/dnf5/vars.d`, `/etc/dnf/vars`, `DNF_VAR_*`, arch, releasever from the rpmdb | `Vars::load`, `detect_releasevers` |
| cache directory | `<id>-` + 8 bytes of SHA-256 of the metalink, mirrorlist or first baseurl | `ConfigRepo::get_unique_id` |
| freshness | the primary file's age against `metadata_expire`, the `attrs/expired` marker, the .repo file's age | `Repo::recompute_expired` |
| solv file | its userdata holds SHA-256 of `"H000"` + repomd.xml | `SolvRepo::can_use_solvfile_cache` |
| installed packages | `/usr/lib/sysimage/rpm/rpmdb.sqlite` | as libsolv reads the rpmdb (`repo_rpmdb.c`) |
| install reasons, origins | `/usr/lib/sysimage/libdnf5/packages.toml`, `nevras.toml` | `system::State` |

::: tip When btd runs dnf5 instead
Any doubt means dnf5 answers, which also refreshes the cache for the next
time:

- a repository cache is missing, expired or does not match its repomd.xml;
- the configuration excludes or includes packages, uses modules,
  versionlock, an installroot or another cache directory;
- the rpmdb has an unflushed write-ahead log (rpm is writing to it);
- `info` gets options, or specs dnf5 could read as `name.arch`,
  `name-version` or a NEVRA glob; `install` gets options, globs, or
  packages that are not all installed with a recorded reason;
- `best=True` is set (Fedora ships `best=False`), since dnf5 would then
  upgrade an installed package on `install`.
:::

**Same answers as dnf5.** `btd info` resolves specs and filters packages
like `dnf5/commands/list/list.cpp` (case-insensitive names, repository
priority, latest version, not older than the installed one) and prints
like libsmartcols prints dnf5's tables: the same column widths, sizes,
`\xNN` escaping in the C locale, wrapping and colors on a terminal, and
labels from dnf5's own translation catalogs, so they come out in your
language. The output was compared byte for byte with `dnf5 info` over
170,000 lines (`python3-*`, `lib*`, `glibc*`), and CI repeats the
comparison on every pull request (`benchmark/compat_test.go`).

## Architecture

| Package | Role |
|---|---|
| `main` | command line: flags, command dispatch, search menu, Copr fallback, `info` output |
| `pkg/dnf` | runs dnf5 (queries as the user, transactions elevated); `Native()` loads the cache once, or returns nil so callers fall back to dnf5 |
| `pkg/sack` | reads dnf5's configuration and validates its caches, then answers `Search`, `Provided`, `Info` and `AlreadyInstalled`; also rpm version comparison (`rpmvercmp`) |
| `pkg/solv` | port of libsolv's `repo_add_solv` (solv format 8 and 9): string pool, relations, schemata, incore data and compressed vertical pages, all bounds checked |
| `pkg/rpmdb` | read-only SQLite table reader (b-trees, overflow pages, records) and rpm header parser, without librpm |
| `pkg/copr` | Copr API v3: project search for this system's chroot, package lists |
| `pkg/menu` | numbered result lists and selections like `1 3 2-4` |
| `pkg/settings` | `config.json`, defaults, privilege elevator |
| `benchmark` | benchmarks against dnf5 and the compatibility tests |

Everything is Go with one dependency (gotext, for translations) and no
cgo: btd links neither libsolv nor librpm, so releases stay a single
static binary.
