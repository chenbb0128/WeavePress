<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { AIJob, AIJobStatus, AIJobType } from '#/api';

import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';

import { useAccess } from '@vben/access';

import dayjs from 'dayjs';
import {
  ElButton,
  ElCard,
  ElDescriptions,
  ElDescriptionsItem,
  ElDrawer,
  ElEmpty,
  ElInput,
  ElMessage,
  ElOption,
  ElPagination,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
  ElTimeline,
  ElTimelineItem,
} from 'element-plus';

import { getAIJobApi, getAIJobsApi, retryAIJobApi } from '#/api';

import {
  AI_JOB_STATUS_LABELS,
  AI_JOB_TYPE_LABELS,
  canRetry,
  shouldPoll,
} from './model';

defineOptions({ name: 'AIJobs' });

const POLL_INTERVAL = 8000;
const RETRY_PERMISSION = 'ai:job:retry';
const router = useRouter();
const { hasAccessByCodes } = useAccess();
const jobs = ref<AIJob[]>([]);
const selected = ref<AIJob>();
const loading = ref(false);
const drawer = ref(false);
const detailLoadingId = ref<number>();
const retryingIds = ref<Set<number>>(new Set());
const query = reactive<{
  articleId: string;
  status: '' | AIJobStatus;
  type: '' | AIJobType;
}>({ articleId: '', status: '', type: '' });
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });

let destroyed = false;
let loadEpoch = 0;
let detailEpoch = 0;
let requestInFlight = false;
let reloadQueued = false;
let timer: ReturnType<typeof setTimeout> | undefined;

function jobTagType(status: AIJobStatus) {
  if (status === 'completed') return 'success';
  if (status === 'failed') return 'danger';
  return 'primary';
}

function safeJobCode(job: AIJob) {
  const code = job.errorCode?.trim() ?? '';
  if (/^[A-Z][A-Z0-9_]{0,79}$/.test(code)) return code;
  return job.status === 'failed' ? 'AI_TASK_FAILED' : '-';
}

function safeMessage(message: string | undefined, fallback: string) {
  const value = message?.trim() ?? '';
  const sensitive =
    /(api[\s_-]*key|prompt|raw\s+output|full\s+output|fingerprint|sk-[a-z\d]|原始输出|完整输出|提示词|内部指纹)/i;
  if (!value || sensitive.test(value)) return fallback;
  return [...value].slice(0, 240).join('');
}

function safeJobMessage(job: AIJob) {
  const fallback =
    job.status === 'failed' ? 'AI 任务处理失败，请稍后重试' : '-';
  return safeMessage(job.errorMessage, fallback);
}

function safeEventMessage(message: string) {
  return safeMessage(message, '任务状态已更新');
}

function formatDuration(job: AIJob) {
  if (!job.startedAt) return '-';
  const end =
    job.finishedAt ??
    (shouldPoll(job.status) ? new Date().toISOString() : job.updatedAt);
  const milliseconds = Date.parse(end) - Date.parse(job.startedAt);
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-';
  if (milliseconds < 60_000) return `${(milliseconds / 1000).toFixed(1)} 秒`;
  if (milliseconds < 3_600_000) {
    return `${(milliseconds / 60_000).toFixed(1)} 分钟`;
  }
  return `${(milliseconds / 3_600_000).toFixed(1)} 小时`;
}

function articleIdParam() {
  const value = query.articleId.trim();
  if (!value) return undefined;
  const articleId = Number(value);
  return Number.isSafeInteger(articleId) && articleId > 0
    ? articleId
    : undefined;
}

function articleIdIsValid() {
  return !query.articleId.trim() || articleIdParam() !== undefined;
}

function canRetryJob(job: AIJob) {
  return canRetry(job) && hasAccessByCodes([RETRY_PERMISSION]);
}

function setRetrying(id: number, retrying: boolean) {
  const next = new Set(retryingIds.value);
  if (retrying) next.add(id);
  else next.delete(id);
  retryingIds.value = next;
}

function clearPoll() {
  if (timer) clearTimeout(timer);
  timer = undefined;
}

function schedulePoll() {
  clearPoll();
  if (destroyed || !jobs.value.some((job) => shouldPoll(job.status))) return;
  timer = setTimeout(() => {
    timer = undefined;
    void load();
  }, POLL_INTERVAL);
}

async function load() {
  const epoch = ++loadEpoch;
  if (requestInFlight) {
    reloadQueued = true;
    return;
  }

  clearPoll();
  requestInFlight = true;
  loading.value = true;
  try {
    const articleId = articleIdParam();
    const data = await getAIJobsApi({
      ...(articleId === undefined ? {} : { articleId }),
      page: pagination.page,
      pageSize: pagination.pageSize,
      ...(query.status ? { status: query.status } : {}),
      ...(query.type ? { type: query.type } : {}),
    });
    if (destroyed || epoch !== loadEpoch) return;
    jobs.value = data.items;
    pagination.total = data.total;
  } catch {
    if (!destroyed && epoch === loadEpoch) {
      ElMessage.error('AI 任务加载失败，请稍后重试');
    }
  } finally {
    requestInFlight = false;
    if (!destroyed) {
      if (reloadQueued || epoch !== loadEpoch) {
        reloadQueued = false;
        void load();
      } else {
        loading.value = false;
        schedulePoll();
      }
    }
  }
}

