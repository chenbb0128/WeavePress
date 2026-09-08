<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline */
import type { DashboardData } from '#/api';

import { onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';

import {
  ElButton,
  ElCard,
  ElEmpty,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { getDashboardApi } from '#/api';

defineOptions({ name: 'Dashboard' });
const router = useRouter();
const loading = ref(false);
const data = ref<DashboardData>({
  articlesTotal: 0,
  collectedToday: 0,
  failedJobs: 0,
  processingJobs: 0,
  recentArticles: [],
  sourceWechat: 0,
  sourceWeb: 0,
});

onMounted(async () => {
  loading.value = true;
  try {
    data.value = await getDashboardApi();
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="space-y-4 p-5">
    <ElCard shadow="never">
      <div
        class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <h1 class="text-2xl font-semibold">内容采集工作台</h1>
          <p class="text-muted-foreground mt-2">
            统一管理微信公众号与普通网页内容。
          </p>
        </div>
        <ElButton type="primary" @click="router.push('/collection/new')">
          提交文章
        </ElButton>
      </div>
    </ElCard>
    <div v-loading="loading" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <ElCard shadow="hover">
        <p class="text-muted-foreground text-sm">文章总数</p>
        <p class="mt-2 text-3xl font-semibold">
          {{ data.articlesTotal }}
        </p>
      </ElCard>
      <ElCard shadow="hover">
        <p class="text-muted-foreground text-sm">今日采集</p>
        <p class="mt-2 text-3xl font-semibold">
          {{ data.collectedToday }}
        </p>
      </ElCard>
      <ElCard shadow="hover">
        <p class="text-muted-foreground text-sm">处理中</p>
        <p class="mt-2 text-3xl font-semibold text-amber-600">
          {{ data.processingJobs }}
        </p>
      </ElCard>
      <ElCard shadow="hover">
        <p class="text-muted-foreground text-sm">失败任务</p>
        <p class="mt-2 text-3xl font-semibold text-red-600">
          {{ data.failedJobs }}
        </p>
      </ElCard>
    </div>
    <ElCard shadow="never">
      <template #header>
        <div class="flex justify-between">
          <span class="font-medium">最近文章</span
          ><ElButton link type="primary" @click="router.push('/articles')">
            查看全部
          </ElButton>
        </div>
      </template>
      <ElTable
        :data="data.recentArticles"
        @row-click="(row) => router.push(`/articles/${row.id}`)"
      >
        <ElTableColumn
          label="标题"
          min-width="260"
          prop="title"
        /><ElTableColumn label="来源" min-width="150" prop="sourceName" />
        <ElTableColumn label="类型" width="120">
          <template #default="{ row }">
            <ElTag effect="plain">
              {{ row.sourceType === 'wechat' ? '公众号' : '网页' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <template #empty><ElEmpty description="还没有采集文章" /></template>
      </ElTable>
    </ElCard>
  </div>
</template>
