import type { Router } from 'vue-router';

import { createPinia, setActivePinia } from 'pinia';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { useAccessStore, useUserStore } from '@vben/stores';

import { createRouterGuard } from './guard';

const mocks = vi.hoisted(() => ({
  fetchUserInfo: vi.fn(),
  generateAccess: vi.fn(),
  getAccessCodes: vi.fn(),
}));

vi.mock('@vben/preferences', () => ({
  preferences: {
    app: {
      defaultHomePath: '/',
    },
    transition: {
      progress: false,
    },
  },
}));

vi.mock('@vben/utils', () => ({
  startProgress: vi.fn(),
  stopProgress: vi.fn(),
}));

vi.mock('#/router/routes', () => ({
  accessRoutes: [],
  coreRouteNames: [],
}));

vi.mock('#/api', () => ({
  getAccessCodesApi: mocks.getAccessCodes,
}));

vi.mock('#/store', () => ({
  useAuthStore: () => ({
    fetchUserInfo: mocks.fetchUserInfo,
  }),
}));

vi.mock('./access', () => ({
  generateAccess: mocks.generateAccess,
}));

describe('router access guard', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.clearAllMocks();
    mocks.getAccessCodes.mockResolvedValue(['ai:settings:update']);
    mocks.generateAccess.mockResolvedValue({
      accessibleMenus: [],
      accessibleRoutes: [],
    });
  });

  it('refreshes access codes when restoring a signed-in session', async () => {
    const accessStore = useAccessStore();
    const userStore = useUserStore();
    const beforeGuards: Array<(to: any, from: any) => Promise<unknown>> = [];
    const router = {
      afterEach: vi.fn(),
      beforeEach: vi.fn((guard) => beforeGuards.push(guard)),
      resolve: vi.fn((path) => ({ path })),
    } as unknown as Router;

    accessStore.setAccessToken('access-token');
    accessStore.setAccessCodes(['article:view']);
    userStore.setUserInfo({ homePath: '/', roles: ['admin'] } as any);
    createRouterGuard(router);

    await beforeGuards[1]?.(
      {
        fullPath: '/ai/settings',
        meta: {},
        name: 'AISettings',
        path: '/ai/settings',
        query: {},
      },
      { query: {} },
    );

    expect(mocks.getAccessCodes).toHaveBeenCalledOnce();
    expect(accessStore.accessCodes).toEqual(['ai:settings:update']);
  });
});
