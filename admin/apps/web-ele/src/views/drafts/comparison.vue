<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type {
  AIQualityInput,
  AIQualityIssue,
  AIQualityReport,
  AIRewriteSuggestion,
  Article,
  DraftAsset,
  EditorDocument,
  EditorImageAttrs,
} from '#/api';

import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';

import { ElAlert, ElButton, ElCard, ElCheckbox, ElTag } from 'element-plus';

import { checkAIQualityApi, rewriteAIBlockApi } from '#/api';

import {
  applyRewrite,
  comparisonSourceBlocks,
  highlightText,
  qualityBlocks,
} from './comparison';

const props = defineProps<{
  article: Article;
  assets: DraftAsset[];
  canCheck: boolean;
  canRewrite: boolean;
  document: EditorDocument;
  editable: boolean;
  title: string;
}>();
const emit = defineEmits<{
  'update:document': [document: EditorDocument];
  'update:title': [title: string];
}>();
const root = ref<HTMLElement>();
const report = ref<AIQualityReport>();
const selected = ref<AIQualityIssue>();
const suggestion = ref<AIRewriteSuggestion>();
const checking = ref(false);
const rewriting = ref(false);
const faithful = ref(true);
const error = ref('');
const changed = ref(false);
const expanded = ref(false);
const fingerprint = computed(() =>
  JSON.stringify([
    props.article.id,
    props.title,
    props.document,
    faithful.value,
  ]),
);
const draftBlocks = computed(() => qualityBlocks(props.document));
const sourceBlocks = computed(() => comparisonSourceBlocks(props.article));
const sourceImages = computed(
  () =>
    new Map(
      (props.article.assets ?? []).map((asset) => [asset.id, asset.mediaUrl]),
    ),
);
const draftImages = computed(
  () => new Map(props.assets.map((asset) => [asset.id, asset.mediaUrl])),
);
let epoch = 0;
let controller = new AbortController();
let suggestionFingerprint = '';

function invalidate() {
  epoch += 1;
  controller.abort();
  controller = new AbortController();
  checking.value = false;
  rewriting.value = false;
}

watch(
  fingerprint,
  () => {
    changed.value = Boolean(report.value || suggestion.value);
    invalidate();
    report.value = undefined;
    selected.value = undefined;
    suggestion.value = undefined;
    error.value = '';
  },
  { flush: 'sync' },
);
onBeforeUnmount(invalidate);

function input(semantic: boolean): AIQualityInput {
  return {
    title: props.title,
    blocks: draftBlocks.value,
    faithful: faithful.value,
    semantic,
  };
}

async function check(semantic: boolean) {
  if (!props.canCheck || checking.value || rewriting.value) return;
  invalidate();
  const activeEpoch = epoch;
  checking.value = true;
  error.value = '';
  changed.value = false;
  suggestion.value = undefined;
  selected.value = undefined;
  try {
    const fast = await checkAIQualityApi(
      props.article.id,
      input(false),
      controller.signal,
    );
    if (epoch !== activeEpoch) return;
    report.value = fast;
    if (semantic) {
      const full = await checkAIQualityApi(
        props.article.id,
        input(true),
        controller.signal,
      );
      if (epoch !== activeEpoch) return;
      report.value = full;
    }
  } catch {
    if (epoch === activeEpoch)
      error.value = report.value
        ? '内容核对失败，已保留快速检查结果，可重试。'
        : '检查失败，请稍后重试。';
  } finally {
    if (epoch === activeEpoch) checking.value = false;
  }
}

async function locate(issue: AIQualityIssue) {
  selected.value = issue;
  expanded.value = true;
  await nextTick();
  const source = [
    ...(root.value?.querySelectorAll<HTMLElement>('[data-source-id]') ?? []),
  ].find((item) => item.dataset.sourceId === issue.sourceBlockId);
  source?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
  const draft = [
    ...(root.value?.querySelectorAll<HTMLElement>('[data-draft-index]') ?? []),
  ].find(
    (item) => Number(item.dataset.draftIndex) === (issue.blockIndex ?? -1),
  );
  draft?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' });
}

function rewritable(issue: AIQualityIssue) {
  if (issue.blockIndex === -1) return true;
  if (issue.blockIndex === undefined) return issue.code === 'TITLE_DUPLICATE';
  return ['heading', 'paragraph', 'quote'].includes(
    draftBlocks.value[issue.blockIndex]?.type ?? '',
  );
}

