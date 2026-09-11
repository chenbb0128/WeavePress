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
  <div class="dashboard-page space-y-5 p-5">
    <ElCard class="hero-card" shadow="never">
      <div
        class="relative z-10 flex flex-col gap-5 sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <p class="hero-eyebrow">WEAVEPRESS CONTENT HUB</p>
          <h1 class="mt-2 text-2xl font-semibold">内容采集工作台</h1>
          <p class="hero-description mt-2">
            统一管理微信公众号与普通网页内容。
          </p>
        </div>
        <ElButton
          class="hero-action"
          size="large"
          type="primary"
          @click="router.push('/collection/new')"
        >
          提交文章
        </ElButton>
      </div>
    </ElCard>
    <div v-loading="loading" class="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <ElCard class="stat-card stat-card--primary" shadow="hover">
        <div class="stat-card__header">
          <p class="stat-card__label">文章总数</p>
          <span class="stat-card__mark">文</span>
        </div>
        <p class="stat-card__value">
          {{ data.articlesTotal }}
        </p>
        <p class="stat-card__hint">累计进入内容库</p>
      </ElCard>
      <ElCard class="stat-card stat-card--success" shadow="hover">
        <div class="stat-card__header">
          <p class="stat-card__label">今日采集</p>
          <span class="stat-card__mark">今</span>
        </div>
        <p class="stat-card__value">
          {{ data.collectedToday }}
        </p>
        <p class="stat-card__hint">今日完成的采集任务</p>
      </ElCard>
      <ElCard class="stat-card stat-card--warning" shadow="hover">
        <div class="stat-card__header">
          <p class="stat-card__label">处理中</p>
          <span class="stat-card__mark">队</span>
        </div>
        <p class="stat-card__value text-amber-600">
          {{ data.processingJobs }}
        </p>
        <p class="stat-card__hint">等待或正在执行</p>
      </ElCard>
      <ElCard class="stat-card stat-card--danger" shadow="hover">
        <div class="stat-card__header">
          <p class="stat-card__label">失败任务</p>
          <span class="stat-card__mark">警</span>
        </div>
        <p class="stat-card__value text-red-600">
          {{ data.failedJobs }}
        </p>
        <p class="stat-card__hint">需要人工查看或重试</p>
      </ElCard>
    </div>
    <div class="grid gap-5 xl:grid-cols-[minmax(0,2fr)_minmax(280px,1fr)]">
      <ElCard class="content-card" shadow="never">
        <template #header>
          <div class="card-heading">
            <div>
              <p class="card-heading__title">最近文章</p>
              <p class="card-heading__description">最新进入内容库的文章</p>
            </div>
            <ElButton link type="primary" @click="router.push('/articles')">
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
              <ElTag effect="light" type="success">
                {{ row.sourceType === 'wechat' ? '公众号' : '网页' }}
              </ElTag>
            </template>
          </ElTableColumn>
          <template #empty><ElEmpty description="还没有采集文章" /></template>
        </ElTable>
      </ElCard>

      <ElCard class="content-card source-card" shadow="never">
        <template #header>
          <div class="card-heading">
            <div>
              <p class="card-heading__title">来源分布</p>
              <p class="card-heading__description">当前内容库来源构成</p>
            </div>
          </div>
        </template>
        <div class="source-list">
          <div class="source-item">
            <span class="source-item__icon source-item__icon--wechat">微</span>
            <div class="min-w-0 flex-1">
              <p class="source-item__name">微信公众号</p>
              <p class="source-item__description">公开公众号文章</p>
            </div>
            <strong class="source-item__value">{{ data.sourceWechat }}</strong>
          </div>
          <div class="source-item">
            <span class="source-item__icon source-item__icon--web">网</span>
            <div class="min-w-0 flex-1">
              <p class="source-item__name">普通网页</p>
              <p class="source-item__description">静态网页与站点文章</p>
            </div>
            <strong class="source-item__value">{{ data.sourceWeb }}</strong>
          </div>
        </div>
      </ElCard>
    </div>
  </div>
</template>

<style scoped>
.dashboard-page {
  min-height: 100%;
  background:
    radial-gradient(circle at 95% 0%, rgb(16 185 129 / 7%), transparent 24rem),
    transparent;
}

