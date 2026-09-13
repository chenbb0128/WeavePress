<script lang="ts" setup>
import type { Editor } from '@tiptap/vue-3';
import type {} from '@tiptap/extension-link';
import type {} from '@tiptap/extension-underline';
import type {} from '@tiptap/starter-kit';

import { computed } from 'vue';

import { ElButton, ElMessageBox } from 'element-plus';

const props = defineProps<{
  editor?: Editor | null;
}>();

const disabled = computed(() => !props.editor || !props.editor.isEditable);

function command(run: (editor: Editor) => void) {
  if (props.editor && props.editor.isEditable) run(props.editor);
}

function toggleBold() {
  command((editor) => editor.chain().focus().toggleBold().run());
}

function toggleItalic() {
  command((editor) => editor.chain().focus().toggleItalic().run());
}

function toggleUnderline() {
  command((editor) => editor.chain().focus().toggleUnderline().run());
}

function toggleHeading(level: 2 | 3) {
  command((editor) => editor.chain().focus().toggleHeading({ level }).run());
}

function toggleBlockquote() {
  command((editor) => editor.chain().focus().toggleBlockquote().run());
}

function toggleBulletList() {
  command((editor) => editor.chain().focus().toggleBulletList().run());
}

function toggleOrderedList() {
  command((editor) => editor.chain().focus().toggleOrderedList().run());
}

function insertHorizontalRule() {
  command((editor) => editor.chain().focus().setHorizontalRule().run());
}

function isHTTPLink(value: string): boolean {
  const href = value.trim();
  if (!/^https?:\/\/\S+$/i.test(href)) return false;
  try {
    const url = new URL(href);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

async function setLink() {
  if (!props.editor || !props.editor.isEditable) return;
  try {
    const result = await ElMessageBox.prompt(
      '请输入 HTTP(S) 链接',
      '设置链接',
      {
        inputPattern: /^https?:\/\/\S+$/i,
        inputValue: props.editor.getAttributes('link').href || '',
        inputErrorMessage: '链接必须以 http:// 或 https:// 开头',
      },
    );
    const href = result.value.trim();
    if (!isHTTPLink(href)) return;
    props.editor
      .chain()
      .focus()
      .extendMarkRange('link')
      .setLink({ href })
      .run();
  } catch {
    // 用户取消输入。
  }
}

function unsetLink() {
  command((editor) =>
    editor.chain().focus().extendMarkRange('link').unsetLink().run(),
  );
}
</script>

<template>
  <div class="flex flex-wrap gap-2" role="toolbar" aria-label="正文排版工具栏">
    <ElButton :disabled="disabled" size="small" @click="toggleBold"
      >粗体</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleItalic"
      >斜体</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleUnderline"
      >下划线</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleHeading(2)"
      >H2</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleHeading(3)"
      >H3</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleBlockquote"
      >引用</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleBulletList"
      >无序列表</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="toggleOrderedList"
      >有序列表</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="insertHorizontalRule"
      >分割线</ElButton
    >
    <ElButton :disabled="disabled" size="small" @click="setLink">链接</ElButton>
    <ElButton :disabled="disabled" size="small" @click="unsetLink"
      >取消链接</ElButton
    >
  </div>
</template>
