const appConfig = Object.freeze({
  defaultAvatar: '/avatar.svg',
  defaultHomePath: '/dashboard',
  logo: '/logo.svg',
  name: import.meta.env.VITE_APP_TITLE || 'WeavePress 管理后台',
  namespace: import.meta.env.VITE_APP_NAMESPACE || 'weavepress-admin',
});

type AppConfig = typeof appConfig;

export { appConfig, type AppConfig };
