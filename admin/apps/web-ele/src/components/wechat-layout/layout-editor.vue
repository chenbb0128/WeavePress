<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline */
import type { DraftMetadata } from './document';

import type {
  DraftAsset,
  EditorDocument,
  EditorImageAttrs,
  WeChatLayoutTheme,
} from '#/api';

import {
  computed,
  onBeforeUnmount,
  ref,
  shallowRef,
  watch,
  watchEffect,
} from 'vue';

import Link from '@tiptap/extension-link';
import Placeholder from '@tiptap/extension-placeholder';
import Underline from '@tiptap/extension-underline';
import StarterKit from '@tiptap/starter-kit';
import { EditorContent, useEditor } from '@tiptap/vue-3';
import { useMediaQuery } from '@vueuse/core';
import {
  ElButton,
  ElDrawer,
  ElInput,
  ElMessage,
  ElOption,
  ElSelect,
} from 'element-plus';

import AssetPanel from './asset-panel.vue';
import { normalizeDocument, renderPreviewHtml } from './document';
import EditorToolbar from './editor-toolbar.vue';
import PhonePreview from './phone-preview.vue';
import ThemePanel from './theme-panel.vue';
import { WechatImage } from './wechat-image';

const props = defineProps<{
  assets: DraftAsset[];
  document: EditorDocument;
  editable: boolean;
  metadata: DraftMetadata;
  themeId: string;
  themes: WeChatLayoutTheme[];
}>();
const emit = defineEmits<{
  coverChange: [assetId: number];
  'update:document': [value: EditorDocument];
  'update:themeId': [value: string];
  upload: [file: File];
}>();

