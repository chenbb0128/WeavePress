<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { CollectionJob, JobStatus } from '#/api';

import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import dayjs from 'dayjs';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElDrawer,
  ElEmpty,
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

import {
  getCollectionJobApi,
  getCollectionJobsApi,
  retryCollectionJobApi,
} from '#/api';

defineOptions({ name: 'CollectionJobs' });
const route = useRoute();
const router = useRouter();
const jobs = ref<CollectionJob[]>([]);
const selected = ref<CollectionJob>();
const loading = ref(false);
const drawer = ref(false);
const query = reactive<{ status: '' | JobStatus }>({ status: '' });
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
let timer: ReturnType<typeof setInterval> | undefined;
const running = new Set<JobStatus>([
  'fetching',
  'parsing',
  'queued',
  'storing_assets',
]);
const labels: Record<JobStatus, string> = {
  queued: '排队中',
  fetching: '获取页面',
  parsing: '解析正文',
  storing_assets: '归档素材',
  completed: '已完成',
  completed_with_warnings: '完成（有警告）',
  failed: '失败',
};

function tagType(status: JobStatus) {
  if (status === 'failed') return 'danger';
  if (status === 'completed') return 'success';
  if (status === 'completed_with_warnings') return 'warning';
  return 'primary';
}
function statusLabel(value: unknown) {
  return labels[value as JobStatus] || String(value);
}
async function load() {
  loading.value = true;
  try {
    const data = await getCollectionJobsApi({
      page: pagination.page,
      pageSize: pagination.pageSize,
      status: query.status || undefined,
    });
    jobs.value = data.items;
    pagination.total = data.total;
  } finally {
    loading.value = false;
  }
}
async function show(job: CollectionJob) {
  selected.value = await getCollectionJobApi(job.id);
  drawer.value = true;
}
async function retry(job: CollectionJob) {
  await retryCollectionJobApi(job.id);
  ElMessage.success('任务已重新进入队列');
  await load();
  selected.value = await getCollectionJobApi(job.id);
}
function changePage(page: number) {
  pagination.page = page;
  void load();
}

onMounted(async () => {
  await load();
  const jobId = Number(route.query.job);
  if (jobId) {
    const item = jobs.value.find((job) => job.id === jobId);
    if (item) await show(item);
  }
  timer = setInterval(() => {
    if (jobs.value.some((job) => running.has(job.status))) void load();
  }, 10_000);
});
onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
});
</script>

<template>
  <div class="space-y-4 p-5">
    <ElCard shadow="never">
      <div
        class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <h1 class="text-2xl font-semibold">采集任务</h1>
          <p class="text-muted-foreground mt-2">
            查看每篇文章的采集阶段、告警和失败原因。
          </p>
        </div>
        <ElButton type="primary" @click="router.push('/collection/new')">
          提交新链接
        </ElButton>
      </div>
    </ElCard>
    <ElCard shadow="never">
      <ElSelect
        v-model="query.status"
        clearable
        placeholder="全部状态"
        class="w-52"
        @change="
          pagination.page = 1;
          load();
        "
      >
        <ElOption
          v-for="(label, value) in labels"
          :key="value"
          :label="label"
          :value="value"
        />
      </ElSelect>
    </ElCard>
    <ElCard shadow="never">
      <ElTable v-loading="loading" :data="jobs" row-key="id">
        <ElTableColumn label="任务" width="100" prop="id" /><ElTableColumn
          label="文章"
          min-width="260"
        >
          <template #default="{ row }">
            {{
              row.article?.title ||
              row.article?.canonicalUrl ||
              `文章 #${row.articleId}`
            }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="160">
          <template #default="{ row }">
            <ElTag :type="tagType(row.status)">
              {{ statusLabel(row.status) }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn
          label="执行次数"
          width="110"
          prop="attempts"
        /><ElTableColumn label="创建时间" width="180">
          <template #default="{ row }">
            {{ dayjs(row.createdAt).format('YYYY-MM-DD HH:mm') }}
          </template>
        </ElTableColumn>
        <ElTableColumn align="right" label="操作" width="180">
          <template #default="{ row }">
            <ElButton link type="primary" @click="show(row as CollectionJob)">
              详情 </ElButton
            ><ElButton
              v-if="row.status === 'failed'"
              link
              type="warning"
              @click="retry(row as CollectionJob)"
            >
              重试
            </ElButton>
          </template>
        </ElTableColumn>
        <template #empty><ElEmpty description="暂无采集任务" /></template>
      </ElTable>
      <div class="mt-4 flex justify-end">
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
    <ElDrawer v-model="drawer" size="min(560px, 92vw)" title="任务详情">
      <template v-if="selected">
        <div class="mb-5 flex items-center gap-3">
          <ElTag :type="tagType(selected.status)">
            {{ labels[selected.status] }} </ElTag
          ><span class="text-muted-foreground">任务 #{{ selected.id }}</span>
        </div>
        <ElAlert
          v-if="selected.errorMessage"
          class="mb-4"
          :closable="false"
          :description="selected.errorMessage"
          :title="selected.errorCode || '任务失败'"
          type="error"
        />
        <ElAlert
          v-for="warning in selected.warnings"
          :key="warning"
          class="mb-3"
          :closable="false"
          :title="warning"
          type="warning"
        />
        <ElTimeline>
          <ElTimelineItem
            v-for="event in selected.events"
            :key="event.id"
            :timestamp="dayjs(event.createdAt).format('MM-DD HH:mm:ss')"
            placement="top"
          >
            <strong>{{ labels[event.status] || event.status }}</strong>
            <p class="text-muted-foreground mt-1">
              {{ event.message }}
            </p>
          </ElTimelineItem>
        </ElTimeline>
        <div class="mt-6 flex gap-2">
          <ElButton
            v-if="selected.status === 'failed'"
            type="warning"
            @click="retry(selected)"
          >
            重新采集 </ElButton
          ><ElButton
            v-if="
              selected.status === 'completed' ||
              selected.status === 'completed_with_warnings'
            "
            type="primary"
            @click="router.push(`/articles/${selected.articleId}`)"
          >
            查看文章
          </ElButton>
        </div>
      </template>
    </ElDrawer>
  </div>
</template>
