# Packaging notes
#
#
# Network: Copr builds run offline by default. Enable "internet access during
# builds" on the Copr project so go can download the modules from GOPROXY.
%global debug_package %{nil}

Name:           btd
Version:        0.0.1
Release:        1%{?dist}
Summary:        Better Than DNF, a dnf5 helper that searches and installs from Copr
License:        MIT
URL:            https://github.com/bacteriafield/btd
# The GitHub archive of tag v<version>; it unpacks to btd-<version>/.
Source:         %{url}/archive/v%{version}/%{name}-%{version}.tar.gz

# go.mod requires go 1.27, newer than Fedora 44's golang (1.26): %build sets
# GOTOOLCHAIN=auto, so go downloads the toolchain go.mod names. 1.21 is the
# first go that can.
BuildRequires:  golang >= 1.21
# msgfmt compiles the translations in po/ into .mo catalogs.
BuildRequires:  gettext
BuildRequires:  make

# Transactions, and every query btd cannot answer from the cache, go to dnf5.
Requires:       dnf5
# `dnf copr enable`, which btd runs to enable a package's Copr project.
Requires:       dnf5-plugins

%description
btd wraps dnf5 on Fedora: one search over the enabled repositories and
Copr (https://copr.fedorainfracloud.org), installs that enable the Copr
project a package needs, and queries answered about 9x faster than dnf5,
with the same results, by reading dnf5's own cache directly.

Search results are numbered and picked like 1 3 2-4. Installs prefer the
official repositories, by package name or capability such as
pkgconfig(gtk4), and only then look in Copr. Copr is btd's AUR: btd builds
nothing, Copr projects ship ready RPMs. Enabling a Copr project always
asks first and defaults to no, and -y never picks one.

The cache btd reads is dnf5's: the libsolv files under /var/cache/libdnf5,
the rpmdb and libdnf5's system state. btd uses them only when they are
exactly what dnf5 would use, checked the way libdnf5 checks them, and
runs dnf5 otherwise, which also refreshes the cache. btd info prints
exactly what dnf5 info prints, with labels from dnf5's own translations.

btd never changes the system itself: installs, upgrades and removals are
dnf5 transactions, elevated with sudo, run0, doas or pkexec, and any other
dnf5 command is passed through as is. btd is pure Go: it links neither
libsolv nor librpm.

%prep
%autosetup

%build
# Modules come from the Go proxy, which is why Copr needs network access.
export GOPROXY=https://proxy.golang.org,direct
# Fedora's go.env sets GOTOOLCHAIN=local; auto builds with go.mod's toolchain
# line (go1.27.2, verified against sum.golang.org), whatever golang the
# chroot has.
export GOTOOLCHAIN=auto
export GOFLAGS=-buildmode=pie
# PREFIX sets where btd looks for its translations (main.locale), VERSION
# what btd --version prints (main.btdVersion).
%make_build PREFIX=%{_prefix} VERSION=%{version}

%install
# Installs /usr/bin/btd and /usr/share/locale/<lang>/LC_MESSAGES/btd.mo.
%make_install PREFIX=%{_prefix}
%find_lang %{name}

%files -f %{name}.lang
%license LICENSE
%doc README.md
%{_bindir}/btd

%changelog
* Mon Oct 26 2026 bacteriafield <evan.limeira@gmail.com> - 0.0.1-1
- First package
