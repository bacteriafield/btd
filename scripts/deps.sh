#!/usr/bin/env bash
# Installs what btd needs to run on Fedora; --build adds what `make install` needs.
#   scripts/deps.sh [--build]
set -euo pipefail

# shellcheck source=/dev/null
. /etc/os-release
if [ "${ID:-}" != fedora ]; then
    echo "btd only supports Fedora (this is ${PRETTY_NAME:-unknown})" >&2
    exit 1
fi

# dnf5-plugins ships `dnf copr`, which btd uses to enable Copr projects.
pkgs=(dnf5 dnf5-plugins)
if [ "${1:-}" = --build ]; then
    pkgs+=(golang make gettext)
fi

sudo=()
if [ "$EUID" -ne 0 ]; then
    sudo=(sudo)
fi
"${sudo[@]}" dnf install -y "${pkgs[@]}"