async function show(job: AIJob) {
  if (detailLoadingId.value !== undefined) return;
  const epoch = ++detailEpoch;
  detailLoadingId.value = job.id;
  try {
    const detail = await getAIJobApi(job.id);
    if (destroyed || epoch !== detailEpoch) return;
    selected.value = detail;
    drawer.value = true;
  } catch {
    if (!destroyed && epoch === detailEpoch) {
      ElMessage.error('AI 任务详情加载失败，请稍后重试');
    }
  } finally {
    if (!destroyed && epoch === detailEpoch) {
      detailLoadingId.value = undefined;
    }
  }
}

async function retry(job: AIJob) {
  if (!canRetryJob(job) || retryingIds.value.has(job.id)) return;
  setRetrying(job.id, true);

  let retried: AIJob;
  try {
    retried = await retryAIJobApi(job.id);
  } catch {
    if (!destroyed) ElMessage.error('AI 任务重试失败，请稍后再试');
    if (!destroyed) setRetrying(job.id, false);
    return;
  }

  if (destroyed) return;
  ElMessage.success('AI 任务已重新进入队列');
  await load();

  if (drawer.value && selected.value?.id === job.id) {
    try {
      const detail = await getAIJobApi(retried.id);
      if (!destroyed) selected.value = detail;
    } catch {
      if (!destroyed) ElMessage.warning('重试成功，但任务详情刷新失败');
    }
  }
  if (!destroyed) setRetrying(job.id, false);
}

function search() {
  if (!articleIdIsValid()) {
    ElMessage.warning('文章 ID 必须是正整数');
    return;
  }
  pagination.page = 1;
  void load();
}

function reset() {
  query.articleId = '';
  query.status = '';
  query.type = '';
  search();
}

function changePage(page: number) {
  pagination.page = page;
  void load();
}

function openResult(job: AIJob) {
  if (job.type === 'analysis') {
    void router.push(`/ai/articles/${job.articleId}`);
  } else if (job.draftId) {
    void router.push(`/drafts/${job.draftId}`);
  }
}

onMounted(load);
onBeforeUnmount(() => {
  destroyed = true;
  loadEpoch += 1;
  detailEpoch += 1;
  clearPoll();
});
</script>

