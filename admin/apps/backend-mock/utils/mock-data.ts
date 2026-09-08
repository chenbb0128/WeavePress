export interface UserInfo {
  homePath?: string;
  id: number;
  password: string;
  realName: string;
  roles: string[];
  username: string;
}

export const MOCK_USERS: UserInfo[] = [
  {
    homePath: '/dashboard',
    id: 1,
    password: '123456',
    realName: 'Administrator',
    roles: ['admin'],
    username: 'admin',
  },
];

export const MOCK_CODES = [
  {
    codes: [
      'dashboard:view',
      'system:user:view',
      'system:user:create',
      'system:user:update',
      'system:user:delete',
    ],
    username: 'admin',
  },
];

const dashboardMenu = {
  component: '/dashboard/index',
  meta: {
    affixTab: true,
    icon: 'lucide:layout-dashboard',
    order: -1,
    title: 'page.dashboard.title',
  },
  name: 'Dashboard',
  path: '/dashboard',
};

const systemUsersMenu = {
  component: '/system/users/index',
  meta: {
    icon: 'lucide:users',
    order: 10,
    title: 'page.users.title',
  },
  name: 'SystemUsers',
  path: '/system/users',
};

export const MOCK_MENUS = [
  {
    menus: [dashboardMenu, systemUsersMenu],
    username: 'admin',
  },
];
