<script lang="ts" setup>
import type { WeChatLayoutTheme } from '#/api';

defineProps<{
  disabled: boolean;
  modelValue: string;
  themes: WeChatLayoutTheme[];
}>();
const emit = defineEmits<{
  'update:modelValue': [value: string];
}>();
</script>

<template>
  <section aria-label="文章排版主题" class="theme-panel">
    <h2 class="mb-3 font-semibold">文章排版主题</h2>
    <button
      v-for="theme in themes"
      :key="theme.id"
      type="button"
      class="theme-card"
      :class="{ selected: modelValue === theme.id }"
      :disabled="disabled"
      :aria-pressed="modelValue === theme.id"
      @click="emit('update:modelValue', theme.id)"
    >
      <span
        class="theme-sample"
        :style="{
          borderColor: theme.tokens.accent,
          backgroundColor: theme.tokens.surface,
          color: theme.tokens.accent,
        }"
      >
        <span
          class="theme-swatch"
          :style="{ backgroundColor: theme.preview }"
        ></span>
        标题与正文排版
      </span>
      <span>{{ theme.name }}</span>
    </button>
  </section>
</template>

<style scoped>
.theme-card {
  display: grid;
  gap: 8px;
  width: 100%;
  padding: 12px;
  margin-bottom: 10px;
  color: var(--el-text-color-primary);
  text-align: left;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
}

.theme-card.selected {
  border-color: var(--el-color-primary);
  box-shadow: 0 0 0 1px var(--el-color-primary);
}

.theme-card:disabled {
  cursor: not-allowed;
}

.theme-sample {
  padding: 8px 10px;
  font-size: 13px;
  border-left: 3px solid;
}

.theme-swatch {
  display: inline-block;
  width: 12px;
  height: 12px;
  margin-right: 6px;
  border-radius: 50%;
}
</style>
