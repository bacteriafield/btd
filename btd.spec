# Copr builds run offline by default: enable "internet access during builds"
# on the Copr project so go can download the modules.
%global debug_package %{nil}

Name:           btd
Version:        0.0.1
Release:        1%{?dist}
Summary:        Better Than DNF, a dnf5 helper that searches and installs from Copr
License:        MIT
URL:            https://github.com/bacteriafield/btd
Source:         %{url}/archive/v%{version}/%{name}-%{version}.tar.gz

BuildRequires:  golang >= 1.26
BuildRequires:  gettext
BuildRequires:  make
Requires:       dnf5
Requires:       dnf5-plugins

%description
btd wraps dnf5 the way yay wraps pacman: one search over the enabled
repositories and Copr, and installs that enable the Copr project a
package needs after asking.

%prep
%autosetup

%build
export GOPROXY=https://proxy.golang.org,direct
export GOFLAGS=-buildmode=pie
%make_build PREFIX=%{_prefix} VERSION=%{version}

%install
%make_install PREFIX=%{_prefix}
%find_lang %{name}

%files -f %{name}.lang
%license LICENSE
%doc README.md
%{_bindir}/btd

%changelog
* Mon Oct 26 2026 bacteriafield <evan.limeira@gmail.com> - 0.0.1-1
- First package
