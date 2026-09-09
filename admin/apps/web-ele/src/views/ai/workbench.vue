<script lang="ts" setup>
import type {
  AIAnalysis,
  AIGeneration,
  AIJob,
  AIStatus,
  AITone,
  Article,
  GenerationInput,
} from '#/api';

import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from 'vue';
import { useRoute, useRouter } from 'vue-router';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElRadio,
  ElRadioGroup,
  ElSelect,
  ElSkeleton,
  ElTag,
} from 'element-plus';

import {
  getAIAnalysesApi,
  getAIAnalysisApi,
  getAIGenerationApi,
  getAIJobApi,
  getAIStatusApi,
  getArticleApi,
  retryAIJobApi,
  startAIAnalysisApi,
  startAIGenerationApi,
} from '#/api';

import {
  AI_JOB_STATUS_LABELS,
  canGenerate,
  canRetry,
  shouldPoll,
  validateGenerationForm,
} from './model';

defineOptions({ name: 'AIWorkbench' });

const POLL_INTERVAL = 3000;
const route = useRoute();
const router = useRouter();
const articleId = Number(route.params.articleId);
const validArticleId = Number.isSafeInteger(articleId) && articleId > 0;

const loading = ref(true);
const loadError = ref('');
const article = ref<Article>();
const aiStatus = ref<AIStatus>({ enabled: false, model: '', provider: '' });
const analyses = ref<AIAnalysis[]>([]);
const selectedAnalysis = ref<AIAnalysis>();
const selectedAnalysisId = ref<number>();
const analysisJob = ref<AIJob>();
const analysisSubmitting = ref(false);
const analysisSelecting = ref(false);
const analysisRequestError = ref('');
const generation = ref<AIGeneration>();
const generationSubmitting = ref(false);
const generationRequestError = ref('');
const generationForm = reactive({
  additionalInstructions: '',
  angleId: '',
  audience: '',
  targetWords: 1000,
  tone: 'professional' as AITone,
});

const toneOptions: { label: string; value: AITone }[] = [
  { label: '专业', value: 'professional' },
  { label: '平实', value: 'plain' },
  { label: '分析', value: 'analytical' },
  { label: '叙事', value: 'storytelling' },
  { label: '温暖', value: 'warm' },
];

let destroyed = false;
let loadEpoch = 0;
let selectionEpoch = 0;
let analysisActionEpoch = 0;
let generationActionEpoch = 0;
let analysisPollEpoch = 0;
let generationPollEpoch = 0;
let analysisTimer: ReturnType<typeof setTimeout> | undefined;
let generationTimer: ReturnType<typeof setTimeout> | undefined;
let pendingGenerationKey = '';
let pendingGenerationFingerprint = '';

const assetURLs = computed(
  () =>
    new Map(
      (article.value?.assets ?? [])
        .filter(
          (asset) => asset.downloadStatus === 'completed' && asset.mediaUrl,
        )
        .map((asset) => [asset.id, asset.mediaUrl]),
    ),
);
const analysisAngles = computed(
  () => selectedAnalysis.value?.angles.slice(0, 3) ?? [],
);
const analysisPolling = computed(() =>
  analysisJob.value ? shouldPoll(analysisJob.value.status) : false,
);
const generationJob = computed(() => generation.value?.job);
const generationPolling = computed(() =>
  generationJob.value ? shouldPoll(generationJob.value.status) : false,
);
const generationInputForValidation = computed<GenerationInput>(() => ({
  ...generationForm,
  idempotencyKey: 'validation-key',
}));
const generationAllowed = computed(
  () =>
    !analysisSelecting.value &&
    Boolean(aiStatus.value.enabled) &&
    Boolean(selectedAnalysis.value?.job) &&
    canGenerate(
      selectedAnalysis.value?.job?.status ?? 'failed',
      generationInputForValidation.value,
    ),
);
const generationButtonLabel = computed(() => {
  if (generationRequestError.value) return '重试提交';
  return generation.value ? '重新生成' : '生成稿件';
});

