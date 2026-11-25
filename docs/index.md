---
# layout: home renders the hero and the feature cards from this frontmatter;
# the markdown below them shows up under the cards.
# https://vitepress.dev/reference/default-theme-home-page
layout: home

hero:
  name: btd
  text: Better Than DNF
  tagline: An RPM helper for Fedora. One search over your repositories and Copr, and queries answered 9x faster than dnf5, with the same results.
  actions:
    - theme: brand
      text: Get started
      link: /guide
    - theme: alt
      text: How it works
      link: /how-it-works
    - theme: alt
      text: GitHub
      link: https://github.com/bacteriafield/btd

features:
  - icon: ⚡
    title: 9x faster than dnf5
    details: Search, info and no-op installs take about 80 ms instead of 700 ms, by reading dnf5's own cache directly.
  - icon: 📦
    title: Copr is btd's AUR
    details: One search covers the enabled repositories and Copr. Installs enable the Copr project a package needs, after asking.
  - icon: 🎯
    title: Same answers as dnf5
    details: btd info prints exactly what dnf5 info prints, compared byte for byte over 170,000 lines in CI.
  - icon: 🛡️
    title: Writes go through dnf5
    details: Installs, upgrades and removals are dnf5 transactions. When the cache is in any doubt, dnf5 answers instead.
  - icon: 🪶
    title: One static binary
    details: Pure Go, no cgo. btd links neither libsolv nor librpm and starts in about 6 ms.
  - icon: ↪️
    title: Drop-in for dnf
    details: Any dnf5 command btd doesn't handle is passed through as is, so btd list --installed just works.
---

## Quick start

```sh
curl -fsSL https://raw.githubusercontent.com/bacteriafield/btd/main/scripts/install.sh | bash

btd neovim          # search, then pick numbers to install: 1 3 2-4
btd info neovim     # dnf5 info, answered from the cache
btd -i obs-studio   # install, falling back to Copr
```
