# btd
BTD (Better Than DNF) - An RPM helper

btd wraps dnf5 on Fedora the way yay wraps pacman: one search over the
enabled repositories and [Copr](https://copr.fedorainfracloud.org), and
installs that enable the Copr project a package needs.

## Install

btd runs on Fedora only. From the latest GitHub release (x86_64):

```sh
git clone https://github.com/bacteriafield/btd && cd btd
./scripts/install.sh          # or a given version: ./scripts/install.sh 0.0.1
```

`scripts/deps.sh` installs what btd needs to run (dnf5 and dnf5-plugins);
`install.sh` calls it first.

From source:

```sh
./scripts/deps.sh --build     # adds golang, make and gettext
make PREFIX=/usr && sudo make install PREFIX=/usr
```
Quick install:
```sh
curl -fsSL https://raw.githubusercontent.com/bacteriafield/btd/main/scritps/install.sh | bash
```

Once the Copr project is published:

```sh
sudo dnf copr enable bacteriafield/btd
sudo dnf install btd
```

## Usage

```sh
btd                           # upgrade the system (dnf upgrade)
btd neovim                    # search, then pick numbers to install: 1 3 2-4
btd -s neovim                 # search only
btd -i obs-studio -y          # install, falling back to Copr
btd -i owner/project/pkg      # install pkg from that Copr project
btd -u / btd -r pkg           # upgrade / remove
btd list --installed          # any other dnf5 command is passed through
```

`btd --help` lists every option. Config lives in
`$XDG_CONFIG_HOME/btd/config.json` (`~/.config/btd/config.json`); every
field has a flag, and `--save` writes the flags you pass to it:

```sh
btd --save --no-bottom-up --sudo-bin run0
```

## How it works

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
- **Reads go through `dnf5 repoquery --qf`, writes through dnf5.** btd
  never kills dnf; Ctrl+C reaches dnf from the terminal and btd keeps
  dnf's exit code. The dnf5daemon D-Bus API is the upgrade path if
  repoquery gets too slow.
- **Copr results** are projects built for this system's chroot
  (`fedora-<release>-<arch>`), ranked by exact name, then name containing
  the term. Copr's search API takes 10-17s, so it runs alongside dnf.
- **Privileges**: only dnf transactions are elevated, with `BTD_SUDO`
  or `sudo_bin`, falling back to sudo, run0, doas and pkexec.

## Translations

`po/btd.pot` is the template; regenerate it after changing messages:

```sh
go run github.com/leonelquinteros/gotext/cli/xgotext@v1.7.2 -pkg-tree . -out po -default btd
msgmerge -U po/pt_BR.po po/btd.pot   # per language
```

The shipped translations were machine-made and keep every format verb
(`%s`, `%[1]s`); native speakers are welcome to review them.

## Development

```sh
go test ./...
docker build -t btd . && docker run --rm -it btd   # Fedora box with btd
```

The Copr project needs "internet access during builds" enabled so the
spec can download Go modules.