function jobTagType(status: AIJob['status']) {
  if (status === 'completed') return 'success';
  if (status === 'failed') return 'danger';
  return 'primary';
}

function safeJobCode(job: AIJob) {
  const code = job.errorCode?.trim() ?? '';
  return /^[A-Z][A-Z0-9_]{0,79}$/.test(code) ? code : 'AI_TASK_FAILED';
}

function safeJobMessage(job: AIJob) {
  const message = job.errorMessage?.trim() ?? '';
  const sensitive =
    /(api[\s_-]*key|prompt|raw\s+output|sk-[a-z\d]|原始输出|提示词)/i;
  if (!message || sensitive.test(message)) return 'AI 任务处理失败，请稍后重试';
  return [...message].slice(0, 240).join('');
}

function formatDuration(job: AIJob) {
  if (!job.startedAt) return '-';
  const end = job.finishedAt ?? job.updatedAt;
  const milliseconds = Date.parse(end) - Date.parse(job.startedAt);
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-';
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(1)} 秒`;
  return `${(milliseconds / 60_000).toFixed(1)} 分钟`;
}

function headingTag(level?: number) {
  if (level === 3) return 'h3';
  if (level === 4) return 'h4';
  return 'h2';
}

function generationFingerprint() {
  return JSON.stringify([
    generationForm.angleId,
    generationForm.audience,
    generationForm.tone,
    generationForm.targetWords,
    generationForm.additionalInstructions,
  ]);
}

watch(
  () => [
    generationForm.angleId,
    generationForm.audience,
    generationForm.tone,
    generationForm.targetWords,
    generationForm.additionalInstructions,
  ],
  () => {
    pendingGenerationKey = '';
    pendingGenerationFingerprint = '';
  },
);

function stopAnalysisPolling() {
  analysisPollEpoch += 1;
  if (analysisTimer) clearTimeout(analysisTimer);
  analysisTimer = undefined;
}

function stopGenerationPolling() {
  generationPollEpoch += 1;
  if (generationTimer) clearTimeout(generationTimer);
  generationTimer = undefined;
}

function suspendGenerationActivity() {
  stopGenerationPolling();
  generationActionEpoch += 1;
  generationSubmitting.value = false;
}

function resetGenerationContext() {
  generation.value = undefined;
  generationRequestError.value = '';
  pendingGenerationKey = '';
  pendingGenerationFingerprint = '';
}

function resumeGenerationPolling() {
  if (generation.value?.job && shouldPoll(generation.value.job.status)) {
    scheduleGenerationPoll();
  }
}

function applyAnalysis(value: AIAnalysis) {
  selectedAnalysis.value = value;
  selectedAnalysisId.value = value.id;
  generationForm.angleId = value.angles[0]?.id ?? '';
}

async function loadAnalysis(id: number) {
  const epoch = ++selectionEpoch;
  suspendGenerationActivity();
  analysisSelecting.value = true;
  analysisRequestError.value = '';
  let committed = false;
  try {
    const data = await getAIAnalysisApi(id);
    if (destroyed || epoch !== selectionEpoch) return;
    applyAnalysis(data);
    resetGenerationContext();
    committed = true;
  } catch {
    if (!destroyed && epoch === selectionEpoch) {
      selectedAnalysisId.value = selectedAnalysis.value?.id;
      analysisRequestError.value = '分析资料加载失败，请稍后重试';
    }
  } finally {
    if (!destroyed && epoch === selectionEpoch) {
      analysisSelecting.value = false;
      if (!committed) resumeGenerationPolling();
    }
  }
}

async function loadSelectedAnalysis() {
  if (!selectedAnalysisId.value) return;
  await loadAnalysis(selectedAnalysisId.value);
}

async function refreshAnalyses() {
  const data = await getAIAnalysesApi(articleId, { page: 1, pageSize: 20 });
  if (destroyed) return;
  analyses.value = data.items;
  const latest = data.items[0];
  if (latest) await loadAnalysis(latest.id);
}

function scheduleAnalysisPoll() {
  if (
    destroyed ||
    !analysisJob.value ||
    !shouldPoll(analysisJob.value.status)
  ) {
    return;
  }
  const epoch = analysisPollEpoch;
  const jobId = analysisJob.value.id;
  analysisTimer = setTimeout(async () => {
    analysisTimer = undefined;
    try {
      const current = await getAIJobApi(jobId);
      if (destroyed || epoch !== analysisPollEpoch) return;
      analysisJob.value = current;
      if (current.status === 'completed') {
        await refreshAnalyses();
      }
    } catch {
      // 请求层已提供统一的安全错误提示；活动任务保持轮询。
    } finally {
      if (
        !destroyed &&
        epoch === analysisPollEpoch &&
        analysisJob.value &&
        shouldPoll(analysisJob.value.status)
      ) {
        scheduleAnalysisPoll();
      }
    }
  }, POLL_INTERVAL);
}

async function acceptAnalysisJob(value: AIJob) {
  stopAnalysisPolling();
  analysisJob.value = value;
  if (shouldPoll(value.status)) {
    scheduleAnalysisPoll();
  } else if (value.status === 'completed') {
    await refreshAnalyses();
  }
}

async function startAnalysis(force: boolean) {
  if (
    analysisSubmitting.value ||
    analysisPolling.value ||
    !validArticleId ||
    !aiStatus.value.enabled ||
    article.value?.status !== 'ready'
  ) {
    return;
  }
  if (force) {
    try {
      await ElMessageBox.confirm(
        '重新分析会创建一份新的资料包，历史分析仍会保留。',
        '确认重新分析',
        { confirmButtonText: '重新分析', type: 'warning' },
      );
    } catch {
      return;
    }
  }

  const epoch = ++analysisActionEpoch;
  analysisSubmitting.value = true;
  analysisRequestError.value = '';
  try {
    const result = await startAIAnalysisApi(articleId, force);
    if (destroyed || epoch !== analysisActionEpoch) return;
    await acceptAnalysisJob(result.job);
  } catch {
    if (!destroyed && epoch === analysisActionEpoch) {
      analysisRequestError.value = '分析请求未成功，请检查网络后重试';
    }
  } finally {
    if (!destroyed && epoch === analysisActionEpoch) {
      analysisSubmitting.value = false;
    }
  }
}

async function retryAnalysisJob() {
  if (
    analysisSubmitting.value ||
    !analysisJob.value ||
    !canRetry(analysisJob.value)
  ) {
    return;
  }
  const epoch = ++analysisActionEpoch;
  analysisSubmitting.value = true;
  analysisRequestError.value = '';
  try {
    const nextJob = await retryAIJobApi(analysisJob.value.id);
    if (destroyed || epoch !== analysisActionEpoch) return;
    await acceptAnalysisJob(nextJob);
    ElMessage.success('分析任务已重新进入队列');
  } catch {
    if (!destroyed && epoch === analysisActionEpoch) {
      analysisRequestError.value = '分析任务重试失败，请稍后再试';
    }
  } finally {
    if (!destroyed && epoch === analysisActionEpoch) {
      analysisSubmitting.value = false;
    }
  }
}

function attachGenerationJob(value: AIGeneration, valueJob: AIJob) {
  return { ...value, job: valueJob, jobId: valueJob.id };
}

function scheduleGenerationPoll() {
  if (
    destroyed ||
    !generation.value ||
    !generation.value.job ||
    !shouldPoll(generation.value.job.status)
  ) {
    return;
  }
  const epoch = generationPollEpoch;
  const generationId = generation.value.id;
  generationTimer = setTimeout(async () => {
    generationTimer = undefined;
    try {
      const current = await getAIGenerationApi(generationId);
      if (destroyed || epoch !== generationPollEpoch) return;
      generation.value = current;
    } catch {
      // 请求层已提供统一的安全错误提示；活动任务保持轮询。
    } finally {
      if (
        !destroyed &&
        epoch === generationPollEpoch &&
        generation.value?.job &&
        shouldPoll(generation.value.job.status)
      ) {
        scheduleGenerationPoll();
      }
    }
  }, POLL_INTERVAL);
}

function acceptGeneration(value: AIGeneration, valueJob: AIJob) {
  stopGenerationPolling();
  generation.value = attachGenerationJob(value, valueJob);
  if (shouldPoll(valueJob.status)) scheduleGenerationPoll();
}

async function submitGeneration() {
  if (
    generationSubmitting.value ||
    generationPolling.value ||
    analysisSelecting.value ||
    !selectedAnalysis.value ||
    !generationAllowed.value
  ) {
    const error = validateGenerationForm(generationInputForValidation.value)[0];
    if (error) ElMessage.warning(error.message);
    return;
  }

  const fingerprint = generationFingerprint();
  if (!pendingGenerationKey || pendingGenerationFingerprint !== fingerprint) {
    pendingGenerationKey = crypto.randomUUID();
    pendingGenerationFingerprint = fingerprint;
  }
  const input: GenerationInput = {
    ...generationForm,
    idempotencyKey: pendingGenerationKey,
  };
  const errors = validateGenerationForm(input);
  if (errors[0]) {
    ElMessage.warning(errors[0].message);
    return;
  }

  const epoch = ++generationActionEpoch;
  generationSubmitting.value = true;
  generationRequestError.value = '';
  try {
    const result = await startAIGenerationApi(selectedAnalysis.value.id, input);
    if (destroyed || epoch !== generationActionEpoch) return;
    pendingGenerationKey = '';
    pendingGenerationFingerprint = '';
    acceptGeneration(result.generation, result.job);
  } catch {
    if (!destroyed && epoch === generationActionEpoch) {
      generationRequestError.value = '生成请求未成功，请检查网络后重试';
    }
  } finally {
    if (!destroyed && epoch === generationActionEpoch) {
      generationSubmitting.value = false;
    }
  }
}

async function retryGenerationJob() {
  const currentJob = generation.value?.job;
  if (generationSubmitting.value || !currentJob || !canRetry(currentJob))
    return;

  const epoch = ++generationActionEpoch;
  generationSubmitting.value = true;
  generationRequestError.value = '';
  try {
    const nextJob = await retryAIJobApi(currentJob.id);
    if (destroyed || epoch !== generationActionEpoch || !generation.value) {
      return;
    }
    acceptGeneration(generation.value, nextJob);
    ElMessage.success('生成任务已重新进入队列');
  } catch {
    if (!destroyed && epoch === generationActionEpoch) {
      generationRequestError.value = '生成任务重试失败，请稍后再试';
    }
  } finally {
    if (!destroyed && epoch === generationActionEpoch) {
      generationSubmitting.value = false;
    }
  }
}

async function openDraft() {
  if (!generation.value?.draftId) return;
  await router.push(`/drafts/${generation.value.draftId}`);
}

async function load() {
  if (!validArticleId) {
    loadError.value = '文章 ID 无效';
    loading.value = false;
    return;
  }
  const epoch = ++loadEpoch;
  loading.value = true;
  try {
    const [articleData, status, history] = await Promise.all([
      getArticleApi(articleId),
      getAIStatusApi(),
      getAIAnalysesApi(articleId, { page: 1, pageSize: 20 }),
    ]);
    if (destroyed || epoch !== loadEpoch) return;
    article.value = articleData;
    aiStatus.value = status;
    analyses.value = history.items;
    const latest = history.items[0];
    if (latest) await loadAnalysis(latest.id);
  } catch {
    if (!destroyed && epoch === loadEpoch) {
      loadError.value = 'AI 采编工作台加载失败，请稍后重试';
    }
  } finally {
    if (!destroyed && epoch === loadEpoch) loading.value = false;
  }
}

onMounted(load);
onBeforeUnmount(() => {
  destroyed = true;
  loadEpoch += 1;
  selectionEpoch += 1;
  analysisActionEpoch += 1;
  generationActionEpoch += 1;
  stopAnalysisPolling();
  stopGenerationPolling();
});
</script>

<template>
  <div class="space-y-4 p-5">
    <ElAlert
      :closable="false"
      title="AI 结果不会自动审核或发布，需人工检查"
      type="warning"
    />
    <ElSkeleton v-if="loading" :rows="12" animated />
    <ElEmpty v-else-if="loadError" :description="loadError" />
    <template v-else-if="article">
      <ElCard shadow="never">
        <div
          class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between"
        >
          <div>
            <div class="mb-2 flex flex-wrap items-center gap-2">
              <ElTag effect="plain">文章 #{{ article.id }}</ElTag>
              <ElTag :type="aiStatus.enabled ? 'success' : 'warning'">
                {{ aiStatus.enabled ? 'AI 已启用' : 'AI 未配置' }}
              </ElTag>
              <span
                v-if="aiStatus.enabled"
                class="text-muted-foreground text-sm"
              >
                {{ aiStatus.provider }} / {{ aiStatus.model }}
              </span>
            </div>
            <h1 class="text-2xl font-semibold">{{ article.title }}</h1>
            <p class="text-muted-foreground mt-2">
              先核对资料包，再选择角度生成待审核稿件。
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <ElButton @click="router.push(`/articles/${article.id}`)">
              返回文章
            </ElButton>
            <ElButton
              :disabled="
                analysisSubmitting ||
                analysisPolling ||
                article.status !== 'ready' ||
                !aiStatus.enabled
              "
              :loading="analysisSubmitting || analysisPolling"
              :type="selectedAnalysis ? 'warning' : 'primary'"
              @click="startAnalysis(Boolean(selectedAnalysis))"
            >
              {{ selectedAnalysis ? '重新分析' : '开始分析' }}
            </ElButton>
          </div>
        </div>
      </ElCard>

      <ElAlert
        v-if="article.status !== 'ready'"
        :closable="false"
        title="文章解析完成后可分析"
        type="warning"
      />
      <ElAlert
        v-else-if="!aiStatus.enabled"
        :closable="false"
        title="AI 服务尚未配置"
        type="warning"
      />
      <ElAlert
        v-if="analysisRequestError"
        :closable="false"
        :title="analysisRequestError"
        type="error"
      />
      <ElCard v-if="analysisJob" shadow="never">
        <template #header>
          <div class="flex items-center justify-between gap-3">
            <strong>分析任务 #{{ analysisJob.id }}</strong>
            <ElTag :type="jobTagType(analysisJob.status)">
              {{ AI_JOB_STATUS_LABELS[analysisJob.status] }}
            </ElTag>
          </div>
        </template>
        <ElAlert
          v-if="analysisJob.status === 'failed'"
          :closable="false"
          :description="safeJobMessage(analysisJob)"
          :title="safeJobCode(analysisJob)"
          type="error"
        />
        <ElButton
          v-if="canRetry(analysisJob)"
          class="mt-3"
          :loading="analysisSubmitting"
          type="warning"
          @click="retryAnalysisJob"
        >
          重试分析任务
        </ElButton>
      </ElCard>

      <ElEmpty
        v-if="!selectedAnalysis && !analysisJob"
        description="尚无 AI 分析，请先开始分析"
      />

      <template v-if="selectedAnalysis">
        <ElCard shadow="never">
          <div class="flex flex-wrap items-center gap-3">
            <strong>历史分析</strong>
            <ElSelect
              v-model="selectedAnalysisId"
              class="w-72"
              :loading="analysisSelecting"
              @change="loadSelectedAnalysis"
            >
              <ElOption
                v-for="item in analyses"
                :key="item.id"
                :label="`分析 #${item.id} · ${item.createdAt}`"
                :value="item.id"
              />
            </ElSelect>
          </div>
        </ElCard>

        <div class="grid gap-4 xl:grid-cols-2">
          <ElCard shadow="never">
            <template #header><strong>摘要</strong></template>
            <p class="whitespace-pre-wrap">{{ selectedAnalysis.summary }}</p>
          </ElCard>

          <ElCard shadow="never">
            <template #header><strong>事实</strong></template>
            <ul class="space-y-3">
              <li v-for="fact in selectedAnalysis.facts" :key="fact.id">
                <div>{{ fact.text }}</div>
                <div class="mt-1 flex flex-wrap gap-1">
                  <ElTag size="small" type="info">{{ fact.id }}</ElTag>
                  <ElTag
                    v-for="blockId in fact.sourceBlockIds"
                    :key="blockId"
                    effect="plain"
                    size="small"
                  >
                    {{ blockId }}
                  </ElTag>
                </div>
              </li>
            </ul>
          </ElCard>

          <ElCard shadow="never">
            <template #header><strong>观点</strong></template>
            <ul class="space-y-3">
              <li
                v-for="viewpoint in selectedAnalysis.viewpoints"
                :key="viewpoint.id"
              >
                <strong>{{ viewpoint.holder }}</strong>
                <p>{{ viewpoint.text }}</p>
                <div class="mt-1 flex flex-wrap gap-1">
                  <ElTag size="small" type="info">
                    {{ viewpoint.id }}
                  </ElTag>
                  <ElTag
                    v-for="blockId in viewpoint.sourceBlockIds"
                    :key="blockId"
                    effect="plain"
                    size="small"
                  >
                    {{ blockId }}
                  </ElTag>
                </div>
              </li>
            </ul>
          </ElCard>

          <ElCard shadow="never">
            <template #header><strong>引用</strong></template>
            <blockquote
              v-for="quote in selectedAnalysis.quotes"
              :key="quote.id"
              class="mb-3 border-l-4 border-gray-300 py-2 pl-4"
            >
              <p>{{ quote.text }}</p>
              <footer class="text-muted-foreground mt-2 text-sm">
                {{ quote.id }} · {{ quote.sourceBlockId }}
              </footer>
            </blockquote>
          </ElCard>

          <ElCard shadow="never">
            <template #header><strong>风险</strong></template>
            <p v-if="selectedAnalysis.risks.length === 0">未识别到额外风险</p>
            <ul v-else class="space-y-3">
              <li v-for="risk in selectedAnalysis.risks" :key="risk.id">
                <div>{{ risk.text }}</div>
                <div class="mt-1 flex flex-wrap gap-1">
                  <ElTag size="small" type="info">{{ risk.id }}</ElTag>
                  <ElTag
                    v-for="blockId in risk.sourceBlockIds"
                    :key="blockId"
                    effect="plain"
                    size="small"
                  >
                    {{ blockId }}
                  </ElTag>
                </div>
              </li>
            </ul>
          </ElCard>

          <ElCard shadow="never">
            <template #header><strong>角度（3 个）</strong></template>
            <ElRadioGroup
              v-model="generationForm.angleId"
              class="w-full"
              :disabled="analysisSelecting"
            >
              <div class="grid w-full gap-3">
                <ElCard
                  v-for="angle in analysisAngles"
                  :key="angle.id"
                  shadow="never"
                >
                  <ElRadio :value="angle.id">
                    <strong>{{ angle.title }}</strong>
                  </ElRadio>
                  <p class="mt-2">{{ angle.thesis }}</p>
                  <ol class="mt-2 list-decimal pl-5 text-sm">
                    <li v-for="item in angle.outline" :key="item">
                      {{ item }}
                    </li>
                  </ol>
                </ElCard>
              </div>
            </ElRadioGroup>
          </ElCard>
        </div>

        <ElCard shadow="never">
          <template #header><strong>生成稿件</strong></template>
          <ElForm label-position="top">
            <div class="grid gap-3 md:grid-cols-3">
              <ElFormItem label="目标读者">
                <ElInput
                  v-model="generationForm.audience"
                  :disabled="generationSubmitting || analysisSelecting"
                  maxlength="100"
                  placeholder="例如：产品经理"
                  show-word-limit
                />
              </ElFormItem>
              <ElFormItem label="语气">
                <ElSelect
                  v-model="generationForm.tone"
                  :disabled="generationSubmitting || analysisSelecting"
                  class="w-full"
                >
                  <ElOption
                    v-for="option in toneOptions"
                    :key="option.value"
                    :label="option.label"
                    :value="option.value"
                  />
                </ElSelect>
              </ElFormItem>
              <ElFormItem label="目标字数">
                <ElInputNumber
                  v-model="generationForm.targetWords"
                  :disabled="generationSubmitting || analysisSelecting"
                  :max="5000"
                  :min="300"
                  :step="100"
                  class="w-full"
                />
              </ElFormItem>
            </div>
            <ElFormItem label="补充要求">
              <ElInput
                v-model="generationForm.additionalInstructions"
                :disabled="generationSubmitting || analysisSelecting"
                maxlength="500"
                placeholder="可选：补充重点、禁用表达或结构要求"
                :rows="4"
                show-word-limit
                type="textarea"
              />
            </ElFormItem>
            <ElAlert
              v-if="generationRequestError"
              class="mb-3"
              :closable="false"
              :title="generationRequestError"
              type="error"
            />
            <ElButton
              :disabled="
                generationSubmitting ||
                generationPolling ||
                analysisSelecting ||
                !generationAllowed
              "
              :loading="generationSubmitting || generationPolling"
              type="primary"
              @click="submitGeneration"
            >
              {{ generationButtonLabel }}
            </ElButton>
          </ElForm>
        </ElCard>
      </template>

      <ElCard v-if="generation" shadow="never">
        <template #header>
          <div class="flex flex-wrap items-center justify-between gap-3">
            <strong>生成结果</strong>
            <ElTag
              v-if="generationJob"
              :type="jobTagType(generationJob.status)"
            >
              {{ AI_JOB_STATUS_LABELS[generationJob.status] }}
            </ElTag>
          </div>
        </template>

        <ElAlert
          v-if="generationJob?.status === 'failed'"
          class="mb-4"
          :closable="false"
          :description="safeJobMessage(generationJob)"
          :title="safeJobCode(generationJob)"
          type="error"
        />
        <ElButton
          v-if="generationJob && canRetry(generationJob)"
          class="mb-4"
          :loading="generationSubmitting"
          type="warning"
          @click="retryGenerationJob"
        >
          重试当前生成任务
        </ElButton>

        <template v-if="generationJob?.status === 'completed'">
          <h2 class="text-2xl font-semibold">{{ generation.title }}</h2>
          <p class="text-muted-foreground mt-2">{{ generation.digest }}</p>

          <article class="generated-blocks mx-auto mt-6 max-w-4xl">
            <template v-for="(block, index) in generation.blocks" :key="index">
              <component
                :is="headingTag(block.level)"
                v-if="block.type === 'heading'"
              >
                {{ block.text }}
              </component>
              <p v-else-if="block.type === 'paragraph'">{{ block.text }}</p>
              <blockquote
                v-else-if="block.type === 'quote'"
                class="border-l-4 border-gray-300 py-2 pl-4"
              >
                {{ block.text }}
              </blockquote>
              <ul v-else-if="block.type === 'list'" class="list-disc pl-5">
                <li v-for="item in block.items" :key="item">{{ item }}</li>
              </ul>
              <figure
                v-else-if="
                  block.type === 'image' &&
                  block.assetId &&
                  assetURLs.get(block.assetId)
                "
              >
                <img
                  :alt="block.alt || ''"
                  :src="assetURLs.get(block.assetId)"
                  loading="lazy"
                />
                <figcaption v-if="block.alt">{{ block.alt }}</figcaption>
              </figure>
            </template>
          </article>

          <div
            v-if="generationJob"
            class="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-4"
          >
            <div>Input Token：{{ generationJob.inputTokens }}</div>
            <div>Output Token：{{ generationJob.outputTokens }}</div>
            <div>Total Token：{{ generationJob.totalTokens }}</div>
            <div>耗时：{{ formatDuration(generationJob) }}</div>
          </div>
          <ElButton
            v-if="generation.draftId"
            class="mt-5"
            type="primary"
            @click="openDraft"
          >
            打开微信稿件
          </ElButton>
        </template>
      </ElCard>
    </template>
  </div>
</template>
