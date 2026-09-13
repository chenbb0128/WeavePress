<script lang="ts" setup>
/* eslint-disable vue/html-closing-bracket-newline, vue/multiline-html-element-content-newline */
import type {
  Draft,
  DraftAsset,
  DraftStatus,
  DraftVersion,
  EditorDocument,
  EditorNode,
  PreflightResult,
  WeChatLayoutTheme,
  WeChatStatus,
} from '#/api';

import {
  computed,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from 'vue';
import {
  onBeforeRouteLeave,
  onBeforeRouteUpdate,
  useRoute,
  useRouter,
} from 'vue-router';
import { useAccess } from '@vben/access';
import { useEventListener } from '@vueuse/core';

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
  ElTooltip,
} from 'element-plus';

import {
  getDraftApi,
  getDraftAssetsApi,
  getDraftPreflightApi,
  getDraftVersionsApi,
  getWeChatLayoutThemesApi,
  getWeChatStatusApi,
  publishDraftApi,
  restoreDraftVersionApi,
  reviewDraftApi,
  submitDraftReviewApi,
  updateDraftApi,
  uploadDraftAssetApi,
} from '#/api';
import {
  documentFingerprint,
  normalizeDocument,
  renderPreviewHtml,
} from '#/components/wechat-layout/document';
import LayoutEditor from '#/components/wechat-layout/layout-editor.vue';
import PhonePreview from '#/components/wechat-layout/phone-preview.vue';