async function rewrite(issue: AIQualityIssue) {
  if (
    !props.editable ||
    !props.canRewrite ||
    rewriting.value ||
    checking.value ||
    !rewritable(issue)
  )
    return;
  invalidate();
  const activeEpoch = epoch;
  suggestionFingerprint = fingerprint.value;
  rewriting.value = true;
  suggestion.value = undefined;
  error.value = '';
  await locate(issue);
  if (epoch !== activeEpoch) return;
  try {
    const result = await rewriteAIBlockApi(
      props.article.id,
      { ...input(false), targetIndex: issue.blockIndex ?? -1 },
      controller.signal,
    );
    if (epoch !== activeEpoch || fingerprint.value !== suggestionFingerprint)
      return;
    suggestion.value = result;
  } catch {
    if (epoch === activeEpoch)
      error.value = '局部改写未通过检查或请求失败，可人工修改后重新检查。';
  } finally {
    if (epoch === activeEpoch) rewriting.value = false;
  }
}

function apply() {
  const result = suggestion.value;
  if (
    !result ||
    !props.editable ||
    !props.canRewrite ||
    fingerprint.value !== suggestionFingerprint
  )
    return;
  if (result.targetIndex === -1) emit('update:title', result.text);
  else {
    const updated = applyRewrite(
      props.document,
      result.targetIndex,
      result.text,
      result.type,
    );
    if (updated) emit('update:document', updated);
  }
  suggestion.value = undefined;
}

function imageUrl(index: number) {
  const attrs = props.document.content?.[index]?.attrs as
    | EditorImageAttrs
    | undefined;
  return attrs ? draftImages.value.get(attrs.draftAssetId) : undefined;
}
</script>

