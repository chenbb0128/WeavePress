import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    name: 'CollectionCreate',
    path: '/collection/new',
    component: () => import('#/views/collection/submit.vue'),
    meta: {
      authority: ['admin', 'editor'],
      icon: 'lucide:circle-plus',
      order: 1,
      title: $t('page.collection.submitTitle'),
    },
  },
  {
    name: 'CollectionJobs',
    path: '/collection/jobs',
    component: () => import('#/views/collection/jobs.vue'),
    meta: {
      authority: ['admin', 'editor'],
      icon: 'lucide:list-checks',
      order: 2,
      title: $t('page.collection.jobsTitle'),
    },
  },
  {
    name: 'Articles',
    path: '/articles',
    component: () => import('#/views/articles/index.vue'),
    meta: {
      authority: ['admin', 'editor'],
      icon: 'lucide:newspaper',
      order: 3,
      title: $t('page.articles.title'),
    },
  },
  {
    name: 'ArticleDetail',
    path: '/articles/:id',
    component: () => import('#/views/articles/detail.vue'),
    meta: {
      activePath: '/articles',
      authority: ['admin', 'editor'],
      hideInMenu: true,
      title: $t('page.articles.detailTitle'),
    },
  },
  {
    name: 'AIWorkbench',
    path: '/ai/articles/:articleId',
    component: () => import('#/views/ai/workbench.vue'),
    meta: {
      activePath: '/articles',
      authority: ['admin', 'editor'],
      hideInMenu: true,
      title: $t('page.ai.workbenchTitle'),
    },
  },
  {
    name: 'AIJobs',
    path: '/ai/jobs',
    component: () => import('#/views/ai/jobs.vue'),
    meta: {
      authority: ['admin', 'editor'],
      icon: 'lucide:sparkles',
      order: 4,
      title: $t('page.ai.jobsTitle'),
    },
  },
  {
    name: 'Drafts',
    path: '/drafts',
    component: () => import('#/views/drafts/index.vue'),
    meta: {
      authority: ['admin', 'editor'],
      icon: 'lucide:file-pen-line',
      order: 5,
      title: $t('page.drafts.title'),
    },
  },
  {
    name: 'DraftDetail',
    path: '/drafts/:id',
    component: () => import('#/views/drafts/detail.vue'),
    meta: {
      activePath: '/drafts',
      authority: ['admin', 'editor'],
      hideInMenu: true,
      title: $t('page.drafts.detailTitle'),
    },
  },
  {
    name: 'WeChatPublishJobs',
    path: '/wechat/publish-jobs',
    component: () => import('#/views/wechat/publish-jobs.vue'),
    meta: {
      authority: ['admin', 'editor'],
      icon: 'lucide:send',
      order: 6,
      title: $t('page.wechat.publishJobsTitle'),
    },
  },
];

export default routes;
