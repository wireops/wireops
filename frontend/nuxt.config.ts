import { useNuxt } from '@nuxt/kit'

export default defineNuxtConfig({
  compatibilityDate: '2025-01-01',

  future: {
    compatibilityVersion: 4,
  },

  modules: ['@nuxt/ui', '@nuxt/eslint'],

  css: ['~/assets/css/main.css'],

  ssr: false,

  nitro: {
    preset: 'static',
  },

  // wireops ships as a single self-hosted binary and is routinely run on
  // air-gapped or egress-filtered hosts, so icons must never be resolved over
  // the network. `provider: 'none'` disables the Iconify API fallback and
  // `clientBundle.scan` inlines every statically-referenced icon at build time.
  // The scanner's default globs only cover templates, but plenty of our icon
  // names live in plain .ts (stack-status, error-parser, composables), so the
  // include list is widened to match.
  icon: {
    provider: 'none',
    serverBundle: false,
    clientBundle: {
      scan: {
        globInclude: ['**/*.{vue,jsx,tsx,js,ts}'],
      },
      sizeLimitKb: 1024,
    },
  },

  hooks: {
    // @nuxt/ui's own default icons (checkbox check, select chevrons, modal
    // close, ...) come from appConfig rather than from any file the scanner
    // looks at, so they have to be added by hand or those components render
    // blank once the Iconify API fallback is off.
    'icon:clientBundleIcons': (icons: Set<string>) => {
      const appConfig = useNuxt().options.appConfig as { ui?: { icons?: Record<string, string> } }
      for (const name of Object.values(appConfig.ui?.icons ?? {})) {
        if (typeof name !== 'string') continue
        const match = /^i[-:](lucide|simple-icons)[-:](.+)$/.exec(name)
        if (match) icons.add(`${match[1]}:${match[2]}`)
      }
    },
  },

  runtimeConfig: {
    public: {
      pocketbaseUrl: process.env.NUXT_PUBLIC_POCKETBASE_URL || '',
    },
  },

  devtools: { enabled: true },

  app: {
    head: {
      title: 'WireOps',
      titleTemplate: 'WireOps - %s',
      link: [
        { rel: 'icon', type: 'image/x-icon', href: '/favicon.ico' },
        { rel: 'icon', type: 'image/png', href: '/favicon.png' },
        { rel: 'manifest', href: '/manifest.webmanifest' },
        { rel: 'apple-touch-icon', href: '/apple-touch-icon.png' },
      ],
      meta: [
        { name: 'viewport', content: 'width=device-width, initial-scale=1, viewport-fit=cover' },
        { name: 'theme-color', content: '#202032' },
        { name: 'mobile-web-app-capable', content: 'yes' },
        { name: 'apple-mobile-web-app-capable', content: 'yes' },
        { name: 'apple-mobile-web-app-status-bar-style', content: 'black-translucent' },
        { name: 'apple-mobile-web-app-title', content: 'WireOps' },
      ],
    },
  },
})
