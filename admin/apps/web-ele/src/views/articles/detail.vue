<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type { AIStatus, Article } from '#/api';

import { computed, onMounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import dayjs from 'dayjs';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElEmpty,
  ElMessage,
  ElSkeleton,
  ElTag,
} from 'element-plus';

import { createDraftApi, getAIStatusApi, getArticleApi } from '#/api';

defineOptions({ name: 'ArticleDetail' });
const route = useRoute();
const router = useRouter();
const loading = ref(true);
const creatingDraft = ref(false);
const article = ref<Article>();
const aiStatus = ref<AIStatus>({ enabled: false, model: '', provider: '' });
const aiDisabledReason = computed(() => {
  if (article.value?.status !== 'ready') return '文章解析完成后可分析';
  if (!aiStatus.value.enabled) return 'AI 服务尚未配置';
  return '';
});
const assetURLs = computed(
  () =>
    new Map(
      (article.value?.assets || [])
        .filter(
          (asset) => asset.downloadStatus === 'completed' && asset.mediaUrl,
        )
        .map((asset) => [asset.id, asset.mediaUrl]),
    ),
);
onMounted(async () => {
  try {
    const articleRequest = getArticleApi(Number(route.params.id));
    const statusRequest = getAIStatusApi().catch(() => ({
      enabled: false,
      model: '',
      provider: '',
    }));
    const [articleData, status] = await Promise.all([
      articleRequest,
      statusRequest,
    ]);
    article.value = articleData;
    aiStatus.value = status;
  } finally {
    loading.value = false;
  }
});
async function analyze() {
  if (!article.value || aiDisabledReason.value) return;
  await router.push(`/ai/articles/${article.value.id}`);
}
async function createDraft() {
  if (!article.value) return;
  creatingDraft.value = true;
  try {
    const draft = await createDraftApi(article.value.id);
    ElMessage.success('微信稿件已创建');
    await router.push(`/drafts/${draft.id}`);
  } finally {
    creatingDraft.value = false;
  }
}
</script>

<template>
  <div class="p-5">
    <ElSkeleton v-if="loading" :rows="10" animated /><template
      v-else-if="article"
    >
      <ElCard shadow="never">
        <div
          class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between"
        >
          <div>
            <div class="mb-3 flex gap-2">
              <ElTag effect="plain">
                {{
                  article.sourceType === 'wechat' ? '微信公众号' : '普通网页'
                }} </ElTag
              ><ElTag
                :type="article.status === 'ready' ? 'success' : 'warning'"
              >
                {{ article.status }}
              </ElTag>
            </div>
            <h1 class="max-w-4xl text-3xl font-bold leading-tight">
              {{ article.title }}
            </h1>
            <p class="text-muted-foreground mt-3">
              {{ article.sourceName || article.author || '未知来源' }} ·
              {{
                dayjs(article.publishedAt || article.createdAt).format(
                  'YYYY-MM-DD HH:mm',
                )
              }}
            </p>
          </div>
          <div class="flex shrink-0 gap-2">
            <ElButton @click="router.back()">返回</ElButton
            ><ElButton
              v-if="article.status === 'ready'"
              :loading="creatingDraft"
              type="primary"
              @click="createDraft"
            >
              生成微信稿件 </ElButton
            ><ElButton :disabled="Boolean(aiDisabledReason)" @click="analyze">
              AI 分析 </ElButton
            ><ElButton tag="a" :href="article.originalUrl" target="_blank">
              原文 </ElButton
            ><ElButton
              v-if="article.rawSnapshotUrl"
              tag="a"
              :href="article.rawSnapshotUrl"
            >
              下载快照
            </ElButton>
          </div>
        </div>
        <p
          v-if="aiDisabledReason"
          class="text-muted-foreground mt-3 text-right text-sm"
        >
          {{ aiDisabledReason }}
        </p>
      </ElCard>
      <ElAlert
        v-if="article.duplicateOfId"
        class="mt-4"
        :closable="false"
        :title="`该内容与文章 #${article.duplicateOfId} 重复`"
        type="warning"
      />
      <ElCard class="mx-auto mt-4 max-w-4xl" shadow="never">
        <article class="article-body">
          <template v-for="(block, index) in article.blocks" :key="index">
            <component
              :is="`h${Math.min(Math.max(block.level || 2, 2), 4)}`"
              v-if="block.type === 'heading'"
            >
              {{ block.text }}
            </component>
            <p v-else-if="block.type === 'paragraph'">{{ block.text }}</p>
            <blockquote v-else-if="block.type === 'quote'">
              {{ block.text }}
            </blockquote>
            <pre
              v-else-if="block.type === 'code'"
            ><code>{{ block.text }}</code></pre>
            <li v-else-if="block.type === 'list'">{{ block.text }}</li>
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
          <ElEmpty
            v-if="!article.blocks?.length"
            description="正文仍在处理中"
          />
        </article>
      </ElCard> </template
    ><ElEmpty v-else description="文章不存在" />
  </div>
</template>

<style scoped>
.article-body {
  font-size: 17px;
  line-height: 1.9;
  color: var(--el-text-color-primary);
}

.article-body h2,
.article-body h3,
.article-body h4 {
  margin: 2rem 0 1rem;
  font-weight: 700;
  line-height: 1.4;
}

.article-body h2 {
  font-size: 1.6rem;
}

.article-body h3 {
  font-size: 1.35rem;
}

.article-body p,
.article-body li {
  margin: 1rem 0;
}

.article-body blockquote {
  padding: 1rem 1.25rem;
  margin: 1.5rem 0;
  background: var(--el-fill-color-light);
  border-left: 4px solid var(--el-color-primary);
}

.article-body pre {
  padding: 1rem;
  overflow-x: auto;
  color: #f9fafb;
  background: #111827;
  border-radius: 0.5rem;
}

.article-body figure {
  margin: 1.5rem 0;
}

.article-body img {
  max-width: 100%;
  margin: 0 auto;
  border-radius: 0.5rem;
}

.article-body figcaption {
  font-size: 0.875rem;
  color: var(--el-text-color-secondary);
  text-align: center;
}
</style>
