#!/usr/bin/env bash
# Installs btd from a GitHub release on Fedora, the latest unless a version is given.
#   scripts/install.sh [version]
set -euo pipefail

repo=bacteriafield/btd

"$(dirname "$(readlink -f "$0")")/deps.sh"

if [ "$(uname -m)" != x86_64 ]; then
    echo "btd releases are x86_64 only; build it with: scripts/deps.sh --build && sudo make install PREFIX=/usr" >&2
    exit 1
fi

version=${1:-}
if [ -z "$version" ]; then
    # releases/latest redirects to .../releases/tag/v<version>
    latest=$(curl -fsSLo /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")
    case $latest in
        */tag/v*) version=${latest##*/tag/v} ;;
        *) echo "no btd release found at https://github.com/$repo/releases" >&2; exit 1 ;;
    esac
fi
version=${version#v}

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "Downloading btd $version..."
if ! curl -fL --progress-bar -o "$tmp/btd.tar.gz" \
    "https://github.com/$repo/releases/download/v$version/btd-$version-x86_64.tar.gz"; then
    echo "btd $version not found at https://github.com/$repo/releases" >&2
    exit 1
fi
tar -xzf "$tmp/btd.tar.gz" -C "$tmp"

sudo=()
if [ "$EUID" -ne 0 ]; then
    sudo=(sudo)
fi

# Copies file by file: extracting the archive over / would hand /, /usr and
# the other directories to the owner and mode stored in the archive.
"${sudo[@]}" install -Dm755 "$tmp/usr/bin/btd" /usr/bin/btd
(cd "$tmp" && find usr/share -type f) | while IFS= read -r file; do
    "${sudo[@]}" install -Dm644 "$tmp/$file" "/$file"
done

echo "Installed $(btd --version)"
