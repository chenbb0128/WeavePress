const publicBase = import.meta.env.BASE_URL.endsWith('/')
  ? import.meta.env.BASE_URL
  : `${import.meta.env.BASE_URL}/`;

const appConfig = Object.freeze({
  defaultAvatar: `${publicBase}avatar.svg`,
  defaultHomePath: '/dashboard',
  logo: `${publicBase}logo.svg`,
  name: import.meta.env.VITE_APP_TITLE || 'WeavePress 管理后台',
  namespace: import.meta.env.VITE_APP_NAMESPACE || 'weavepress-admin',
});

type AppConfig = typeof appConfig;

export { appConfig, type AppConfig };
