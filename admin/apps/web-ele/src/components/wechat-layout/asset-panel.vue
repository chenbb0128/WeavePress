<script lang="ts" setup>
import type { DraftAsset } from '#/api';

import { ref } from 'vue';
import { ElButton, ElEmpty, ElMessage, ElTag } from 'element-plus';

const props = defineProps<{
  assets: DraftAsset[];
  coverAssetId?: number;
  disabled: boolean;
}>();
const emit = defineEmits<{
  insert: [asset: DraftAsset];
  cover: [assetId: number];
  upload: [file: File];
}>();
const fileInput = ref<HTMLInputElement>();

function upload(event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = '';
  if (!file || props.disabled) return;
  if (!/\.(jpe?g|png|gif|webp)$/i.test(file.name)) {
    ElMessage.warning('仅支持 JPEG、PNG、GIF、WebP 图片');
    return;
  }
  if (file.size > 10 * 1024 * 1024) {
    ElMessage.warning('单张图片不能超过 10 MiB');
    return;
  }
  emit('upload', file);
}
function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}
</script>

<template>
  <section aria-label="稿件素材">
    <div class="mb-3 flex items-center justify-between gap-2">
      <h2 class="font-semibold">稿件素材</h2>
      <ElButton :disabled="disabled" size="small" @click="fileInput?.click()"
        >上传图片</ElButton
      >
    </div>
    <input
      ref="fileInput"
      type="file"
      accept=".jpg,.jpeg,.png,.gif,.webp"
      hidden
      :disabled="disabled"
      @change="upload"
    />
    <div v-for="asset in assets" :key="asset.id" class="asset-card">
      <img :src="asset.mediaUrl" :alt="`素材 #${asset.id}`" loading="lazy" />
      <div class="mt-2 flex flex-wrap items-center gap-2 text-sm">
        <span
          >#{{ asset.id }} ·
          {{ asset.origin === 'article' ? '来源文章' : '本地上传' }}</span
        >
        <ElTag v-if="asset.id === coverAssetId" size="small">封面</ElTag>
      </div>
      <p class="text-muted-foreground my-2 text-xs">
        {{ asset.width }} × {{ asset.height }} ·
        {{ formatBytes(asset.byteSize) }}
      </p>
      <p v-if="!asset.bodyEligible" class="mb-2 text-xs text-warning">
        超过微信正文 1 MiB 限制
      </p>
      <div class="flex flex-wrap gap-2">
        <ElButton
          :disabled="disabled || !asset.bodyEligible"
          size="small"
          type="primary"
          plain
          @click="emit('insert', asset)"
          >插入正文</ElButton
        >
        <ElButton
          :disabled="disabled || !asset.coverEligible"
          size="small"
          @click="emit('cover', asset.id)"
          >设为封面</ElButton
        >
      </div>
    </div>
    <ElEmpty
      v-if="!assets.length"
      description="暂无可用素材"
      :image-size="64"
    />
  </section>
</template>

<style scoped>
.asset-card {
  margin-bottom: 12px;
  padding: 12px;
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
}
.asset-card img {
  width: 100%;
  height: 140px;
  object-fit: contain;
  background: var(--el-fill-color-light);
}
.text-warning {
  color: var(--el-color-warning);
}
</style>