const desktop = useMediaQuery('(min-width: 1280px)');
const leftDrawer = ref(false);
const rightDrawer = ref(false);
const selectedImage = ref<EditorImageAttrs>();
const theme = computed(() =>
  props.themes.find((item) => item.id === props.themeId),
);
const assetURLs = computed(
  () => new Map(props.assets.map((asset) => [asset.id, asset.mediaUrl])),
);
const bodyHtml = computed(() =>
  theme.value
    ? renderPreviewHtml(props.document, theme.value, assetURLs.value)
    : '',
);
const coverUrl = computed(() =>
  assetURLs.value.get(props.metadata.coverAssetId ?? 0),
);
const canvasVariables = computed(() => {
  if (!theme.value) return {};
  return Object.fromEntries(
    Object.entries(theme.value.tokens).flatMap(([key, value]) => {
      const valid =
        key === 'fontFamily'
          ? value.length <= 160 && /^[\w\s'",-]+$/.test(value)
          : /^#[\dA-Fa-f]{3}(?:[\dA-Fa-f]{3}|[\dA-Fa-f]{5})?$/.test(value);
      const property =
        key === 'fontFamily' ? '--article-font-family' : `--article-${key}`;
      return valid ? [[property, value]] : [];
    }),
  );
});

// NodeView 的 URL 只从稿件素材映射读取，不写入文档 attrs。
const imageExtension = WechatImage.extend({
  addNodeView() {
    return ({ node }) => {
      const currentNode = shallowRef(node);
      const dom = window.document.createElement('figure');
      const image = window.document.createElement('img');
      const caption = window.document.createElement('figcaption');
      dom.contentEditable = 'false';
      dom.append(image, caption);
      const stop = watchEffect(() => {
        const attrs = currentNode.value.attrs as EditorImageAttrs;
        const source = assetURLs.value.get(attrs.draftAssetId);
        if (source && /^(?:https?:\/\/|\/(?!\/))/i.test(source))
          image.src = source;
        else image.removeAttribute('src');
        image.alt = attrs.alt;
        image.style.width = `${[50, 75, 100].includes(attrs.width) ? attrs.width : 100}%`;
        caption.textContent = attrs.caption;
        caption.hidden = !attrs.caption;
        dom.dataset.draftAssetId = String(attrs.draftAssetId);
      });
      return {
        dom,
        update(updated) {
          if (updated.type !== node.type) return false;
          currentNode.value = updated;
          return true;
        },
        destroy: stop,
      };
    };
  },
});

function safeHTTPURL(value: string) {
  if (!/^https?:\/\//i.test(value)) return false;
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}
function cleanPastedHTML(html: string) {
  const document = new DOMParser().parseFromString(html, 'text/html');
  document
    .querySelectorAll(
      'script,style,iframe,object,embed,form,input,button,link,meta,base,img',
    )
    .forEach((element) => element.remove());
  for (const element of document.body.querySelectorAll('*')) {
    for (const attribute of [...element.attributes]) {
      if (
        /^(?:style|class|id)$/i.test(attribute.name) ||
        /^on/i.test(attribute.name)
      )
        element.removeAttribute(attribute.name);
    }
    if (
      element.hasAttribute('href') &&
      !safeHTTPURL(element.getAttribute('href') ?? '')
    )
      element.removeAttribute('href');
    if (
      element instanceof HTMLElement &&
      element.matches('figure[data-draft-asset-id]')
    ) {
      const id = Number(element.dataset.draftAssetId);
      if (!props.assets.some((asset) => asset.id === id && asset.bodyEligible))
        delete element.dataset.draftAssetId;
    }
  }
  return document.body.innerHTML;
}

const editor = useEditor({
  content: normalizeDocument(props.document),
  editable: props.editable,
  extensions: [
    StarterKit.configure({
      heading: { levels: [2, 3] },
      codeBlock: false,
      code: false,
      strike: false,
      link: false,
      underline: false,
      trailingNode: false,
    }),
    Underline,
    Link.configure({
      openOnClick: false,
      protocols: ['http', 'https'],
      isAllowedUri: safeHTTPURL,
    }),
    Placeholder.configure({
      placeholder: '开始编辑微信正文，或从左侧插入素材…',
    }),
    imageExtension,
  ],
  editorProps: { transformPastedHTML: cleanPastedHTML },
  onUpdate({ editor }) {
    emit(
      'update:document',
      normalizeDocument(editor.getJSON() as EditorDocument),
    );
    updateSelection();
  },
  onSelectionUpdate: updateSelection,
});
function updateSelection() {
  const instance = editor.value;
  selectedImage.value = instance?.isActive('image')
    ? ({ ...instance.getAttributes('image') } as EditorImageAttrs)
    : undefined;
}
watch(
  () => props.editable,
  (value) => editor.value?.setEditable(value, false),
);
watch(
  () => props.document,
  (value) => {
    const instance = editor.value;
    if (!instance) return;
    const normalized = normalizeDocument(value);
    if (
      JSON.stringify(normalized) !==
      JSON.stringify(normalizeDocument(instance.getJSON() as EditorDocument))
    ) {
      instance.commands.setContent(normalized, { emitUpdate: false });
      updateSelection();
    }
  },
  { deep: true },
);
watch(desktop, (value) => {
  if (value) {
    leftDrawer.value = false;
    rightDrawer.value = false;
  }
});
onBeforeUnmount(() => editor.value?.destroy());

function insertAsset(asset: DraftAsset) {
  if (!props.editable || !asset.bodyEligible) return;
  editor.value
    ?.chain()
    .focus()
    .insertContent({
      type: 'image',
      attrs: {
        draftAssetId: asset.id,
        width: 100,
        align: 'center',
        alt: '',
        caption: '',
      },
    })
    .run();
}
function changeImage(attrs: Partial<EditorImageAttrs>) {
  if (!props.editable || !editor.value?.isActive('image')) return;
  editor.value.commands.updateAttributes('image', attrs);
}
async function copyLayout() {
  if (!bodyHtml.value) return;
  const html = bodyHtml.value;
  const plain =
    new DOMParser().parseFromString(html, 'text/html').body.textContent ?? '';
  try {
    if (
      typeof ClipboardItem !== 'undefined' &&
      navigator.clipboard?.write &&
      (!ClipboardItem.supports || ClipboardItem.supports('text/html'))
    ) {
      try {
        await navigator.clipboard.write([
          new ClipboardItem({
            'text/html': new Blob([html], { type: 'text/html' }),
            'text/plain': new Blob([plain], { type: 'text/plain' }),
          }),
        ]);
        ElMessage.success('微信排版已复制');
        return;
      } catch (error) {
        if (
          !(error instanceof DOMException) ||
          error.name !== 'NotSupportedError'
        )
          throw error;
      }
    }
    await navigator.clipboard.writeText(html);
    ElMessage.success('当前浏览器不支持富剪贴板，已复制排版 HTML');
  } catch {
    ElMessage.error('复制失败，请允许剪贴板权限后重试');
  }
}
</script>

<template>
  <div class="layout-workspace">
    <div v-if="!desktop" class="mb-3 flex gap-2">
      <ElButton @click="leftDrawer = true">主题与素材</ElButton>
      <ElButton @click="rightDrawer = true">手机预览</ElButton>
    </div>
    <div class="workspace-grid">
      <aside v-if="desktop" class="workspace-panel side-panel">
        <ThemePanel
          :themes="themes"
          :model-value="themeId"
          :disabled="!editable"
          @update:model-value="emit('update:themeId', $event)"
        />
        <AssetPanel
          class="mt-6"
          :assets="assets"
          :cover-asset-id="metadata.coverAssetId"
          :disabled="!editable"
          @insert="insertAsset"
          @cover="emit('coverChange', $event)"
          @upload="emit('upload', $event)"
        />
      </aside>
      <section class="workspace-panel editor-panel" aria-label="正文编辑器">
        <div class="editor-tools">
          <EditorToolbar :editor="editor" />
          <ElButton
            class="mt-3"
            size="small"
            :disabled="!bodyHtml"
            @click="copyLayout"
          >
            复制微信排版
          </ElButton>
          <div v-if="selectedImage" class="image-controls">
            <label
              >图片宽度
              <ElSelect
                :model-value="selectedImage.width"
                :disabled="!editable"
                aria-label="图片宽度"
                @update:model-value="changeImage({ width: $event })"
              >
                <ElOption
                  v-for="width in [50, 75, 100]"
                  :key="width"
                  :label="`${width}%`"
                  :value="width"
                />
              </ElSelect>
            </label>
            <label
              >替代文字
              <ElInput
                :model-value="selectedImage.alt"
                :disabled="!editable"
                aria-label="图片替代文字"
                @update:model-value="changeImage({ alt: $event })"
              />
            </label>
            <label
              >图片说明
              <ElInput
                :model-value="selectedImage.caption"
                :disabled="!editable"
                aria-label="图片说明"
                @update:model-value="changeImage({ caption: $event })"
              />
            </label>
          </div>
        </div>
        <EditorContent
          class="article-canvas"
          :style="canvasVariables"
          :editor="editor"
        />
      </section>
      <aside v-if="desktop" class="workspace-panel preview-panel">
        <h2 class="mb-3 font-semibold">手机预览</h2>
        <slot name="preview">
          <PhonePreview
            v-bind="metadata"
            :cover-url="coverUrl"
            :body-html="bodyHtml"
          />
        </slot>
      </aside>
    </div>
    <ElDrawer
      v-model="leftDrawer"
      title="主题与素材"
      direction="ltr"
      size="min(320px, 92vw)"
    >
      <ThemePanel
        :themes="themes"
        :model-value="themeId"
        :disabled="!editable"
        @update:model-value="emit('update:themeId', $event)"
      />
      <AssetPanel
        class="mt-6"
        :assets="assets"
        :cover-asset-id="metadata.coverAssetId"
        :disabled="!editable"
        @insert="insertAsset"
        @cover="emit('coverChange', $event)"
        @upload="emit('upload', $event)"
      />
    </ElDrawer>
    <ElDrawer v-model="rightDrawer" title="手机预览" size="min(430px, 100vw)">
      <slot name="preview">
        <PhonePreview
          v-bind="metadata"
          :cover-url="coverUrl"
          :body-html="bodyHtml"
        />
      </slot>
    </ElDrawer>
  </div>
</template>

<style scoped>
.layout-workspace {
  min-width: 0;
}

.workspace-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 16px;
  overflow-x: auto;
}

.workspace-panel {
  min-width: 0;
  padding: 14px;
  color: var(--el-text-color-primary);
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: var(--el-border-radius-base);
}

.side-panel {
  max-height: 1000px;
  overflow-y: auto;
}

.editor-panel {
  padding: 0;
}

.editor-tools {
  padding: 14px;
  border-bottom: 1px solid var(--el-border-color);
}

.image-controls {
  display: grid;
  gap: 10px;
  margin-top: 14px;
}

.image-controls label {
  display: grid;
  gap: 4px;
  font-size: 13px;
}

.article-canvas {
  min-height: 760px;
  padding: 24px;
  background: #fff;
}

.article-canvas :deep(.tiptap) {
  min-height: 700px;
  font: 16px/1.85 var(--article-font-family);
  color: var(--article-text);
  overflow-wrap: anywhere;
  outline: none;
}

.article-canvas :deep(p) {
  margin: 0 0 16px;
  color: var(--article-text);
}

.article-canvas :deep(h2) {
  padding-left: 12px;
  margin: 32px 0 16px;
  font-size: 22px;
  font-weight: bold;
  line-height: 1.4;
  color: var(--article-heading);
  border-left: 4px solid var(--article-accent);
}

.article-canvas :deep(h3) {
  margin: 24px 0 12px;
  font-size: 18px;
  font-weight: bold;
  line-height: 1.5;
  color: var(--article-heading);
}

.article-canvas :deep(blockquote) {
  padding: 12px 16px;
  margin: 20px 0;
  color: var(--article-text);
  background: var(--article-surface);
  border-left: 4px solid var(--article-accent);
}

.article-canvas :deep(ul),
.article-canvas :deep(ol) {
  padding-left: 24px;
  margin: 0 0 16px;
  color: var(--article-text);
}

.article-canvas :deep(ul) {
  list-style: disc;
}

.article-canvas :deep(ol) {
  list-style: decimal;
}

.article-canvas :deep(a) {
  color: var(--article-accent);
  text-decoration: underline;
}

.article-canvas :deep(hr) {
  margin: 28px 0;
  border: 0;
  border-top: 1px solid var(--article-border);
}

.article-canvas :deep(figure) {
  margin: 20px 0;
  color: var(--article-muted);
}

.article-canvas :deep(figure img) {
  display: block;
  max-width: 100%;
  height: auto;
  margin: 0 auto;
}

.article-canvas :deep(figcaption) {
  margin-top: 8px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--article-muted);
  text-align: center;
}

.article-canvas :deep(.ProseMirror-selectednode) {
  outline: 2px solid var(--el-color-primary);
}

.article-canvas :deep(p.is-editor-empty:first-child::before) {
  float: left;
  height: 0;
  color: var(--article-muted);
  pointer-events: none;
  content: attr(data-placeholder);
}

@media (min-width: 1280px) {
  .workspace-grid {
    grid-template-columns: 280px minmax(520px, 1fr) 420px;
  }
}
</style>
