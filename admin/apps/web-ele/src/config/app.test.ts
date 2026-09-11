import { afterEach, describe, expect, it, vi } from 'vitest';

describe('app config public assets', () => {
  afterEach(() => {
    vi.unstubAllEnvs();
    vi.resetModules();
  });

  it('keeps public assets under the deployed admin base path', async () => {
    vi.stubEnv('BASE_URL', '/admin/');
    vi.resetModules();

    const { appConfig } = await import('./app');

    expect(appConfig.logo).toBe('/admin/logo.svg');
    expect(appConfig.defaultAvatar).toBe('/admin/avatar.svg');
  });
});