.hero-card {
  position: relative;
  overflow: hidden;
  border: 1px solid rgb(16 185 129 / 16%);
  background: linear-gradient(
    120deg,
    rgb(16 185 129 / 12%),
    rgb(255 255 255 / 78%) 58%
  );
}

:global(.dark) .hero-card {
  background: linear-gradient(
    120deg,
    rgb(16 185 129 / 14%),
    hsl(var(--card)) 58%
  );
}

.hero-card::after {
  position: absolute;
  top: -5rem;
  right: -3rem;
  width: 15rem;
  height: 15rem;
  content: '';
  border: 2rem solid rgb(16 185 129 / 8%);
  border-radius: 9999px;
}

.hero-card :deep(.el-card__body) {
  padding: 28px 30px;
}

.hero-eyebrow {
  color: rgb(5 150 105);
  font-size: 11px;
  font-weight: 700;
  letter-spacing: 0.16em;
}

.hero-description,
.stat-card__label,
.stat-card__hint,
.card-heading__description,
.source-item__description {
  color: hsl(var(--muted-foreground));
}

.hero-action {
  min-width: 112px;
  box-shadow: 0 8px 20px rgb(16 185 129 / 20%);
}

.stat-card {
  position: relative;
  overflow: hidden;
  border-color: hsl(var(--border));
}

.stat-card::before {
  position: absolute;
  inset: 0 auto 0 0;
  width: 3px;
  content: '';
  background: var(--stat-color);
}

.stat-card--primary {
  --stat-color: rgb(59 130 246);
  --stat-soft: rgb(59 130 246 / 10%);
}

.stat-card--success {
  --stat-color: rgb(16 185 129);
  --stat-soft: rgb(16 185 129 / 10%);
}

.stat-card--warning {
  --stat-color: rgb(245 158 11);
  --stat-soft: rgb(245 158 11 / 10%);
}

.stat-card--danger {
  --stat-color: rgb(244 63 94);
  --stat-soft: rgb(244 63 94 / 10%);
}

.stat-card :deep(.el-card__body) {
  padding: 22px 24px;
}

.stat-card__header,
.card-heading,
.source-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.stat-card__label {
  font-size: 14px;
}

.stat-card__mark {
  display: inline-flex;
  width: 34px;
  height: 34px;
  align-items: center;
  justify-content: center;
  color: var(--stat-color);
  font-size: 13px;
  font-weight: 700;
  background: var(--stat-soft);
  border-radius: 10px;
}

.stat-card__value {
  margin-top: 14px;
  font-size: 30px;
  font-weight: 700;
  line-height: 1;
}

.stat-card__hint {
  margin-top: 10px;
  font-size: 12px;
}

.content-card {
  border-color: hsl(var(--border));
}

.content-card :deep(.el-card__header) {
  padding: 20px 22px 16px;
}

.content-card :deep(.el-card__body) {
  padding: 0 22px 20px;
}

.card-heading__title,
.source-item__value {
  font-weight: 600;
}

.card-heading__description,
.source-item__description {
  margin-top: 4px;
  font-size: 12px;
}

.source-list {
  display: grid;
  gap: 12px;
  padding-top: 4px;
}

.source-item {
  gap: 12px;
  padding: 14px;
  background: hsl(var(--muted) / 45%);
  border: 1px solid hsl(var(--border));
  border-radius: 12px;
}

.source-item__icon {
  display: inline-flex;
  width: 38px;
  height: 38px;
  flex: none;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 700;
  border-radius: 12px;
}

.source-item__icon--wechat {
  color: rgb(5 150 105);
  background: rgb(16 185 129 / 12%);
}

.source-item__icon--web {
  color: rgb(37 99 235);
  background: rgb(59 130 246 / 12%);
}

.source-item__name {
  font-size: 14px;
  font-weight: 500;
}

.source-item__value {
  font-size: 20px;
}

@media (max-width: 640px) {
  .dashboard-page {
    padding: 12px;
  }

  .hero-card :deep(.el-card__body) {
    padding: 22px 20px;
  }
}
</style>