<template>
  <div class="wp-page">
    <ElCard class="wp-page-hero" shadow="never">
      <div class="wp-page-hero__content">
        <div>
          <p class="wp-page-eyebrow">AI PROCESSING</p>
          <h1 class="wp-page-title">AI 任务</h1>
          <p class="wp-page-description">
            查看文章分析与稿件生成任务的执行状态、用量和失败原因。
          </p>
        </div>
        <ElTag effect="light" size="large" type="success">异步队列</ElTag>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-filter-panel" shadow="never">
      <div class="wp-filter-bar">
        <span class="wp-filter-bar__label">筛选条件</span>
        <div class="wp-filter-bar__controls">
          <ElSelect
            v-model="query.type"
            clearable
            class="w-48"
            placeholder="全部任务类型"
          >
            <ElOption
              v-for="(label, value) in AI_JOB_TYPE_LABELS"
              :key="value"
              :label="label"
              :value="value"
            />
          </ElSelect>
          <ElSelect
            v-model="query.status"
            clearable
            class="w-48"
            placeholder="全部状态"
          >
            <ElOption
              v-for="(label, value) in AI_JOB_STATUS_LABELS"
              :key="value"
              :label="label"
              :value="value"
            />
          </ElSelect>
          <ElInput
            v-model="query.articleId"
            clearable
            class="w-48"
            placeholder="文章 ID"
            @keyup.enter="search"
          />
          <ElButton type="primary" @click="search">查询</ElButton>
          <ElButton @click="reset">重置</ElButton>
        </div>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-table-panel" shadow="never">
      <div class="wp-table-panel__header">
        <div>
          <p class="wp-panel-title">任务列表</p>
          <p class="wp-panel-description">跟踪模型、Token 用量和任务执行结果</p>
        </div>
        <span class="wp-record-count">共 {{ pagination.total }} 条记录</span>
      </div>
      <ElTable
        v-loading="loading"
        class="wp-data-table"
        :data="jobs"
        row-key="id"
      >
        <ElTableColumn label="Job ID" width="90" prop="id" />
        <ElTableColumn label="来源文章" min-width="220">
          <template #default="{ row }">
            {{ row.article?.title || `文章 #${row.articleId}` }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="任务类型" width="110">
          <template #default="{ row }">
            {{ AI_JOB_TYPE_LABELS[row.type as AIJobType] || row.type }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="100">
          <template #default="{ row }">
            <ElTag :type="jobTagType(row.status)">
              {{
                AI_JOB_STATUS_LABELS[row.status as AIJobStatus] || row.status
              }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="Provider" min-width="120" prop="provider" />
        <ElTableColumn label="Model" min-width="140" prop="model" />
        <ElTableColumn label="Input Token" width="115">
          <template #default="{ row }">
            {{ row.inputTokens.toLocaleString() }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="Output Token" width="120">
          <template #default="{ row }">
            {{ row.outputTokens.toLocaleString() }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="Total Token" width="115">
          <template #default="{ row }">
            {{ row.totalTokens.toLocaleString() }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="执行次数" width="125">
          <template #default="{ row }">
            {{ row.attempts }} 次
            <span
              v-if="row.manualRetries"
              class="text-muted-foreground ml-1 text-xs"
            >
              手动 {{ row.manualRetries }}
            </span>
          </template>
        </ElTableColumn>
        <ElTableColumn label="耗时" width="100">
          <template #default="{ row }">
            {{ formatDuration(row as AIJob) }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="创建时间" width="180">
          <template #default="{ row }">
            {{ dayjs(row.createdAt).format('YYYY-MM-DD HH:mm') }}
          </template>
        </ElTableColumn>
        <ElTableColumn align="right" fixed="right" label="操作" width="150">
          <template #default="{ row }">
            <ElButton
              :disabled="
                detailLoadingId !== undefined && detailLoadingId !== row.id
              "
              :loading="detailLoadingId === row.id"
              link
              type="primary"
              @click="show(row as AIJob)"
            >
              详情
            </ElButton>
            <ElButton
              v-if="canRetryJob(row as AIJob)"
              :loading="retryingIds.has(row.id)"
              link
              type="warning"
              @click="retry(row as AIJob)"
            >
              重试
            </ElButton>
          </template>
        </ElTableColumn>
        <template #empty><ElEmpty description="暂无 AI 任务" /></template>
      </ElTable>
      <div class="wp-table-panel__footer">
        <ElPagination
          background
          :current-page="pagination.page"
          layout="total, prev, pager, next"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          @current-change="changePage"
        />
      </div>
    </ElCard>
    <ElDrawer
      v-model="drawer"
      class="wp-detail-drawer"
      size="min(640px, 92vw)"
      title="AI 任务详情"
    >
      <template v-if="selected">
        <div class="mb-5 flex items-center gap-3">
          <ElTag :type="jobTagType(selected.status)">
            {{ AI_JOB_STATUS_LABELS[selected.status] }}
          </ElTag>
          <span class="text-muted-foreground">任务 #{{ selected.id }}</span>
        </div>
        <ElDescriptions :column="1" border>
          <ElDescriptionsItem label="错误代码">
            {{ safeJobCode(selected) }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="错误信息">
            {{ safeJobMessage(selected) }}
          </ElDescriptionsItem>
          <ElDescriptionsItem label="可重试">
            <ElTag :type="selected.retryable ? 'success' : 'info'">
              {{ selected.retryable ? '是' : '否' }}
            </ElTag>
          </ElDescriptionsItem>
        </ElDescriptions>

        <h2 class="mb-4 mt-6 text-base font-semibold">执行事件</h2>
        <ElTimeline v-if="selected.events?.length">
          <ElTimelineItem
            v-for="event in selected.events"
            :key="event.id"
            :timestamp="dayjs(event.createdAt).format('MM-DD HH:mm:ss')"
            placement="top"
          >
            <strong>
              {{
                AI_JOB_STATUS_LABELS[event.status as AIJobStatus] ||
                event.status
              }}
            </strong>
            <p class="text-muted-foreground mt-1">
              {{ safeEventMessage(event.message) }}
            </p>
          </ElTimelineItem>
        </ElTimeline>
        <ElEmpty v-else description="暂无执行事件" :image-size="72" />

        <div class="mt-6 flex gap-2">
          <ElButton
            v-if="canRetryJob(selected)"
            :loading="retryingIds.has(selected.id)"
            type="warning"
            @click="retry(selected)"
          >
            重新执行
          </ElButton>
          <ElButton
            v-if="
              selected.type === 'analysis' && selected.status === 'completed'
            "
            type="primary"
            @click="openResult(selected)"
          >
            打开 AI 工作台
          </ElButton>
          <ElButton
            v-if="
              selected.type === 'generation' &&
              selected.status === 'completed' &&
              selected.draftId
            "
            type="primary"
            @click="openResult(selected)"
          >
            查看稿件
          </ElButton>
        </div>
      </template>
    </ElDrawer>
  </div>
</template>
