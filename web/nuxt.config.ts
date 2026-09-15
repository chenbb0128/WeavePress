export default defineNuxtConfig({
  ssr: true,
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
  routeRules: {
    '/': { prerender: true },
    '/login': { ssr: false },
    '/articles': { ssr: false },
    '/articles/**': { ssr: false },
  },
  nitro: {
    prerender: {
      crawlLinks: false,
      routes: ['/'],
      ignore: ['/admin', '/admin/**'],
    },
  },
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
      htmlAttrs: { lang: 'zh-CN' },
      title: 'WeavePress｜公众号采集、AI 采编与微信排版平台',
      meta: [
        { name: 'description', content: 'WeavePress 帮助内容团队集中采集微信公众号与网页文章，通过 AI 分析、辅助改写和微信排版，构建从内容发现到多平台发布的一体化工作流。' },
        { name: 'theme-color', content: '#4f46e5' },
      ],
    },
  },
});
