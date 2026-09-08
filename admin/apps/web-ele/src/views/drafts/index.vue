<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { Draft, DraftStatus } from '#/api';

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

import { getDraftsApi } from '#/api';

defineOptions({ name: 'Drafts' });
const router = useRouter();
const drafts = ref<Draft[]>([]);
const loading = ref(false);
const query = reactive<{ keyword: string; status: '' | DraftStatus }>({
  keyword: '',
  status: '',
});
const pagination = reactive({ page: 1, pageSize: 20, total: 0 });
const labels: Record<DraftStatus, string> = {
  editing: '编辑中',
  in_review: '待审核',
  approved: '已通过',
  publishing: '发布中',
  published: '已写入草稿箱',
  publish_failed: '发布失败',
};

function tagType(status: DraftStatus) {
  if (status === 'published') return 'success';
  if (status === 'publish_failed') return 'danger';
  if (status === 'approved') return 'success';
  if (status === 'in_review') return 'warning';
  return 'primary';
}
async function load() {
  loading.value = true;
  try {
    const data = await getDraftsApi({
      keyword: query.keyword.trim() || undefined,
      page: pagination.page,
      pageSize: pagination.pageSize,
      status: query.status || undefined,
    });
    drafts.value = data.items;
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
  query.status = '';
  search();
}
onMounted(load);
</script>

<template>
  <div class="space-y-4 p-5">
    <ElCard shadow="never">
      <div
        class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between"
      >
        <div>
          <h1 class="text-2xl font-semibold">微信稿件</h1>
          <p class="text-muted-foreground mt-2">
            从采集文章生成母稿，经过编辑与人工审核后写入公众号草稿箱。
          </p>
        </div>
        <ElButton type="primary" @click="router.push('/articles')">
          从内容库创建
        </ElButton>
      </div>
    </ElCard>
    <ElCard shadow="never">
      <div class="flex flex-wrap gap-3">
        <ElInput
          v-model="query.keyword"
          clearable
          class="w-72"
          placeholder="搜索标题或作者"
          @keyup.enter="search"
        /><ElSelect
          v-model="query.status"
          clearable
          class="w-48"
          placeholder="全部状态"
        >
          <ElOption
            v-for="(label, value) in labels"
            :key="value"
            :label="label"
            :value="value"
          /> </ElSelect
        ><ElButton type="primary" @click="search">查询</ElButton
        ><ElButton @click="reset">重置</ElButton>
      </div>
    </ElCard>
    <ElCard shadow="never">
      <ElTable
        v-loading="loading"
        :data="drafts"
        row-key="id"
        @row-click="(row) => router.push(`/drafts/${row.id}`)"
      >
        <ElTableColumn label="稿件" min-width="320">
          <template #default="{ row }">
            <div class="font-medium">{{ row.title }}</div>
            <div class="text-muted-foreground mt-1 text-xs">
              来源文章 #{{ row.sourceArticleId }} · v{{ row.currentVersion }}
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn label="作者" min-width="140" prop="author" />
        <ElTableColumn label="状态" width="150">
          <template #default="{ row }">
            <ElTag :type="tagType(row.status)">{{
              labels[row.status as DraftStatus]
            }}</ElTag>
          </template>
        </ElTableColumn>
        <ElTableColumn label="更新时间" width="180">
          <template #default="{ row }">
            {{ dayjs(row.updatedAt).format('YYYY-MM-DD HH:mm') }}
          </template>
        </ElTableColumn>
        <ElTableColumn align="right" label="操作" width="100">
          <template #default="{ row }">
            <ElButton
              link
              type="primary"
              @click.stop="router.push(`/drafts/${row.id}`)"
            >
              打开
            </ElButton>
          </template>
        </ElTableColumn>
        <template #empty><ElEmpty description="暂无微信稿件" /></template>
      </ElTable>
      <div class="mt-4 flex justify-end">
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