defineOptions({ name: 'DraftDetail' });
const route = useRoute();
const router = useRouter();
const { hasAccessByCodes } = useAccess();
const draft = ref<Draft>();
const versions = ref<DraftVersion[]>([]);
const themes = ref<WeChatLayoutTheme[]>([]);
const assets = ref<DraftAsset[]>([]);
const wechat = ref<WeChatStatus>({ appConfigured: false, enabled: false });
const preflight = ref<PreflightResult>({ issues: [], valid: false });
const loading = ref(true);
const submitting = ref(false);
const uploading = ref(false);
const conflict = ref(false);
const savedFingerprint = ref('');
let loadEpoch = 0;
let disposed = false;
let requests = new AbortController();
interface DraftContext {
  id: number;
  epoch: number;
  signal: AbortSignal;
}
function invalidateContext() {
  loadEpoch += 1;
  requests.abort();
  requests = new AbortController();
  submitting.value = false;
  uploading.value = false;
}
function isCurrent(context: DraftContext) {
  return (
    !disposed &&
    context.epoch === loadEpoch &&
    context.id === Number(route.params.id) &&
    !context.signal.aborted
  );
}
function captureContext(): DraftContext | undefined {
  if (
    disposed ||
    loading.value ||
    !draft.value ||
    draft.value.id !== Number(route.params.id)
  )
    return;
  return { id: draft.value.id, epoch: loadEpoch, signal: requests.signal };
}
const form = reactive({
  title: '',
  author: '',
  digest: '',
  changeNote: '',
  coverAssetId: undefined as number | undefined,
  editorDocument: { type: 'doc', content: [] } as EditorDocument,
  themeId: 'minimal-business',
});
const metadata = computed(() => ({
  title: form.title,
  author: form.author,
  digest: form.digest,
  coverAssetId: form.coverAssetId,
}));
const dirty = computed(
  () =>
    Boolean(savedFingerprint.value) &&
    documentFingerprint(form.editorDocument, form.themeId, metadata.value) !==
      savedFingerprint.value,
);
const migrationFailed = computed(() =>
  Boolean(draft.value && !draft.value.editorDocument),
);
const editable = computed(
  () =>
    draft.value?.status === 'editing' &&
    hasAccessByCodes(['draft:update']) &&
    !migrationFailed.value &&
    !submitting.value &&
    !uploading.value,
);
const cover = computed(() =>
  assets.value.find((asset) => asset.id === form.coverAssetId),
);
const previewHtml = computed(() => {
  if (migrationFailed.value || !dirty.value)
    return draft.value?.previewHtml ?? '';
  const theme = themes.value.find((item) => item.id === form.themeId);
  return theme
    ? renderPreviewHtml(
        form.editorDocument,
        theme,
        new Map(assets.value.map((asset) => [asset.id, asset.mediaUrl])),
      )
    : '';
});
const statusLabels: Record<DraftStatus, string> = {
  editing: '编辑中',
  in_review: '待审核',
  approved: '已通过',
  publishing: '发布中',
  published: '已写入草稿箱',
  publish_failed: '发布失败',
};

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
    editorDocument: normalizeDocument(
      value.editorDocument ?? { type: 'doc', content: [] },
    ),
    coverAssetId: value.coverAssetId,
    digest: value.digest,
    themeId: value.themeId || 'minimal-business',
    title: value.title,
  });
  savedFingerprint.value = documentFingerprint(
    form.editorDocument,
    form.themeId,
    metadata.value,
  );
  conflict.value = false;
}
async function load() {
  if (disposed) return;
  invalidateContext();
  const id = Number(route.params.id);
  const context = { id, epoch: loadEpoch, signal: requests.signal };
  loading.value = true;
  draft.value = undefined;
  savedFingerprint.value = '';
  try {
    const [
      draftData,
      versionData,
      status,
      preflightData,
      themeData,
      assetData,
    ] = await Promise.all([
      getDraftApi(id, context.signal),
      getDraftVersionsApi(id, context.signal),
      getWeChatStatusApi(context.signal),
      getDraftPreflightApi(id, context.signal),
      getWeChatLayoutThemesApi(context.signal),
      getDraftAssetsApi(id, context.signal),
    ]);
    if (!isCurrent(context)) return;
    draft.value = draftData;
    versions.value = versionData;
    wechat.value = status;
    preflight.value = preflightData;
    themes.value = themeData;
    assets.value = assetData;
    fillForm(draftData);
  } catch {
    if (isCurrent(context)) ElMessage.error('稿件加载失败，请刷新重试');
  } finally {
    if (isCurrent(context)) loading.value = false;
  }
}
async function refresh(context: DraftContext) {
  if (!isCurrent(context)) return;
  const updated = await getDraftApi(context.id, context.signal);
  if (!isCurrent(context)) return;
  draft.value = updated;
  fillForm(updated);
  await refreshSavedDetails(context);
}
async function refreshSavedDetails(context: DraftContext) {
  if (!isCurrent(context)) return;
  try {
    const [versionData, preflightData, assetData] = await Promise.all([
      getDraftVersionsApi(context.id, context.signal),
      getDraftPreflightApi(context.id, context.signal),
      getDraftAssetsApi(context.id, context.signal),
    ]);
    if (!isCurrent(context)) return;
    versions.value = versionData;
    preflight.value = preflightData;
    assets.value = assetData;
  } catch {
    if (!isCurrent(context)) return;
    preflight.value = { issues: [], valid: false };
    ElMessage.warning('稿件操作已成功，版本或预检信息刷新失败，请稍后刷新');
  }
}
function hasBody(nodes: EditorNode[] = []): boolean {
  return nodes.some(
    (node) =>
      node.type === 'image' ||
      Boolean(node.text?.trim()) ||
      hasBody(node.content),
  );
}
function reportSaveError(error: unknown, context: DraftContext) {
  if (!isCurrent(context)) return;
  const failure = error as
    | { code?: number; response?: { status?: number }; status?: number }
    | undefined;
  // RequestClient 将 HTTP 错误解包为响应正文；30001 对应 HTTP 409。
  if (
    failure?.code === 30001 ||
    (failure?.response?.status ?? failure?.status) === 409
  ) {
    conflict.value = true;
    ElMessage.warning('稿件已被其他人更新，可复制当前内容后刷新');
  } else {
    ElMessage.error('操作失败，当前排版已保留，请稍后重试');
  }
}
async function save() {
  if (!draft.value || !editable.value) return;
  const context = captureContext();
  if (!context) return;
  if (!form.title.trim() || !hasBody(form.editorDocument.content)) {
    ElMessage.warning('标题和正文不能为空');
    return;
  }
  submitting.value = true;
  try {
    const updated = await updateDraftApi(
      context.id,
      {
        author: form.author,
        changeNote: form.changeNote,
        editorDocument: normalizeDocument(form.editorDocument),
        themeId: form.themeId,
        coverAssetId: form.coverAssetId,
        digest: form.digest,
        expectedVersion: draft.value.currentVersion,
        title: form.title,
      },
      context.signal,
    );
    if (!isCurrent(context)) return;
    draft.value = updated;
    fillForm(updated);
    ElMessage.success('稿件已保存并生成新版本');
    await refreshSavedDetails(context);
  } catch (error) {
    reportSaveError(error, context);
  } finally {
    if (isCurrent(context)) submitting.value = false;
  }
}
async function upload(file: File) {
  if (!draft.value || !editable.value) return;
  const context = captureContext();
  if (!context) return;
  uploading.value = true;
  try {
    const asset = await uploadDraftAssetApi(context.id, file, context.signal);
    if (!isCurrent(context)) return;
    assets.value = [...assets.value, asset];
    ElMessage.success('图片上传成功');
  } catch {
    if (isCurrent(context)) ElMessage.error('图片上传失败，当前排版已保留');
  } finally {
    if (isCurrent(context)) uploading.value = false;
  }
}
async function submitReview() {
  if (!draft.value || !editable.value || dirty.value) return;
  const context = captureContext();
  if (!context) return;
  submitting.value = true;
  try {
    await submitDraftReviewApi(context.id, context.signal);
    if (!isCurrent(context)) return;
    ElMessage.success('稿件已提交审核');
    await refresh(context);
  } catch (error) {
    reportSaveError(error, context);
  } finally {
    if (isCurrent(context)) submitting.value = false;
  }
}
async function approve() {
  const context = captureContext();
  if (!context || submitting.value || uploading.value) return;
  submitting.value = true;
  try {
    try {
      await ElMessageBox.confirm(
        '确认该稿件可以写入微信公众号草稿箱吗？',
        '审核通过',
      );
    } catch {
      return;
    }
    if (!isCurrent(context)) return;
    await reviewDraftApi(context.id, true, '', context.signal);
    if (!isCurrent(context)) return;
    ElMessage.success('稿件已审核通过');
    await refresh(context);
  } catch (error) {
    reportSaveError(error, context);
  } finally {
    if (isCurrent(context)) submitting.value = false;
  }
}
async function reject() {
  const context = captureContext();
  if (!context || submitting.value || uploading.value) return;
  submitting.value = true;
  try {
    const result = await ElMessageBox.prompt(
      '请填写需要修改的内容',
      '退回稿件',
      {
        inputValidator: (value) => Boolean(value.trim()) || '请填写退回原因',
      },
    );
    if (!isCurrent(context)) return;
    await reviewDraftApi(context.id, false, result.value, context.signal);
    if (!isCurrent(context)) return;
    ElMessage.success('稿件已退回编辑');
    await refresh(context);
  } catch {
    // 用户取消操作。
  } finally {
    if (isCurrent(context)) submitting.value = false;
  }
}
async function publish() {
  const context = captureContext();
  if (!context || submitting.value || uploading.value) return;
  if (!form.coverAssetId) {
    ElMessage.warning('请先选择封面并保存');
    return;
  }
  submitting.value = true;
  try {
    try {
      await ElMessageBox.confirm(
        '系统将上传正文图片和封面，并创建微信公众号草稿；不会自动群发。',
        '写入公众号草稿箱',
      );
    } catch {
      return;
    }
    if (!isCurrent(context)) return;
    const job = await publishDraftApi(context.id, context.signal);
    if (!isCurrent(context)) return;
    ElMessage.success('发布任务已进入队列');
    await router.push(`/wechat/publish-jobs?job=${job.id}`);
  } catch (error) {
    reportSaveError(error, context);
  } finally {
    if (isCurrent(context)) submitting.value = false;
  }
}
async function restoreVersion(version: number) {
  if (!draft.value || !editable.value || version >= draft.value.currentVersion)
    return;
  const context = captureContext();
  if (!context) return;
  const expectedVersion = draft.value.currentVersion;
  submitting.value = true;
  try {
    try {
      await ElMessageBox.confirm(
        `将 v${version} 的内容复制为新版本，现有版本不会删除。${dirty.value ? '当前未保存修改将被替换。' : ''}`,
        '恢复历史版本',
        { type: 'warning' },
      );
    } catch {
      return;
    }
    if (!isCurrent(context)) return;
    const updated = await restoreDraftVersionApi(
      context.id,
      version,
      expectedVersion,
      context.signal,
    );
    if (!isCurrent(context)) return;
    draft.value = updated;
    fillForm(updated);
    ElMessage.success(`已从 v${version} 生成新版本`);
    await refreshSavedDetails(context);
  } catch (error) {
    reportSaveError(error, context);
  } finally {
    if (isCurrent(context)) submitting.value = false;
  }
}
async function confirmLeave() {
  if (!dirty.value) return true;
  try {
    await ElMessageBox.confirm('当前排版尚未保存，确认离开吗？', '未保存修改', {
      type: 'warning',
    });
    return true;
  } catch {
    return false;
  }
}
onBeforeRouteLeave(confirmLeave);
onBeforeRouteUpdate(confirmLeave);
useEventListener(window, 'beforeunload', (event) => {
  if (!dirty.value) return;
  event.preventDefault();
  event.returnValue = '';
});
watch(
  () => route.params.id,
  (id, previous) => {
    if (id === previous) return;
    if (id) void load();
    else invalidateContext();
  },
  { flush: 'sync' },
);
onBeforeUnmount(() => {
  disposed = true;
  invalidateContext();
});
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
              v-access:code="'draft:update'"
              :loading="submitting"
              :disabled="!editable"
              type="primary"
              @click="save"
              >保存新版本</ElButton
            >
            <ElTooltip
              v-if="draft.status === 'editing'"
              :disabled="!dirty"
              content="请先保存当前排版后再提交审核"
            >
              <span v-access:code="'draft:update'">
                <ElButton
                  :disabled="dirty || !editable"
                  type="warning"
                  @click="submitReview"
                  >提交审核</ElButton
                >
              </span>
            </ElTooltip>
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

      <ElAlert
        v-if="draft.migrationWarnings?.length"
        class="mt-4"
        :closable="false"
        title="旧稿件转换提示"
        type="warning"
      >
        <ul class="mt-2 list-disc pl-5">
          <li v-for="warning in draft.migrationWarnings" :key="warning">
            {{ warning }}
          </li>
        </ul>
      </ElAlert>
      <ElAlert
        v-if="migrationFailed"
        class="mt-4"
        :closable="false"
        title="旧稿件转换失败，当前为只读模式，保留原稿预览。"
        type="error"
      />
      <ElAlert
        v-if="conflict"
        class="mt-4"
        :closable="false"
        title="稿件已被其他人更新，可复制当前内容后刷新"
        type="warning"
      />
      <ElCard class="mt-4" shadow="never">
        <template #header>
          <div class="flex items-center gap-3">
            <strong>稿件内容</strong>
            <ElTag :type="dirty ? 'warning' : 'info'">{{
              dirty ? '有未保存修改，请先保存再提交审核' : '已保存'
            }}</ElTag>
            <span v-if="uploading" class="text-muted-foreground text-sm"
              >图片上传中…</span
            >
          </div>
        </template>
        <ElForm label-position="top">
          <ElFormItem label="标题">
            <ElInput
              v-model="form.title"
              :disabled="!editable"
              maxlength="64"
              show-word-limit
            />
          </ElFormItem>
          <div class="grid gap-3 sm:grid-cols-2">
            <ElFormItem label="作者">
              <ElInput
                v-model="form.author"
                :disabled="!editable"
                maxlength="8"
                show-word-limit
              />
            </ElFormItem>
            <ElFormItem label="封面素材">
              <ElSelect
                v-model="form.coverAssetId"
                :disabled="!editable"
                class="w-full"
                clearable
                placeholder="请选择稿件素材"
                @clear="form.coverAssetId = undefined"
              >
                <ElOption
                  v-for="asset in assets"
                  :key="asset.id"
                  :label="`素材 #${asset.id}`"
                  :value="asset.id"
                  :disabled="!asset.coverEligible"
                />
              </ElSelect>
            </ElFormItem>
          </div>
          <ElFormItem label="摘要">
            <ElInput
              v-model="form.digest"
              :disabled="!editable"
              maxlength="120"
              :rows="2"
              show-word-limit
              type="textarea"
            />
          </ElFormItem>
          <ElFormItem v-if="draft.status === 'editing'" label="本次修改说明">
            <ElInput
              v-model="form.changeNote"
              :disabled="!editable"
              maxlength="255"
              placeholder="例如：调整标题和段落结构"
            />
          </ElFormItem>
        </ElForm>
      </ElCard>
      <LayoutEditor
        class="mt-4"
        v-model:document="form.editorDocument"
        v-model:theme-id="form.themeId"
        :assets="assets"
        :editable="editable"
        :metadata="metadata"
        :themes="themes"
        @cover-change="form.coverAssetId = $event"
        @upload="upload"
      >
        <template #preview>
          <PhonePreview
            v-bind="metadata"
            :cover-url="cover?.mediaUrl"
            :body-html="previewHtml"
          />
        </template>
      </LayoutEditor>

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
                  v-access:code="'draft:update'"
                  :disabled="!editable"
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
