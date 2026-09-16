import type { App } from 'vue';

import { createApp, nextTick } from 'vue';

import { afterEach, describe, expect, it, vi } from 'vitest';

import AISettings from './settings.vue';

const mocks = vi.hoisted(() => ({
  getAISettingsApi: vi.fn(),
  testAISettingsApi: vi.fn(),
  updateAISettingsApi: vi.fn(),
}));

vi.mock('#/api', () => ({
  getAISettingsApi: mocks.getAISettingsApi,
  testAISettingsApi: mocks.testAISettingsApi,
  updateAISettingsApi: mocks.updateAISettingsApi,
}));

const apps: App[] = [];

async function settle() {
  for (let index = 0; index < 5; index += 1) {
    await Promise.resolve();
    await nextTick();
  }
}

async function mountSettings() {
  mocks.getAISettingsApi.mockResolvedValue({
    activeProvider: 'zhipu',
    enabled: true,
    providers: [
      {
        baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
        baseUrlEditable: false,
        id: 'zhipu',
        keyConfigured: true,
        model: 'glm-5.3-flash',
        modelOptions: ['glm-5.3-flash'],
        name: '智谱 GLM',
      },
    ],
  });
  mocks.testAISettingsApi.mockResolvedValue({
    latencyMs: 10,
    model: 'GLM-5.3',
    provider: 'zhipu',
    success: true,
  });
  const host = document.createElement('div');
  document.body.append(host);
  const app = createApp(AISettings);
  app.directive('access', () => {});
  app.directive('loading', () => {});
  apps.push(app);
  app.mount(host);
  await settle();
  return host;
}

describe('ai settings', () => {
  afterEach(() => {
    for (const app of apps.splice(0)) app.unmount();
    document.body.textContent = '';
    vi.resetAllMocks();
  });

  it('renders the model id as a plain text field', async () => {
    const host = await mountSettings();
    const modelItem = [
      ...host.querySelectorAll<HTMLElement>('.el-form-item'),
    ].find((item) => item.textContent?.includes('Model ID'));

    expect(modelItem?.querySelector('.el-select')).toBeNull();
    const input = modelItem?.querySelector<HTMLInputElement>('input');
    expect(input).toBeTruthy();
    if (!input) throw new Error('Model ID input was not rendered');
    input.value = 'GLM-5.3';
    input.dispatchEvent(new Event('input', { bubbles: true }));
    await nextTick();
    expect(input.value).toBe('GLM-5.3');
  });
});
