<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { Article, ArticleStatus, SourceType } from '#/api';

import { onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';

import dayjs from 'dayjs';
import {
  ElButton,
  ElCard,
  ElEmpty,
  ElInput,
  ElOption,
  ElPagination,
  ElSelect,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';

import { getArticlesApi } from '#/api';

defineOptions({ name: 'Articles' });
const router = useRouter();
const articles = ref<Article[]>([]);
const loading = ref(false);
const query = reactive<{
  keyword: string;
  sourceType: '' | SourceType;
  status: '' | ArticleStatus;
}>({ keyword: '', sourceType: '', status: '' });
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
const statusLabels: Record<ArticleStatus, string> = {
  pending: '待处理',
  processing: '处理中',
  ready: '可阅读',
  failed: '失败',
};

async function load() {
  loading.value = true;
  try {
    const data = await getArticlesApi({
      keyword: query.keyword.trim() || undefined,
      page: pagination.page,
      pageSize: pagination.pageSize,
      sourceType: query.sourceType || undefined,
      status: query.status || undefined,
    });
    articles.value = data.items;
    pagination.total = data.total;
  } finally {
    loading.value = false;
  }
}
function search() {
  pagination.page = 1;
  void load();
}
function reset() {
  query.keyword = '';
  query.sourceType = '';
  query.status = '';
  search();
}
function statusLabel(value: unknown) {
  return statusLabels[value as ArticleStatus] || String(value);
}
onMounted(load);
</script>

<template>
  <div class="wp-page">
    <ElCard class="wp-page-hero" shadow="never">
      <div class="wp-page-hero__content">
        <div>
          <p class="wp-page-eyebrow">CONTENT LIBRARY</p>
          <h1 class="wp-page-title">内容库</h1>
          <p class="wp-page-description">集中查看已经采集和结构化的文章。</p>
        </div>
        <ElButton
          class="wp-page-action"
          size="large"
          type="primary"
          @click="router.push('/collection/new')"
        >
          提交文章
        </ElButton>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-filter-panel" shadow="never">
      <div class="wp-filter-bar">
        <span class="wp-filter-bar__label">筛选条件</span>
        <div class="wp-filter-bar__controls">
          <ElInput
            v-model="query.keyword"
            clearable
            class="w-72"
            placeholder="搜索标题、作者或来源"
            @keyup.enter="search"
          /><ElSelect
            v-model="query.sourceType"
            clearable
            class="w-40"
            placeholder="来源类型"
          >
            <ElOption label="微信公众号" value="wechat" /><ElOption
              label="普通网页"
              value="web"
            /> </ElSelect
          ><ElSelect
            v-model="query.status"
            clearable
            class="w-40"
            placeholder="处理状态"
          >
            <ElOption
              v-for="(label, value) in statusLabels"
              :key="value"
              :label="label"
              :value="value"
            /> </ElSelect
          ><ElButton type="primary" @click="search">查询</ElButton
          ><ElButton @click="reset">重置</ElButton>
        </div>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-table-panel" shadow="never">
      <div class="wp-table-panel__header">
        <div>
          <p class="wp-panel-title">内容列表</p>
          <p class="wp-panel-description">查看正文状态、来源和发布时间</p>
        </div>
        <span class="wp-record-count">共 {{ pagination.total }} 篇文章</span>
      </div>
      <ElTable
        v-loading="loading"
        class="wp-data-table"
        :data="articles"
        row-key="id"
        @row-click="(row) => router.push(`/articles/${row.id}`)"
      >
        <ElTableColumn
          label="标题"
          min-width="300"
          prop="title"
        /><ElTableColumn label="来源" min-width="160">
          <template #default="{ row }">
            {{ row.sourceName || row.author || '-' }}
          </template>
        </ElTableColumn>
        <ElTableColumn label="类型" width="120">
          <template #default="{ row }">
            <ElTag effect="plain">
              {{ row.sourceType === 'wechat' ? '公众号' : '网页' }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="状态" width="120">
          <template #default="{ row }">
            <ElTag
              :type="
                row.status === 'ready'
                  ? 'success'
                  : row.status === 'failed'
                    ? 'danger'
                    : 'primary'
              "
            >
              {{ statusLabel(row.status) }}
            </ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="发布时间" width="180">
          <template #default="{ row }">
            {{
              dayjs(row.publishedAt || row.createdAt).format('YYYY-MM-DD HH:mm')
            }}
          </template>
        </ElTableColumn>
        <template #empty><ElEmpty description="内容库还是空的" /></template>
      </ElTable>
      <div class="wp-table-panel__footer">
        <ElPagination
          background
          :current-page="pagination.page"
          layout="total, prev, pager, next"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          @current-change="
            (page) => {
              pagination.page = page;
              load();
            }
          "
        />
      </div>
    </ElCard>
  </div>
</template>
