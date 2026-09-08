<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type {
  Asset,
  Draft,
  DraftStatus,
  DraftVersion,
  PreflightResult,
  WeChatStatus,
} from '#/api';

import { computed, nextTick, onMounted, reactive, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import dayjs from 'dayjs';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElEmpty,
  ElForm,
  ElFormItem,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElSkeleton,
  ElTable,
  ElTableColumn,
  ElTag,
  ElTimeline,
  ElTimelineItem,
} from 'element-plus';

import {
  getDraftApi,
  getDraftPreflightApi,
  getDraftVersionsApi,
  getWeChatStatusApi,
  publishDraftApi,
  restoreDraftVersionApi,
  reviewDraftApi,
  submitDraftReviewApi,
  updateDraftApi,
} from '#/api';

defineOptions({ name: 'DraftDetail' });
const route = useRoute();
const router = useRouter();
const draft = ref<Draft>();
const versions = ref<DraftVersion[]>([]);
const wechat = ref<WeChatStatus>({ appConfigured: false, enabled: false });
const preflight = ref<PreflightResult>({ issues: [], valid: false });
const editorContainer = ref<HTMLElement>();
const loading = ref(true);
const submitting = ref(false);
const form = reactive({
  author: '',
  changeNote: '',
  contentHtml: '',
  coverAssetId: undefined as number | undefined,
  digest: '',
  title: '',
});
const statusLabels: Record<DraftStatus, string> = {
  editing: '编辑中',
  in_review: '待审核',
  approved: '已通过',
  publishing: '发布中',
  published: '已写入草稿箱',
  publish_failed: '发布失败',
};
const assets = computed(() =>
  (draft.value?.sourceArticle?.assets || []).filter(
    (asset) => asset.downloadStatus === 'completed',
  ),
);
const cover = computed(() =>
  assets.value.find((asset) => asset.id === form.coverAssetId),
);
const previewDocument = computed(
  () => `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src http: https: data:; style-src 'unsafe-inline'"><style>
body{max-width:680px;margin:0 auto;padding:28px 24px;color:#1f2937;font:16px/1.85 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}h2,h3,h4{line-height:1.4;margin:1.8em 0 .8em}img{display:block;max-width:100%;height:auto;margin:1.4em auto}figure{margin:1.4em 0}figcaption{text-align:center;color:#6b7280;font-size:13px}blockquote{margin:1.4em 0;padding:10px 18px;border-left:4px solid #07c160;background:#f6f7f8}pre{overflow:auto;padding:14px;background:#111827;color:#f9fafb;border-radius:6px}a{color:#2563eb}</style></head><body>${draft.value?.previewHtml || ''}</body></html>`,
);

