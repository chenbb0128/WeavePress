import type {
  AIProviderId,
  AISettings,
  TestAISettingsInput,
  UpdateAISettingsInput,
} from '#/api';

export interface AISettingsForm {
  activeProvider: AIProviderId;
  apiKey: string;
  baseUrl: string;
  enabled: boolean;
  model: string;
}

export function createSettingsForm(settings: AISettings): AISettingsForm {
  const form: AISettingsForm = {
    activeProvider: settings.activeProvider,
    apiKey: '',
    baseUrl: '',
    enabled: settings.enabled,
    model: '',
  };
  applyProviderToForm(form, settings, settings.activeProvider);
  return form;
}

export function applyProviderToForm(
  form: AISettingsForm,
  settings: AISettings,
  providerId: AIProviderId,
) {
  const provider =
    settings.providers.find((item) => item.id === providerId) ??
    settings.providers[0];
  if (!provider) return;
  form.activeProvider = provider.id;
  form.baseUrl = provider.baseUrl;
  form.model = provider.model;
  form.apiKey = '';
}

export function buildSettingsInput(
  form: AISettingsForm,
): UpdateAISettingsInput {
  return {
    activeProvider: form.activeProvider,
    apiKey: form.apiKey,
    baseUrl: form.baseUrl.trim(),
    enabled: form.enabled,
    model: form.model.trim(),
  };
}

export function buildConnectionTestInput(
  form: AISettingsForm,
): TestAISettingsInput {
  const { activeProvider, apiKey, baseUrl, model } = buildSettingsInput(form);
  return { activeProvider, apiKey, baseUrl, model };
}

export function getApiKeyPlaceholder(
  settings: AISettings | undefined,
  providerId: AIProviderId,
) {
  const provider = settings?.providers.find((item) => item.id === providerId);
  return provider?.keyConfigured ? '••••••••••••（已保存）' : '输入 API Key';
}
