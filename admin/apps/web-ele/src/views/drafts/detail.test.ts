import type { Draft, DraftStatus } from '#/api';
import type { App } from 'vue';

import { createApp, nextTick } from 'vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import DraftDetail from './detail.vue';

const mocks = vi.hoisted(() => ({
  getDraftApi: vi.fn(),
  getDraftAssetsApi: vi.fn(),
  getDraftPreflightApi: vi.fn(),
  getDraftVersionsApi: vi.fn(),
  getWeChatLayoutThemesApi: vi.fn(),
  getWeChatStatusApi: vi.fn(),
  updateDraftApi: vi.fn(),
}));

vi.mock('#/api', () => ({
  ...mocks,
  publishDraftApi: vi.fn(),
  restoreDraftVersionApi: vi.fn(),
  reviewDraftApi: vi.fn(),
  submitDraftReviewApi: vi.fn(),
  uploadDraftAssetApi: vi.fn(),
}));
vi.mock('@vben/access', () => ({
  useAccess: () => ({ hasAccessByCodes: () => true }),
}));
vi.mock('vue-router', () => ({
  onBeforeRouteLeave: vi.fn(),
  onBeforeRouteUpdate: vi.fn(),
  useRoute: () => ({ params: { id: '1' } }),
  useRouter: () => ({ back: vi.fn(), push: vi.fn() }),
}));

const apps: App[] = [];
function legacyDraft(status: DraftStatus, migrationNeeded = true): Draft {
  return {
    id: 1,
    sourceArticleId: 1,
    title: '旧审核稿',
    author: '',
    digest: '',
    contentHtml: '<p>正文</p>',
    previewHtml: '<p>正文</p>',
    editorDocument: {
      type: 'doc',
      content: [
        { type: 'paragraph', content: [{ type: 'text', text: '正文' }] },
      ],
    },
    themeId: 'minimal-business',
    themeVersion: 1,
    migrationNeeded,
    currentVersion: 1,
    status,
    createdBy: 1,
    updatedBy: 1,
    createdAt: '2026-09-12T00:00:00Z',
    updatedAt: '2026-09-12T00:00:00Z',
  };
}
async function settle() {
  for (let i = 0; i < 8; i += 1) {
    await Promise.resolve();
    await nextTick();
  }
}
function button(host: ParentNode, text: string) {
  return [...host.querySelectorAll('button')].find((item) =>
    item.textContent?.includes(text),
  );
}
async function mountDraft(draft: Draft) {
  mocks.getDraftApi.mockResolvedValue(draft);
  const host = document.createElement('div');
  document.body.append(host);
  const app = createApp(DraftDetail);
  app.directive('access', () => {});
  apps.push(app);
  app.mount(host);
  await settle();
  return host;
}

describe('legacy reviewed draft migration', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    mocks.getDraftAssetsApi.mockResolvedValue([]);
    mocks.getDraftVersionsApi.mockResolvedValue([]);
    mocks.getDraftPreflightApi.mockResolvedValue({ valid: false, issues: [] });
    mocks.getWeChatLayoutThemesApi.mockResolvedValue([]);
    mocks.getWeChatStatusApi.mockResolvedValue({
      enabled: false,
      appConfigured: false,
    });
  });
  afterEach(() => {
    for (const app of apps.splice(0)) app.unmount();
    document.body.textContent = '';
  });

  it.each(['approved', 'publish_failed'] as const)(
    'explicitly saves %s legacy content and explains the new review',
    async (status) => {
      const original = legacyDraft(status);
      mocks.updateDraftApi.mockResolvedValue({
        ...original,
        status: 'editing',
        currentVersion: 2,
        migrationNeeded: false,
      });
      const host = await mountDraft(original);
      const migrate = button(host, '保存迁移稿并重新审核');
      expect(migrate?.disabled).toBe(false);
      expect(host.textContent).toContain('清除旧审核结论');
      migrate?.click();
      await settle();
      expect(document.querySelector('.el-message-box')?.textContent).toContain(
        '重新审核',
      );
      expect(mocks.updateDraftApi).not.toHaveBeenCalled();
      const confirm = document.querySelector<HTMLButtonElement>(
        '.el-message-box__btns .el-button--primary',
      );
      expect(confirm).not.toBeNull();
      confirm?.click();
      await settle();
      expect(mocks.updateDraftApi).toHaveBeenCalledWith(
        1,
        expect.objectContaining({
          editorDocument: original.editorDocument,
          themeId: 'minimal-business',
          expectedVersion: 1,
        }),
        expect.any(AbortSignal),
      );
      expect(host.textContent).toContain('v2');
      expect(button(host, '保存迁移稿并重新审核')).toBeUndefined();
      expect(button(host, '提交审核')).toBeDefined();
    },
  );

  it.each([
    'approved',
    'publish_failed',
    'in_review',
    'publishing',
    'published',
  ] as const)('keeps ordinary %s drafts locked', async (status) => {
    const host = await mountDraft(legacyDraft(status, false));
    expect(button(host, '保存迁移稿并重新审核')).toBeUndefined();
    expect(button(host, '保存新版本')).toBeUndefined();
  });
});