function statusType(status: DraftStatus) {
  if (status === 'published' || status === 'approved') return 'success';
  if (status === 'publish_failed') return 'danger';
  if (status === 'in_review') return 'warning';
  return 'primary';
}
function fillForm(value: Draft) {
  Object.assign(form, {
    author: value.author,
    changeNote: '',
    contentHtml: value.contentHtml,
    coverAssetId: value.coverAssetId,
    digest: value.digest,
    title: value.title,
  });
}
async function load() {
  const id = Number(route.params.id);
  loading.value = true;
  try {
    const [draftData, versionData, status, preflightData] = await Promise.all([
      getDraftApi(id),
      getDraftVersionsApi(id),
      getWeChatStatusApi(),
      getDraftPreflightApi(id),
    ]);
    draft.value = draftData;
    versions.value = versionData;
    wechat.value = status;
    preflight.value = preflightData;
    fillForm(draftData);
  } finally {
    loading.value = false;
  }
}
async function refresh() {
  const id = Number(route.params.id);
  const [draftData, versionData, preflightData] = await Promise.all([
    getDraftApi(id),
    getDraftVersionsApi(id),
    getDraftPreflightApi(id),
  ]);
  draft.value = draftData;
  versions.value = versionData;
  preflight.value = preflightData;
  fillForm(draftData);
}
async function save() {
  if (!draft.value || !form.title.trim() || !form.contentHtml.trim()) {
    ElMessage.warning('标题和正文不能为空');
    return;
  }
  submitting.value = true;
  try {
    const updated = await updateDraftApi(draft.value.id, {
      author: form.author,
      changeNote: form.changeNote,
      contentHtml: form.contentHtml,
      coverAssetId: form.coverAssetId,
      digest: form.digest,
      expectedVersion: draft.value.currentVersion,
      title: form.title,
    });
    draft.value = updated;
    versions.value = await getDraftVersionsApi(updated.id);
    preflight.value = await getDraftPreflightApi(updated.id);
    fillForm(updated);
    ElMessage.success('稿件已保存并生成新版本');
  } finally {
    submitting.value = false;
  }
}
async function submitReview() {
  if (!draft.value) return;
  await submitDraftReviewApi(draft.value.id);
  ElMessage.success('稿件已提交审核');
  await refresh();
}
async function approve() {
  if (!draft.value) return;
  await ElMessageBox.confirm(
    '确认该稿件可以写入微信公众号草稿箱吗？',
    '审核通过',
  );
  await reviewDraftApi(draft.value.id, true);
  ElMessage.success('稿件已审核通过');
  await refresh();
}
async function reject() {
  if (!draft.value) return;
  try {
    const result = await ElMessageBox.prompt(
      '请填写需要修改的内容',
      '退回稿件',
      {
        inputValidator: (value) => Boolean(value.trim()) || '请填写退回原因',
      },
    );
    await reviewDraftApi(draft.value.id, false, result.value);
    ElMessage.success('稿件已退回编辑');
    await refresh();
  } catch {
    // 用户取消操作。
  }
}
async function publish() {
  if (!draft.value) return;
  if (!form.coverAssetId) {
    ElMessage.warning('请先选择封面并保存');
    return;
  }
  await ElMessageBox.confirm(
    '系统将上传正文图片和封面，并创建微信公众号草稿；不会自动群发。',
    '写入公众号草稿箱',
  );
  const job = await publishDraftApi(draft.value.id);
  ElMessage.success('发布任务已进入队列');
  await router.push(`/wechat/publish-jobs?job=${job.id}`);
}
function escapeHTML(value: string) {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('"', '&quot;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;');
}
function insertHTML(before: string, after = '', placeholder = '') {
  if (!draft.value || draft.value.status !== 'editing') return;
  const textarea = editorContainer.value?.querySelector('textarea');
  const start = textarea?.selectionStart ?? form.contentHtml.length;
  const end = textarea?.selectionEnd ?? start;
  const selected = form.contentHtml.slice(start, end) || placeholder;
  form.contentHtml = `${form.contentHtml.slice(0, start)}${before}${selected}${after}${form.contentHtml.slice(end)}`;
  nextTick(() => {
    const cursor = start + before.length + selected.length + after.length;
    textarea?.focus();
    textarea?.setSelectionRange(cursor, cursor);
  });
}
async function insertAsset(asset: Asset) {
  if (!draft.value || draft.value.status !== 'editing') return;
  try {
    const result = await ElMessageBox.prompt(
      '可选：填写图片说明，留空则只插入图片。',
      `插入素材 #${asset.id}`,
      { inputPlaceholder: '图片说明' },
    );
    const caption = result.value.trim();
    const escaped = escapeHTML(caption);
    insertHTML(
      `<figure><img data-weavepress-asset-id="${asset.id}" alt="${escaped}">`,
      `${caption ? `<figcaption>${escaped}</figcaption>` : ''}</figure>`,
    );
  } catch {
    // 用户取消操作。
  }
}
async function restoreVersion(version: number) {
  if (!draft.value || version >= draft.value.currentVersion) return;
  await ElMessageBox.confirm(
    `将 v${version} 的内容复制为新版本，现有版本不会删除。`,
    '恢复历史版本',
  );
  submitting.value = true;
  try {
    const updated = await restoreDraftVersionApi(
      draft.value.id,
      version,
      draft.value.currentVersion,
    );
    draft.value = updated;
    versions.value = await getDraftVersionsApi(updated.id);
    preflight.value = await getDraftPreflightApi(updated.id);
    fillForm(updated);
    ElMessage.success(`已从 v${version} 生成新版本`);
  } finally {
    submitting.value = false;
  }
}
function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MiB`;
}
onMounted(load);
</script>

<template>
  <div class="p-5">
    <ElSkeleton v-if="loading" :rows="12" animated />
    <template v-else-if="draft">
      <ElCard shadow="never">
        <div
          class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between"
        >
          <div>
            <div class="mb-3 flex flex-wrap items-center gap-2">
              <ElTag :type="statusType(draft.status)">{{
                statusLabels[draft.status]
              }}</ElTag>
              <ElTag effect="plain">v{{ draft.currentVersion }}</ElTag>
              <span class="text-muted-foreground text-sm"
                >稿件 #{{ draft.id }}</span
              >
            </div>
            <h1 class="text-2xl font-semibold">{{ draft.title }}</h1>
            <p class="text-muted-foreground mt-2">
              来源文章 #{{ draft.sourceArticleId }} · 更新于
              {{ dayjs(draft.updatedAt).format('YYYY-MM-DD HH:mm') }}
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <ElButton @click="router.back()">返回</ElButton>
            <ElButton @click="router.push(`/articles/${draft.sourceArticleId}`)"
              >查看来源</ElButton
            >
            <ElButton
              v-if="draft.status === 'editing'"
              :loading="submitting"
              type="primary"
              @click="save"
              >保存新版本</ElButton
            >
            <ElButton
              v-if="draft.status === 'editing'"
              type="warning"
              @click="submitReview"
              >提交审核</ElButton
            >
            <ElButton
              v-if="draft.status === 'in_review'"
              v-access:code="'draft:review'"
              type="success"
              @click="approve"
              >审核通过</ElButton
            >
            <ElButton
              v-if="draft.status === 'in_review'"
              v-access:code="'draft:review'"
              type="danger"
              plain
              @click="reject"
              >退回修改</ElButton
            >
            <ElButton
              v-if="draft.status === 'approved'"
              v-access:code="'draft:publish'"
              :disabled="!wechat.enabled || !preflight.valid"
              type="success"
              @click="publish"
              >写入公众号草稿箱</ElButton
            >
          </div>
        </div>
      </ElCard>

      <ElAlert
        v-if="!wechat.enabled"
        class="mt-4"
        :closable="false"
        title="微信公众号发布未启用；配置服务端 AppID/AppSecret 后即可写入草稿箱。"
        type="warning"
      />

      <ElAlert
        v-if="preflight.valid"
        class="mt-4"
        :closable="false"
        title="发布前检查已通过；当前已保存版本符合微信公众号草稿规则。"
        type="success"
      />
      <ElAlert
        v-else
        class="mt-4"
        :closable="false"
        title="发布前检查未通过，请处理下列问题后重新保存。"
        type="error"
      >
        <ul class="mt-2 list-disc pl-5">
          <li
            v-for="issue in preflight.issues"
            :key="`${issue.code}-${issue.field}`"
          >
            {{ issue.message }}（{{ issue.code }}）
          </li>
        </ul>
      </ElAlert>

      <div class="mt-4 grid gap-4 xl:grid-cols-2">
        <ElCard shadow="never">
          <template #header><strong>稿件内容</strong></template>
          <ElForm label-position="top">
            <ElFormItem label="标题">
              <ElInput
                v-model="form.title"
                :disabled="draft.status !== 'editing'"
                maxlength="64"
                show-word-limit
              />
            </ElFormItem>
            <div class="grid gap-3 sm:grid-cols-2">
              <ElFormItem label="作者">
                <ElInput
                  v-model="form.author"
                  :disabled="draft.status !== 'editing'"
                  maxlength="8"
                  show-word-limit
                />
              </ElFormItem>
              <ElFormItem label="封面素材">
                <ElSelect
                  v-model="form.coverAssetId"
                  :disabled="draft.status !== 'editing'"
                  class="w-full"
                  clearable
                  placeholder="请选择归档图片"
                >
                  <ElOption
                    v-for="asset in assets"
                    :key="asset.id"
                    :label="`素材 #${asset.id}${asset.isCover ? '（原文封面）' : ''}`"
                    :value="asset.id"
                  />
                </ElSelect>
              </ElFormItem>
            </div>
            <ElFormItem label="摘要">
              <ElInput
                v-model="form.digest"
                :disabled="draft.status !== 'editing'"
                maxlength="120"
                :rows="3"
                show-word-limit
                type="textarea"
              />
            </ElFormItem>
            <ElFormItem
              label="正文 HTML（图片使用 data-weavepress-asset-id 占位）"
            >
              <div class="w-full">
                <div
                  v-if="draft.status === 'editing'"
                  class="mb-2 flex flex-wrap gap-2"
                >
                  <ElButton
                    size="small"
                    @click="insertHTML('<p>', '</p>', '段落')"
                    >段落</ElButton
                  >
                  <ElButton
                    size="small"
                    @click="insertHTML('<h2>', '</h2>', '二级标题')"
                    >H2</ElButton
                  >
                  <ElButton
                    size="small"
                    @click="insertHTML('<h3>', '</h3>', '三级标题')"
                    >H3</ElButton
                  >
                  <ElButton
                    size="small"
                    @click="insertHTML('<strong>', '</strong>', '加粗文字')"
                    >加粗</ElButton
                  >
                  <ElButton
                    size="small"
                    @click="
                      insertHTML('<blockquote>', '</blockquote>', '引用内容')
                    "
                    >引用</ElButton
                  >
                  <ElButton size="small" @click="insertHTML('<br>')"
                    >换行</ElButton
                  >
                </div>
                <div ref="editorContainer">
                  <ElInput
                    v-model="form.contentHtml"
                    :disabled="draft.status !== 'editing'"
                    :rows="22"
                    resize="vertical"
                    type="textarea"
                  />
                </div>
              </div>
            </ElFormItem>
            <ElFormItem label="来源素材图库">
              <div
                v-if="assets.length"
                class="grid w-full gap-3 sm:grid-cols-2"
              >
                <div
                  v-for="asset in assets"
                  :key="asset.id"
                  class="rounded border p-3"
                >
                  <img
                    :src="asset.mediaUrl"
                    :alt="`素材 #${asset.id}`"
                    class="h-32 w-full rounded bg-gray-50 object-contain"
                  />
                  <div
                    class="mt-2 flex items-center justify-between gap-2 text-sm"
                  >
                    <span
                      >#{{ asset.id }} · {{ formatBytes(asset.byteSize) }}</span
                    >
                    <ElTag v-if="asset.isCover" size="small" type="success"
                      >原文封面</ElTag
                    >
                  </div>
                  <div
                    v-if="draft.status === 'editing'"
                    class="mt-2 flex gap-2"
                  >
                    <ElButton
                      size="small"
                      type="primary"
                      plain
                      @click="insertAsset(asset)"
                      >插入正文</ElButton
                    >
                    <ElButton size="small" @click="form.coverAssetId = asset.id"
                      >设为封面</ElButton
                    >
                  </div>
                </div>
              </div>
              <ElEmpty v-else description="暂无可用的归档素材" />
            </ElFormItem>
            <ElFormItem v-if="draft.status === 'editing'" label="本次修改说明">
              <ElInput
                v-model="form.changeNote"
                maxlength="255"
                placeholder="例如：调整标题和段落结构"
              />
            </ElFormItem>
          </ElForm>
        </ElCard>

        <div class="space-y-4">
          <ElCard shadow="never">
            <template #header
              ><strong>微信正文预览（最近保存版本）</strong></template
            >
            <div v-if="cover?.mediaUrl" class="mb-4">
              <p class="text-muted-foreground mb-2 text-sm">封面</p>
              <img
                :src="cover.mediaUrl"
                alt="稿件封面"
                class="max-h-48 rounded object-cover"
              />
            </div>
            <iframe
              :srcdoc="previewDocument"
              class="h-[680px] w-full rounded border"
              sandbox=""
              title="微信稿件预览"
            ></iframe>
          </ElCard>
        </div>
      </div>

      <div class="mt-4 grid gap-4 xl:grid-cols-2">
        <ElCard shadow="never">
          <template #header><strong>版本记录</strong></template>
          <ElTable :data="versions" max-height="420" row-key="id">
            <ElTableColumn label="版本" width="90">
              <template #default="{ row }">v{{ row.version }}</template>
            </ElTableColumn>
            <ElTableColumn label="修改说明" min-width="220" prop="changeNote" />
            <ElTableColumn label="保存时间" width="170">
              <template #default="{ row }">{{
                dayjs(row.createdAt).format('MM-DD HH:mm')
              }}</template>
            </ElTableColumn>
            <ElTableColumn
              v-if="draft.status === 'editing'"
              label="操作"
              width="110"
              fixed="right"
            >
              <template #default="{ row }">
                <ElButton
                  v-if="row.version < draft.currentVersion"
                  :loading="submitting"
                  link
                  type="primary"
                  @click="restoreVersion(row.version)"
                  >恢复</ElButton
                >
              </template>
            </ElTableColumn>
          </ElTable>
        </ElCard>
        <ElCard shadow="never">
          <template #header><strong>流程审计</strong></template>
          <ElTimeline v-if="draft.events?.length">
            <ElTimelineItem
              v-for="event in [...draft.events].reverse()"
              :key="event.id"
              :timestamp="dayjs(event.createdAt).format('MM-DD HH:mm:ss')"
              placement="top"
            >
              <strong>{{
                statusLabels[event.toStatus] || event.toStatus
              }}</strong>
              <p class="text-muted-foreground mt-1">{{ event.note }}</p>
            </ElTimelineItem>
          </ElTimeline>
          <ElEmpty v-else description="暂无流程记录" />
        </ElCard>
      </div>
    </template>
    <ElEmpty v-else description="稿件不存在" />
  </div>
</template>
