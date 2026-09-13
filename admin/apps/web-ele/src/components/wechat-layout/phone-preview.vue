<script lang="ts" setup>
import { computed } from 'vue';

const props = defineProps<{
  author: string;
  bodyHtml: string;
  coverUrl?: string;
  digest: string;
  title: string;
}>();
function escapeHTML(value: string) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;');
}
const srcdoc = computed(() => {
  const cover =
    props.coverUrl && /^(?:https?:\/\/|\/(?!\/))/i.test(props.coverUrl)
      ? `<img class="cover" src="${escapeHTML(props.coverUrl)}" alt="稿件封面">`
      : '';
  return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src https: http: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><meta name="viewport" content="width=device-width, initial-scale=1"><style>body{margin:0;padding:24px 20px;background:#fff;color:#1f2937;font:16px/1.85 system-ui,sans-serif;overflow-wrap:anywhere}h1{font-size:22px;line-height:1.4;margin:0 0 12px}.author,.digest{color:#6b7280;font-size:14px}.cover{display:block;width:100%;height:auto;margin:16px 0}img{max-width:100%}</style></head><body><h1>${escapeHTML(props.title)}</h1><p class="author">${escapeHTML(props.author)}</p><p class="digest">${escapeHTML(props.digest)}</p>${cover}${props.bodyHtml}</body></html>`;
});
</script>

<template>
  <div class="phone-frame">
    <iframe :srcdoc="srcdoc" sandbox="" title="微信稿件手机预览"></iframe>
  </div>
</template>

<style scoped>
.phone-frame {
  width: 390px;
  max-width: 100%;
  height: 760px;
  margin: 0 auto;
  overflow: hidden;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 24px;
}

iframe {
  display: block;
  width: 100%;
  height: 100%;
  border: 0;
}
</style>
