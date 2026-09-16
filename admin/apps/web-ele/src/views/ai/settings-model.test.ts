import type { AISettings } from '#/api';

import { describe, expect, it } from 'vitest';

import {
  applyProviderToForm,
  buildSettingsInput,
  createSettingsForm,
} from './settings-model';

const settings: AISettings = {
  activeProvider: 'zhipu',
  enabled: false,
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
    {
      baseUrl: 'https://api.openai.com/v1',
      baseUrlEditable: false,
      id: 'openai',
      keyConfigured: false,
      model: 'gpt-5-mini',
      modelOptions: ['gpt-5', 'gpt-5-mini'],
      name: 'OpenAI',
    },
  ],
};

describe('ai settings view model', () => {
  it('loads the active provider without exposing a saved key', () => {
    const form = createSettingsForm(settings);

    expect(form).toEqual({
      activeProvider: 'zhipu',
      apiKey: '',
      baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
      enabled: false,
      model: 'glm-5.3-flash',
    });
  });

  it('switches provider while keeping the key field empty', () => {
    const form = createSettingsForm(settings);
    form.apiKey = 'must-be-cleared';

    applyProviderToForm(form, settings, 'openai');

    expect(form.activeProvider).toBe('openai');
    expect(form.model).toBe('gpt-5-mini');
    expect(form.apiKey).toBe('');
  });

  it('trims non-sensitive fields but preserves the entered key', () => {
    const form = createSettingsForm(settings);
    form.model = '  glm-custom  ';
    form.apiKey = ' key-with-spaces ';

    expect(buildSettingsInput(form)).toEqual({
      activeProvider: 'zhipu',
      apiKey: ' key-with-spaces ',
      baseUrl: 'https://open.bigmodel.cn/api/paas/v4',
      enabled: false,
      model: 'glm-custom',
    });
  });
});
