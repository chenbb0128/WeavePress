import type { RouteRecordRaw } from 'vue-router';

import { $t } from '#/locales';

const routes: RouteRecordRaw[] = [
  {
    name: 'SystemUsers',
    path: '/system/users',
    component: () => import('#/views/system/users/index.vue'),
    meta: {
      authority: ['admin'],
      icon: 'lucide:users',
      order: 10,
      title: $t('page.users.title'),
    },
  },
];

export default routes;
