<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { PublishJobStatus, WeChatPublishJob, WeChatStatus } from '#/api';

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
  getWeChatPublishJobApi,
  getWeChatPublishJobsApi,
  getWeChatStatusApi,
  retryWeChatPublishJobApi,
} from '#/api';

defineOptions({ name: 'WeChatPublishJobs' });
const route = useRoute();
const router = useRouter();
const jobs = ref<WeChatPublishJob[]>([]);
const selected = ref<WeChatPublishJob>();
const wechat = ref<WeChatStatus>({ appConfigured: false, enabled: false });
const loading = ref(false);
const drawer = ref(false);
const query = reactive<{ status: '' | PublishJobStatus }>({ status: '' });
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
let timer: ReturnType<typeof setInterval> | undefined;
const running = new Set<PublishJobStatus>(['publishing', 'queued']);
const labels: Record<PublishJobStatus, string> = {
  queued: '排队中',
  publishing: '上传并创建草稿',
  completed: '已写入草稿箱',
  failed: '失败',
};

function tagType(status: PublishJobStatus) {
  if (status === 'failed') return 'danger';
  if (status === 'completed') return 'success';
  return 'primary';
}
async function load() {
  loading.value = true;
  try {
    const [data, status] = await Promise.all([
      getWeChatPublishJobsApi({
        page: pagination.page,
        pageSize: pagination.pageSize,
        status: query.status || undefined,
      }),
      getWeChatStatusApi(),
    ]);
    jobs.value = data.items;
    pagination.total = data.total;
    wechat.value = status;
  } finally {
    loading.value = false;
  }
}
async function show(job: WeChatPublishJob) {
  selected.value = await getWeChatPublishJobApi(job.id);
  drawer.value = true;
}
async function retry(job: WeChatPublishJob) {
  await retryWeChatPublishJobApi(job.id);
  ElMessage.success('发布任务已重新进入队列');
  await load();
  selected.value = await getWeChatPublishJobApi(job.id);
}
function changePage(page: number) {
  pagination.page = page;
  void load();
}

onMounted(async () => {
  await load();
  const jobID = Number(route.query.job);
  if (jobID) {
    const item = jobs.value.find((job) => job.id === jobID);
    if (item) await show(item);
  }
  timer = setInterval(() => {
    if (jobs.value.some((job) => running.has(job.status))) void load();
  }, 8000);
});
onBeforeUnmount(() => {
  if (timer) clearInterval(timer);
});
</script>

<template>
  <div class="wp-page">
    <ElCard class="wp-page-hero" shadow="never">
      <div class="wp-page-hero__content">
        <div>
          <p class="wp-page-eyebrow">WECHAT PUBLISHING</p>
          <h1 class="wp-page-title">微信发布任务</h1>
          <p class="wp-page-description">
            查看素材上传和写入微信公众号草稿箱的执行结果；系统不会自动群发。
          </p>
        </div>
        <ElButton
          class="wp-page-action"
          size="large"
          type="primary"
          @click="router.push('/drafts')"
          >查看稿件</ElButton
        >
      </div>
    </ElCard>
    <ElAlert
      v-if="!wechat.enabled"
      class="wp-inline-alert"
      :closable="false"
      title="微信公众号发布未启用，请在服务端配置 AppID 和 AppSecret。"
      type="warning"
    />
    <ElCard class="wp-panel wp-filter-panel" shadow="never">
      <div class="wp-filter-bar">
        <span class="wp-filter-bar__label">筛选条件</span>
        <div class="wp-filter-bar__controls">
          <ElSelect
            v-model="query.status"
            clearable
            class="w-56"
            placeholder="全部状态"
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
        </div>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-table-panel" shadow="never">
      <div class="wp-table-panel__header">
        <div>
          <p class="wp-panel-title">发布记录</p>
          <p class="wp-panel-description">查看草稿写入状态与公众号返回结果</p>
        </div>
        <span class="wp-record-count">共 {{ pagination.total }} 条记录</span>
      </div>
      <ElTable
        v-loading="loading"
        class="wp-data-table"
        :data="jobs"
        row-key="id"
      >
        <ElTableColumn label="任务" width="90" prop="id" />
        <ElTableColumn label="稿件" min-width="280">
          <template #default="{ row }">
            {{ row.draft?.title || `稿件 #${row.draftId}` }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="170">
          <template #default="{ row }">
            <ElTag :type="tagType(row.status)">{{
              labels[row.status as PublishJobStatus]
            }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="执行次数" width="110" prop="attempts" />
        <ElTableColumn label="创建时间" width="180">
          <template #default="{ row }">{{
            dayjs(row.createdAt).format('YYYY-MM-DD HH:mm')
          }}</template>
        </ElTableColumn>
        <ElTableColumn align="right" label="操作" width="180">
          <template #default="{ row }">
            <ElButton link type="primary" @click="show(row as WeChatPublishJob)"
              >详情</ElButton
            >
            <ElButton
              v-if="row.status === 'failed'"
              v-access:code="'wechat-publish:retry'"
              link
              type="warning"
              @click="retry(row as WeChatPublishJob)"
              >重试</ElButton
            >
          </template>
        </ElTableColumn>
        <template #empty><ElEmpty description="暂无微信发布任务" /></template>
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
      size="min(560px, 92vw)"
      title="微信发布任务详情"
    >
      <template v-if="selected">
        <div class="mb-5 flex items-center gap-3">
          <ElTag :type="tagType(selected.status)">{{
            labels[selected.status]
          }}</ElTag>
          <span class="text-muted-foreground">任务 #{{ selected.id }}</span>
        </div>
        <ElAlert
          v-if="selected.errorMessage"
          class="mb-4"
          :closable="false"
          :description="selected.errorMessage"
          :title="selected.errorCode || '发布失败'"
          type="error"
        />
        <ElAlert
          v-if="selected.remoteMediaId"
          class="mb-4"
          :closable="false"
          :description="selected.remoteMediaId"
          title="微信公众号草稿 media_id"
          type="success"
        />
        <ElTimeline>
          <ElTimelineItem
            v-for="event in selected.events"
            :key="event.id"
            :timestamp="dayjs(event.createdAt).format('MM-DD HH:mm:ss')"
            placement="top"
          >
            <strong>{{ labels[event.status] || event.status }}</strong>
            <p class="text-muted-foreground mt-1">{{ event.message }}</p>
          </ElTimelineItem>
        </ElTimeline>
        <div class="mt-6 flex gap-2">
          <ElButton
            v-if="selected.status === 'failed'"
            v-access:code="'wechat-publish:retry'"
            type="warning"
            @click="retry(selected)"
            >重新发布</ElButton
          >
          <ElButton
            type="primary"
            @click="router.push(`/drafts/${selected.draftId}`)"
            >查看稿件</ElButton
          >
        </div>
      </template>
    </ElDrawer>
  </div>
</template>
