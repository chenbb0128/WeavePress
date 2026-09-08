import { defineOverridesPreferences } from '@vben/preferences';

import { appConfig } from '#/config/app';

export const overridesPreferences = defineOverridesPreferences({
  app: {
    accessMode: 'frontend',
    defaultAvatar: appConfig.defaultAvatar,
    defaultHomePath: appConfig.defaultHomePath,
    enableCheckUpdates: false,
    enableCopyPreferences: false,
    name: appConfig.name,
    enableRefreshToken: true,
  },
  copyright: {
    companyName: appConfig.name,
    companySiteLink: '',
    date: '2026',
    enable: false,
    icp: '',
    icpLink: '',
    settingShow: false,
  },
  logo: {
    enable: true,
    showText: true,
    source: appConfig.logo,
    sourceDark: appConfig.logo,
  },
  shortcutKeys: {
    globalLockScreen: false,
  },
  theme: {
    colorPrimary: 'hsl(161 72% 34%)',
    mode: 'light',
  },
  widget: {
    lockScreen: false,
    notification: false,
    timezone: false,
  },
});
