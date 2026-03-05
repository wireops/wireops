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

  runtimeConfig: {
    public: {
      pocketbaseUrl: process.env.NUXT_PUBLIC_POCKETBASE_URL || 'http://localhost:8090',
    },
  },

  devtools: { enabled: true },
})
