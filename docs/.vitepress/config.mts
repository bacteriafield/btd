import { defineConfig } from 'vitepress'

const repo = 'https://github.com/bacteriafield/btd'

// https://vitepress.dev/reference/site-config
export default defineConfig({
  title: 'btd',
  description: 'Better Than DNF: an RPM helper that answers dnf5 queries 9x faster',
  // ponytail: no `base`; set base: '/btd/' if this gets served from
  // GitHub Pages at bacteriafield.github.io/btd.

  // https://vitepress.dev/reference/default-theme-config
  themeConfig: {
    nav: [
      { text: 'Guide', link: '/guide' },
      { text: 'How it works', link: '/how-it-works' },
      { text: 'Releases', link: `${repo}/releases` }
    ],

    // One sidebar for every page: the site is only two of them.
    sidebar: [
      {
        text: 'Guide',
        items: [
          { text: 'Install', link: '/guide#install' },
          { text: 'Usage', link: '/guide#usage' },
          { text: 'Benchmarks', link: '/guide#benchmarks' }
        ]
      },
      {
        text: 'Internals',
        items: [
          { text: 'How it works', link: '/how-it-works' },
          { text: "Reading dnf5's cache", link: '/how-it-works#reading-dnf5-s-cache' },
          { text: 'Architecture', link: '/how-it-works#architecture' }
        ]
      }
    ],

    // Built-in client-side search, no external service.
    search: { provider: 'local' },

    socialLinks: [{ icon: 'github', link: repo }],

    editLink: {
      pattern: `${repo}/edit/main/docs/:path`,
      text: 'Edit this page on GitHub'
    },

    footer: {
      message: 'Released under the MIT License.',
      copyright: 'Copyright © 2026 Evandro'
    }
  }
})
