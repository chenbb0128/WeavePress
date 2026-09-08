<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline */
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElInput,
  ElMessage,
  ElTag,
} from 'element-plus';

import { submitCollectionApi } from '#/api';

defineOptions({ name: 'CollectionCreate' });
const router = useRouter();
const articleUrl = ref('');
const submitting = ref(false);
const sourceType = computed(() => {
  if (/https?:\/\/mp\.weixin\.qq\.com\//i.test(articleUrl.value)) {
    return '微信公众号';
  }
  if (articleUrl.value.trim()) {
    return '普通网页';
  }
  return '等待输入';
});

async function submit() {
  if (!/^https?:\/\//i.test(articleUrl.value.trim())) {
    ElMessage.warning('请输入完整的 HTTP 或 HTTPS 文章链接');
    return;
  }
  submitting.value = true;
  try {
    const result = await submitCollectionApi(articleUrl.value.trim());
    if (result.reused && result.article.status === 'ready') {
      ElMessage.success('文章已存在，已打开已有内容');
      await router.push(`/articles/${result.article.id}`);
      return;
    }
    ElMessage.success(
      result.reused ? '已有采集任务正在处理' : '采集任务已创建',
    );
    await router.push({
      path: '/collection/jobs',
      query: { job: result.job.id },
    });
  } finally {
    submitting.value = false;
  }
}
</script>

<template>
  <div class="space-y-4 p-5">
    <ElCard shadow="never">
      <h1 class="text-2xl font-semibold">提交文章链接</h1>
      <p class="text-muted-foreground mt-2">
        支持微信公众号公开文章和普通静态网页，采集任务会在后台异步执行。
      </p>
    </ElCard>
    <ElCard shadow="never">
      <div class="mx-auto max-w-3xl py-8">
        <div class="mb-4 flex items-center gap-2">
          <span class="text-sm font-medium">识别来源</span
          ><ElTag effect="plain">{{ sourceType }}</ElTag>
        </div>
        <ElInput
          v-model="articleUrl"
          clearable
          placeholder="https://mp.weixin.qq.com/s/... 或普通文章地址"
          size="large"
          @keyup.enter="submit"
        />
        <ElButton
          class="mt-4 w-full"
          :loading="submitting"
          size="large"
          type="primary"
          @click="submit"
        >
          开始采集
        </ElButton>
        <ElAlert
          class="mt-6"
          :closable="false"
          description="系统不会绕过登录、验证码或平台风控。仅提交公开、自产或已获授权的内容。"
          show-icon
          title="采集合规提示"
          type="info"
        />
      </div>
    </ElCard>
  </div>
</template>
