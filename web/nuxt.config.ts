export default defineNuxtConfig({
  ssr: false,
  compatibilityDate: '2026-09-08',
  modules: ['@nuxt/ui', '@pinia/nuxt'],
  css: ['~/assets/css/main.css'],
  devtools: { enabled: false },
  fonts: {
    providers: {
      google: false,
      googleicons: false,
    },
  },
  runtimeConfig: { public: { apiBase: '/api' } },
  vite: {
    server: {
      proxy: {
        '/api': { target: 'http://localhost:8080', changeOrigin: true },
        '/media': { target: 'http://localhost:8080', changeOrigin: true },
      },
    },
  },
  app: {
    head: {
      title: 'WeavePress · 内容中心',
      meta: [{ name: 'description', content: 'WeavePress 团队内容阅读中心' }],
    },
  },
});
