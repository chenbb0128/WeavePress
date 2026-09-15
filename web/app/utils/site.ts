export const SITE_URL = 'https://wp.pdurl.cn/';
export const READER_HOME = '/articles';
export const ADMIN_HOME = '/admin/';

export function isPublicWebRoute(path: string) {
  return path === '/' || path === '/login';
}

export function resolvePostLoginPath(path?: string) {
  if (!path || path === '/' || path === '/login' || !path.startsWith('/') || path.startsWith('//')) {
    return READER_HOME;
  }
  return path;
}

export function buildWebsiteStructuredData() {
  return {
    '@context': 'https://schema.org',
    '@graph': [
      {
        '@type': 'WebSite',
        name: 'WeavePress',
        url: SITE_URL,
        description: '面向内容团队的采集、AI 采编、微信排版与多平台发布工作台。',
        inLanguage: 'zh-CN',
      },
      {
        '@type': 'SoftwareApplication',
        name: 'WeavePress',
        url: SITE_URL,
        applicationCategory: 'BusinessApplication',
        operatingSystem: 'Web',
        description: '将微信公众号与网页内容集中采集、分析、重组并完成微信排版。',
        offers: {
          '@type': 'Offer',
          price: '0',
          priceCurrency: 'CNY',
        },
      },
    ],
  } as const;
}
