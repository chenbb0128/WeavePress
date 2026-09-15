<script lang="ts" setup>
import type { FormInstance, FormRules } from 'element-plus';

import type { AIProviderId, AIProviderSettings, AISettings } from '#/api';

import { computed, onMounted, reactive, ref } from 'vue';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
  ElSwitch,
  ElTag,
} from 'element-plus';

import { getAISettingsApi, updateAISettingsApi } from '#/api';

import {
  applyProviderToForm,
  buildSettingsInput,
  createSettingsForm,
} from './settings-model';

defineOptions({ name: 'AISettings' });

const loading = ref(false);
const saving = ref(false);
const formRef = ref<FormInstance>();
const settings = ref<AISettings>();
const form = reactive({
  activeProvider: 'zhipu' as AIProviderId,
  apiKey: '',
  baseUrl: '',
  enabled: false,
  model: '',
});

const currentProvider = computed<AIProviderSettings | undefined>(() =>
  settings.value?.providers.find((item) => item.id === form.activeProvider),
);
const rules = computed<FormRules>(() => ({
  apiKey:
    form.enabled && !currentProvider.value?.keyConfigured
      ? [{ required: true, message: '首次启用该服务商时请输入 API Key' }]
      : [],
  baseUrl: currentProvider.value?.baseUrlEditable
    ? [
        { required: true, message: '请输入兼容服务的 Base URL' },
        {
          pattern: /^https:\/\/[^\s]+$/,
          message: 'Base URL 必须使用 HTTPS',
          trigger: 'blur',
        },
      ]
    : [],
  model: [{ required: true, message: '请选择或输入 Model ID' }],
}));

function replaceSettings(value: AISettings) {
  settings.value = value;
  Object.assign(form, createSettingsForm(value));
}

async function load() {
  loading.value = true;
  try {
    replaceSettings(await getAISettingsApi());
  } catch {
    ElMessage.error('AI 设置加载失败，请稍后重试');
  } finally {
    loading.value = false;
  }
}

function changeProvider(providerId: AIProviderId) {
  if (!settings.value) return;
  applyProviderToForm(form, settings.value, providerId);
  formRef.value?.clearValidate();
}

async function save() {
  if (!(await formRef.value?.validate().catch(() => false))) return;
  saving.value = true;
  try {
    const result = await updateAISettingsApi(buildSettingsInput(form));
    replaceSettings(result);
    ElMessage.success('AI 设置已保存并立即生效');
  } catch {
    ElMessage.error('AI 设置保存失败，请检查配置');
  } finally {
    form.apiKey = '';
    saving.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div v-loading="loading" class="wp-page">
    <ElCard class="wp-page-hero" shadow="never">
      <div class="wp-page-hero__content">
        <div>
          <p class="wp-page-eyebrow">AI PROVIDER</p>
          <h1 class="wp-page-title">AI 服务设置</h1>
          <p class="wp-page-description">
            分别保存智谱 GLM、OpenAI 或兼容服务，保存后 API 与 Worker 无需重启。
          </p>
        </div>
        <ElTag :type="form.enabled ? 'success' : 'info'" effect="plain" size="large">
          {{ form.enabled ? 'AI 已启用' : 'AI 已停用' }}
        </ElTag>
      </div>
    </ElCard>

    <ElCard class="wp-panel ai-settings-panel" shadow="never">
      <ElAlert
        :closable="false"
        show-icon
        title="API Key 会加密保存在数据库中；页面不会回显已保存的 Key。"
        type="info"
      />
      <ElForm
        ref="formRef"
        class="ai-settings-form"
        label-position="top"
        :model="form"
        :rules="rules"
      >
        <ElFormItem label="启用 AI">
          <ElSwitch v-model="form.enabled" />
          <span class="field-hint">关闭后不会创建或执行新的 AI 任务</span>
        </ElFormItem>

        <ElFormItem label="服务商" prop="activeProvider">
          <ElSelect
            v-model="form.activeProvider"
            class="field-control"
            @change="changeProvider"
          >
            <ElOption
              v-for="provider in settings?.providers ?? []"
              :key="provider.id"
              :label="provider.name"
              :value="provider.id"
            />
          </ElSelect>
        </ElFormItem>

        <ElFormItem label="Base URL" prop="baseUrl">
          <ElInput
            v-model="form.baseUrl"
            class="field-control"
            :disabled="!currentProvider?.baseUrlEditable"
            placeholder="https://llm.example.com/v1"
          />
        </ElFormItem>

        <ElFormItem label="Model ID" prop="model">
          <ElSelect
            v-model="form.model"
            allow-create
            class="field-control"
            default-first-option
            filterable
            placeholder="选择或输入 Model ID"
          >
            <ElOption
              v-for="model in currentProvider?.modelOptions ?? []"
              :key="model"
              :label="model"
              :value="model"
            />
          </ElSelect>
        </ElFormItem>

        <ElFormItem label="API Key" prop="apiKey">
          <ElInput
            v-model="form.apiKey"
            autocomplete="new-password"
            class="field-control"
            placeholder="输入新 Key；留空保留原 Key"
            show-password
            type="password"
          />
          <span class="field-hint">
            {{
              currentProvider?.keyConfigured
                ? '已安全保存，留空不会修改'
                : '该服务商尚未配置 Key'
            }}
          </span>
        </ElFormItem>

        <ElButton
          v-access:code="'ai:settings:update'"
          :loading="saving"
          size="large"
          type="primary"
          @click="save"
        >
          保存设置
        </ElButton>
      </ElForm>
    </ElCard>
  </div>
</template>

<style scoped>
.ai-settings-panel {
  max-width: 880px;
}

.ai-settings-form {
  margin-top: 24px;
}

.field-control {
  width: min(100%, 620px);
}

.field-hint {
  margin-left: 12px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
}

@media (max-width: 640px) {
  .field-hint {
    display: block;
    margin: 8px 0 0;
    width: 100%;
  }
}
</style>
