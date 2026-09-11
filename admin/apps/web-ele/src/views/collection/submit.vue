<script lang="ts" setup>
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
  <div class="wp-page">
    <ElCard class="wp-page-hero" shadow="never">
      <div class="wp-page-hero__content">
        <div>
          <p class="wp-page-eyebrow">CONTENT COLLECTION</p>
          <h1 class="wp-page-title">提交文章链接</h1>
          <p class="wp-page-description">
            支持微信公众号公开文章和普通静态网页，采集任务会在后台异步执行。
          </p>
        </div>
        <ElTag effect="light" size="large" type="success">
          {{ sourceType }}
        </ElTag>
      </div>
    </ElCard>
    <ElCard class="wp-panel wp-submit-panel" shadow="never">
      <div class="wp-submit-box">
        <div class="wp-submit-label">
          <span>文章地址</span><ElTag effect="plain">{{ sourceType }}</ElTag>
        </div>
        <ElInput
          v-model="articleUrl"
          clearable
          placeholder="https://mp.weixin.qq.com/s/... 或普通文章地址"
          size="large"
          @keyup.enter="submit"
        />
        <p class="wp-submit-help">
          粘贴公开文章链接，系统会自动识别来源、抽取正文并归档图片。
        </p>
        <ElButton
          class="mt-4 w-full"
          :loading="submitting"
          size="large"
          type="primary"
          @click="submit"
        >
          开始采集
        </ElButton>
        <div class="wp-submit-features">
          <div class="wp-submit-feature">
            <strong>自动识别来源</strong>
            <span>识别微信公众号文章与普通静态网页。</span>
          </div>
          <div class="wp-submit-feature">
            <strong>后台异步处理</strong>
            <span>提交后可离开页面，任务进度会持续更新。</span>
          </div>
          <div class="wp-submit-feature">
            <strong>统一内容归档</strong>
            <span>正文、来源信息和图片会集中进入内容库。</span>
          </div>
        </div>
        <ElAlert
          class="wp-inline-alert mt-6"
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
