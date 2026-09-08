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
];

export default routes;