<template>
  <ElCard class="mt-4" shadow="never">
    <template #header>
      <div class="flex flex-wrap items-center justify-between gap-3">
        <strong>原文与成稿对照</strong>
        <div class="flex flex-wrap gap-2">
          <ElButton @click="expanded = !expanded">{{
            expanded ? '收起对照' : '展开对照'
          }}</ElButton>
          <ElButton
            v-if="canCheck"
            :disabled="checking || rewriting"
            @click="check(false)"
            >快速检查</ElButton
          >
          <ElButton
            v-if="canCheck"
            :loading="checking"
            :disabled="rewriting"
            type="primary"
            @click="check(true)"
            >检查当前内容</ElButton
          >
        </div>
      </div>
    </template>
    <div ref="root">
      <p class="text-muted-foreground text-sm">
        快速检查标题、正文复用和引用数量；内容检查还核对事实、数字与作者归因。检查包含当前未保存的修改。
      </p>
      <ElCheckbox v-model="faithful"
        >完整性检查（忠实复刻；角度改写可取消）</ElCheckbox
      >
      <ElAlert
        v-if="error"
        class="mt-3"
        :closable="false"
        :title="error"
        type="warning"
      />
      <ElAlert
        v-if="changed"
        class="mt-3"
        :closable="false"
        title="内容已变更，请重新检查。"
        type="info"
      />
      <div v-if="report" class="mt-3 space-y-3">
        <p class="text-sm">
          {{
            report.semanticChecked
              ? '已完成内容核对，语义结果仍需人工确认'
              : '已完成快速检查，事实与身份归因尚未核对'
          }}
          · {{ report.issues.length }} 项待处理
        </p>
        <p v-if="!report.issues.length" class="text-muted-foreground text-sm">
          本次检查未发现问题，可继续人工校稿。
        </p>
        <div
          v-for="(issue, index) in report.issues"
          :key="index"
          class="rounded border p-3"
        >
          <div class="flex flex-wrap items-center gap-2">
            <ElTag :type="issue.severity === 'error' ? 'danger' : 'warning'">{{
              issue.severity === 'error' ? '需修改' : '待核对'
            }}</ElTag>
            <span
              >{{
                issue.blockIndex === undefined || issue.blockIndex === -1
                  ? issue.code === 'TOPIC_MISSING'
                    ? '话题完整性'
                    : '标题'
                  : `第 ${issue.blockIndex + 1} 块`
              }}：{{ issue.message }}</span
            >
          </div>
          <p
            v-if="issue.sourceExcerpt"
            class="text-muted-foreground mt-2 whitespace-pre-wrap text-sm"
          >
            原文：{{ issue.sourceExcerpt }}
          </p>
          <div class="mt-2 flex gap-2">
            <ElButton size="small" @click="locate(issue)">查看对照</ElButton>
            <ElButton
              v-if="editable && canRewrite && rewritable(issue)"
              size="small"
              :disabled="checking || rewriting"
              @click="rewrite(issue)"
              >{{
                issue.blockIndex === undefined || issue.blockIndex === -1
                  ? '改写标题'
                  : '改写此段'
              }}</ElButton
            >
          </div>
        </div>
      </div>
      <div v-if="suggestion" class="mt-4 rounded border p-4">
        <strong>局部改写建议</strong>
        <p class="mt-2 whitespace-pre-wrap">{{ suggestion.text }}</p>
        <p class="text-muted-foreground mt-2 text-sm">
          应用将替换对应标题或段落文字，保留其他内容和图片；核对后保存新版本。
        </p>
        <div class="mt-3 flex gap-2">
          <ElButton :disabled="!editable" type="primary" @click="apply"
            >应用建议</ElButton
          >
          <ElButton @click="suggestion = undefined">放弃建议</ElButton>
        </div>
      </div>
      <div v-show="expanded" class="mt-4 grid gap-4 lg:grid-cols-2">
        <section
          aria-label="原文对照"
          class="max-h-[600px] space-y-3 overflow-auto rounded border p-4"
        >
          <h3 class="font-semibold">原文</h3>
          <h4
            data-source-id="TITLE"
            class="font-semibold"
            :class="{
              'bg-amber-50':
                selected?.code === 'TITLE_DUPLICATE' ||
                selected?.sourceBlockId === 'TITLE',
            }"
          >
            {{ article.title }}
          </h4>
          <div
            v-for="block in sourceBlocks"
            :key="block.id"
            :data-source-id="block.id"
            class="whitespace-pre-wrap rounded p-2"
            :class="{ 'bg-amber-50': selected?.sourceBlockId === block.id }"
          >
            <span class="text-muted-foreground text-xs"
              >{{ block.id }}{{ block.type === 'image' ? ' · 图片' : '' }}</span
            >
            <img
              v-if="
                block.type === 'image' &&
                block.assetId &&
                sourceImages.get(block.assetId)
              "
              :src="sourceImages.get(block.assetId)"
              :alt="block.alt || ''"
              class="mt-2 max-h-56"
              loading="lazy"
            />
            <p v-else>
              <template
                v-for="(segment, index) in highlightText(
                  block.text ?? '',
                  selected?.sourceBlockId === block.id
                    ? selected.sourceExcerpt
                    : '',
                )"
                :key="index"
                ><mark v-if="segment.matched">{{ segment.text }}</mark
                ><span v-else>{{ segment.text }}</span></template
              >
            </p>
          </div>
        </section>
        <section
          aria-label="成稿对照"
          class="max-h-[600px] space-y-3 overflow-auto rounded border p-4"
        >
          <h3 class="font-semibold">当前成稿</h3>
          <h4
            data-draft-index="-1"
            class="font-semibold"
            :class="{
              'bg-amber-50':
                selected?.code === 'TITLE_DUPLICATE' ||
                selected?.blockIndex === -1,
            }"
          >
            {{ title }}
          </h4>
          <div
            v-for="(block, index) in draftBlocks"
            :key="index"
            :data-draft-index="index"
            class="whitespace-pre-wrap rounded p-2"
            :class="{ 'bg-amber-50': selected?.blockIndex === index }"
          >
            <span class="text-muted-foreground text-xs"
              >第 {{ index + 1 }} 块{{
                block.type === 'image' ? ' · 图片' : ''
              }}</span
            >
            <img
              v-if="block.type === 'image' && imageUrl(index)"
              :src="imageUrl(index)"
              alt="稿件配图"
              class="mt-2 max-h-56"
              loading="lazy"
            />
            <p v-else>
              <template
                v-for="(segment, part) in highlightText(
                  block.text,
                  selected?.blockIndex === index ? selected.excerpt : '',
                )"
                :key="part"
                ><mark v-if="segment.matched">{{ segment.text }}</mark
                ><span v-else>{{ segment.text }}</span></template
              >
            </p>
          </div>
        </section>
      </div>
    </div>
  </ElCard>
</template>
