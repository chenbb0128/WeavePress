import type { App } from 'vue';

import type { AIAnalysis, AIGeneration, AIJob, Article } from '#/api';

import { createApp, nextTick } from 'vue';

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import ArticleDetail from '../articles/detail.vue';
import AIWorkbench from './workbench.vue';

const mocks = vi.hoisted(() => ({
  createDraftApi: vi.fn(),
  confirm: vi.fn(),
  getAIAnalysesApi: vi.fn(),
  getAIAnalysisApi: vi.fn(),
  getAIGenerationApi: vi.fn(),
  getAIJobApi: vi.fn(),
  getAIStatusApi: vi.fn(),
  getArticleApi: vi.fn(),
  push: vi.fn(),
  retryAIJobApi: vi.fn(),
  routeParams: {
    articleId: undefined as string | undefined,
    id: '7' as string | undefined,
  },
  startAIAnalysisApi: vi.fn(),
  startAIGenerationApi: vi.fn(),
}));

vi.mock('#/api', () => ({
  createDraftApi: mocks.createDraftApi,
  getAIAnalysesApi: mocks.getAIAnalysesApi,
  getAIAnalysisApi: mocks.getAIAnalysisApi,
  getAIGenerationApi: mocks.getAIGenerationApi,
  getAIJobApi: mocks.getAIJobApi,
  getAIStatusApi: mocks.getAIStatusApi,
  getArticleApi: mocks.getArticleApi,
  retryAIJobApi: mocks.retryAIJobApi,
  startAIAnalysisApi: mocks.startAIAnalysisApi,
  startAIGenerationApi: mocks.startAIGenerationApi,
}));

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: mocks.routeParams }),
  useRouter: () => ({ back: vi.fn(), push: mocks.push }),
}));

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>();
  return {
    ...actual,
    ElMessageBox: { confirm: mocks.confirm },
  };
});

const mountedApps: App[] = [];

function article(status: Article['status'] = 'ready'): Article {
  return {
    assets: [
      {
        byteSize: 1024,
        downloadStatus: 'completed',
        height: 600,
        id: 11,
        isCover: true,
        mediaType: 'image/jpeg',
        mediaUrl: '/media/safe.jpg',
        position: 0,
        width: 800,
      },
    ],
    author: '作者',
    blocks: [],
    canonicalUrl: 'https://example.com/article',
    createdAt: '2026-09-09T00:00:00Z',
    id: 7,
    language: 'zh-CN',
    originalUrl: 'https://example.com/article',
    publishedAt: '2026-09-09T00:00:00Z',
    sourceName: '示例来源',
    sourceType: 'web',
    status,
    title: '示例文章',
    updatedAt: '2026-09-09T00:00:00Z',
  };
}

function job(status: AIJob['status'], overrides: Partial<AIJob> = {}): AIJob {
  return {
    articleId: 7,
    attempts: 1,
    createdAt: '2026-09-09T00:00:00Z',
    errorCode: status === 'failed' ? 'AI_PROCESSING_FAILED' : undefined,
    errorMessage: status === 'failed' ? 'AI 任务处理失败' : undefined,
    finishedAt: status === 'completed' ? '2026-09-09T00:00:03Z' : null,
    id: 21,
    inputTokens: 120,
    manualRetries: 0,
    model: 'test-model',
    outputTokens: 80,
    parentJobId: null,
    promptVersion: 'analysis-v1',
    provider: 'test-provider',
    requestedBy: 1,
    retryable: status === 'failed',
    startedAt: '2026-09-09T00:00:00Z',
    status,
    totalTokens: 200,
    type: 'analysis',
    updatedAt: '2026-09-09T00:00:03Z',
    ...overrides,
  };
}

function analysis(
  currentJob: AIJob = job('completed'),
  overrides: Partial<AIAnalysis> = {},
): AIAnalysis {
  return {
    angles: [
      {
        id: 'A1',
        outline: ['背景', '影响'],
        thesis: '从行业影响切入',
        title: '行业影响',
      },
      {
        id: 'A2',
        outline: ['事实', '结论'],
        thesis: '从事实核查切入',
        title: '事实核查',
      },
      {
        id: 'A3',
        outline: ['风险', '建议'],
        thesis: '从风险控制切入',
        title: '风险控制',
      },
    ],
    articleId: 7,
    createdAt: '2026-09-09T00:00:03Z',
    facts: [
      {
        confidence: 'high',
        id: 'F1',
        sourceBlockIds: ['B1'],
        text: '事实一',
      },
    ],
    id: 31,
    job: currentJob,
    jobId: currentJob.id,
    quotes: [{ id: 'Q1', sourceBlockId: 'B2', text: '原文引用' }],
    risks: [],
    summary: '分析摘要',
    viewpoints: [
      {
        holder: '受访者',
        id: 'V1',
        sourceBlockIds: ['B3'],
        text: '观点一',
      },
    ],
    ...overrides,
  };
}

function generation(draftId?: number): AIGeneration {
  const generationJob = job('completed', {
    finishedAt: '2026-09-09T00:00:05Z',
    id: 41,
    inputTokens: 300,
    outputTokens: 500,
    promptVersion: 'generation-v1',
    startedAt: '2026-09-09T00:00:01Z',
    totalTokens: 800,
    type: 'generation',
  });
  return {
    additionalInstructions: '',
    analysisId: 31,
    angleId: 'A1',
    audience: '产品团队',
    blocks: [
      { level: 2, text: '受控标题', type: 'heading' },
      {
        factIds: ['F1'],
        text: '<img src=x onerror=alert(1)>',
        type: 'paragraph',
      },
      { assetId: 11, alt: '安全图片', type: 'image' },
      { assetId: 999, alt: '无效图片', type: 'image' },
      { items: ['列表一', '列表二'], type: 'list' },
    ],
    createdAt: '2026-09-09T00:00:01Z',
    digest: '生成摘要',
    draftId,
    id: 51,
    job: generationJob,
    jobId: generationJob.id,
    targetWords: 1000,
    title: '生成标题',
    tone: 'professional',
    updatedAt: '2026-09-09T00:00:05Z',
  };
}

function deferred<T>() {
  let reject!: (reason?: unknown) => void;
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    reject = promiseReject;
    resolve = promiseResolve;
  });
  return { promise, reject, resolve };
}

function mountComponent(component: Parameters<typeof createApp>[0]) {
  const host = document.createElement('div');
  document.body.append(host);
  const app = createApp(component);
  const errors: unknown[] = [];
  app.config.errorHandler = (error) => errors.push(error);
  mountedApps.push(app);
  app.mount(host);
  return { app, errors, host };
}

async function settle() {
  for (let index = 0; index < 5; index += 1) {
    await Promise.resolve();
    await nextTick();
  }
}

function buttonByText(host: HTMLElement, text: string) {
  return [...host.querySelectorAll('button')].find((button) =>
    button.textContent?.includes(text),
  );
}

describe('article detail AI entry', () => {
  beforeEach(() => {
    vi.resetAllMocks();
    mocks.routeParams.id = '7';
    mocks.routeParams.articleId = undefined;
  });

  afterEach(() => {
    for (const app of mountedApps.splice(0)) app.unmount();
    document.body.textContent = '';
  });

  it('loads article and AI status in parallel and opens enabled workbench', async () => {
    const articleRequest = deferred<Article>();
    const statusRequest = deferred<{
      enabled: boolean;
      model: string;
      provider: string;
    }>();
    mocks.getArticleApi.mockReturnValue(articleRequest.promise);
    mocks.getAIStatusApi.mockReturnValue(statusRequest.promise);

    const { host } = mountComponent(ArticleDetail);

    expect(mocks.getArticleApi).toHaveBeenCalledWith(7);
    expect(mocks.getAIStatusApi).toHaveBeenCalledOnce();

    articleRequest.resolve(article());
    statusRequest.resolve({
      enabled: true,
      model: 'test-model',
      provider: 'test-provider',
    });
    await settle();

    const button = buttonByText(host, 'AI 分析');
    expect(button?.disabled).toBe(false);
    button?.click();
    expect(mocks.push).toHaveBeenCalledWith('/ai/articles/7');
  });

  it.each([
    ['processing', true, '文章解析完成后可分析'],
    ['ready', false, 'AI 服务尚未配置'],
  ] as const)(
    'disables entry for article status %s and AI enabled %s',
    async (status, enabled, hint) => {
      mocks.getArticleApi.mockResolvedValue(article(status));
      mocks.getAIStatusApi.mockResolvedValue({
        enabled,
        model: '',
        provider: '',
      });

      const { host } = mountComponent(ArticleDetail);
      await settle();

      expect(buttonByText(host, 'AI 分析')?.disabled).toBe(true);
      expect(host.textContent).toContain(hint);
    },
  );

  it('keeps the article usable when AI status loading fails', async () => {
    mocks.getArticleApi.mockResolvedValue(article());
    mocks.getAIStatusApi.mockRejectedValue(new Error('status unavailable'));

    const { errors, host } = mountComponent(ArticleDetail);
    await settle();

    expect(errors).toEqual([]);
    expect(host.textContent).toContain('示例文章');
    expect(buttonByText(host, '生成微信稿件')).toBeDefined();
    expect(buttonByText(host, 'AI 分析')?.disabled).toBe(true);
    expect(host.textContent).toContain('AI 服务尚未配置');
  });
});

describe('ai workbench', () => {
  beforeEach(() => {
    vi.useRealTimers();
    vi.resetAllMocks();
    mocks.routeParams.id = undefined;
    mocks.routeParams.articleId = '7';
    mocks.confirm.mockResolvedValue('confirm');
    mocks.getArticleApi.mockResolvedValue(article());
    mocks.getAIStatusApi.mockResolvedValue({
      enabled: true,
      model: 'test-model',
      provider: 'test-provider',
    });
    mocks.getAIAnalysesApi.mockResolvedValue({
      items: [],
      page: 1,
      pageSize: 20,
      total: 0,
    });
  });

  it('loads the articleId route parameter', async () => {
    const { host } = mountComponent(AIWorkbench);
    await settle();

    expect(mocks.getArticleApi).toHaveBeenCalledWith(7);
    expect(host.textContent).not.toContain('文章 ID 无效');
  });

  it('unlocks generation and ignores its stale result after switching analysis', async () => {
    const latest = analysis();
    const olderJob = job('completed', { id: 20 });
    const older = analysis(olderJob, {
      createdAt: '2026-09-08T00:00:03Z',
      id: 30,
      jobId: olderJob.id,
      summary: '旧分析摘要',
    });
    mocks.getAIAnalysesApi.mockResolvedValue({
      items: [latest, older],
      page: 1,
      pageSize: 20,
      total: 2,
    });
    mocks.getAIAnalysisApi.mockImplementation((id: number) =>
      Promise.resolve(id === older.id ? older : latest),
    );
    const submission = deferred<{
      generation: AIGeneration;
      job: AIJob;
      reused: boolean;
    }>();
    mocks.startAIGenerationApi.mockReturnValue(submission.promise);

    const { host } = mountComponent(AIWorkbench);
    await settle();
    const audienceInput = host.querySelector<HTMLInputElement>(
      'input[placeholder="例如：产品经理"]',
    );
    expect(audienceInput).toBeTruthy();
    if (!audienceInput) return;
    audienceInput.value = '产品团队';
    audienceInput.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    buttonByText(host, '生成稿件')?.click();
    await settle();
    expect(mocks.startAIGenerationApi).toHaveBeenCalledOnce();

    host.querySelector<HTMLElement>('.w-72 .el-select__wrapper')?.click();
    await settle();
    const olderOption = [
      ...document.querySelectorAll<HTMLElement>('.el-select-dropdown__item'),
    ].find((option) => option.textContent?.includes('分析 #30'));
    expect(olderOption).toBeTruthy();
    olderOption?.click();
    await settle();

    expect(mocks.getAIAnalysisApi).toHaveBeenLastCalledWith(30);
    expect(host.textContent).toContain('旧分析摘要');
    expect(buttonByText(host, '生成稿件')?.disabled).toBe(false);

    const staleGeneration = generation(61);
    submission.resolve({
      generation: staleGeneration,
      job: staleGeneration.job as AIJob,
      reused: false,
    });
    await settle();
    expect(host.textContent).not.toContain('生成标题');
    expect(host.textContent).toContain('旧分析摘要');
  });

  it('cancels a pending generation before analysis selection settles', async () => {
    const latest = analysis();
    const olderJob = job('completed', { id: 20 });
    const older = analysis(olderJob, {
      createdAt: '2026-09-08T00:00:03Z',
      id: 30,
      jobId: olderJob.id,
      summary: '旧分析摘要',
    });
    const selection = deferred<AIAnalysis>();
    mocks.getAIAnalysesApi.mockResolvedValue({
      items: [latest, older],
      page: 1,
      pageSize: 20,
      total: 2,
    });
    mocks.getAIAnalysisApi.mockImplementation((id: number) =>
      id === older.id ? selection.promise : Promise.resolve(latest),
    );
    const submission = deferred<{
      generation: AIGeneration;
      job: AIJob;
      reused: boolean;
    }>();
    mocks.startAIGenerationApi.mockReturnValue(submission.promise);

    const { host } = mountComponent(AIWorkbench);
    await settle();
    const audienceInput = host.querySelector<HTMLInputElement>(
      'input[placeholder="例如：产品经理"]',
    );
    expect(audienceInput).toBeTruthy();
    if (!audienceInput) return;
    audienceInput.value = '产品团队';
    audienceInput.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    buttonByText(host, '生成稿件')?.click();
    await settle();

    host.querySelector<HTMLElement>('.w-72 .el-select__wrapper')?.click();
    await settle();
    const olderOption = [
      ...document.querySelectorAll<HTMLElement>('.el-select-dropdown__item'),
    ].find((option) => option.textContent?.includes('分析 #30'));
    expect(olderOption).toBeTruthy();
    olderOption?.click();
    await settle();

    expect(mocks.getAIAnalysisApi).toHaveBeenLastCalledWith(30);
    expect(buttonByText(host, '生成稿件')?.disabled).toBe(false);

    const staleGeneration = generation(61);
    submission.resolve({
      generation: staleGeneration,
      job: staleGeneration.job as AIJob,
      reused: false,
    });
    await settle();
    expect(host.textContent).not.toContain('生成标题');
    expect(buttonByText(host, '生成稿件')?.disabled).toBe(false);

    selection.reject(new Error('analysis unavailable'));
    await settle();
    expect(host.textContent).toContain('分析资料加载失败，请稍后重试');
    expect(host.textContent).not.toContain('生成标题');
    expect(buttonByText(host, '生成稿件')?.disabled).toBe(false);
  });

  afterEach(() => {
    for (const app of mountedApps.splice(0)) app.unmount();
    document.body.textContent = '';
    vi.useRealTimers();
  });

  it('confirms reanalysis and sends force true', async () => {
    const existing = analysis();
    mocks.getAIAnalysesApi.mockResolvedValue({
      items: [existing],
      page: 1,
      pageSize: 20,
      total: 1,
    });
    mocks.getAIAnalysisApi.mockResolvedValue(existing);
    mocks.startAIAnalysisApi.mockResolvedValue({
      job: job('completed'),
      reused: false,
    });

    const { host } = mountComponent(AIWorkbench);
    await settle();
    buttonByText(host, '重新分析')?.click();
    await settle();

    expect(mocks.confirm).toHaveBeenCalledOnce();
    expect(mocks.startAIAnalysisApi).toHaveBeenCalledWith(7, true);
  });

  it('polls only active analysis jobs every three seconds and clears on unmount', async () => {
    vi.useFakeTimers();
    mocks.startAIAnalysisApi
      .mockResolvedValueOnce({ job: job('completed'), reused: true })
      .mockResolvedValueOnce({ job: job('queued'), reused: false });

    const firstMount = mountComponent(AIWorkbench);
    await settle();
    buttonByText(firstMount.host, '开始分析')?.click();
    await settle();
    await vi.advanceTimersByTimeAsync(3000);
    await settle();
    expect(mocks.getAIJobApi).not.toHaveBeenCalled();
    firstMount.app.unmount();
    mountedApps.splice(mountedApps.indexOf(firstMount.app), 1);

    const secondMount = mountComponent(AIWorkbench);
    await settle();
    buttonByText(secondMount.host, '开始分析')?.click();
    await settle();

    mocks.getAIJobApi.mockResolvedValue(job('running'));
    await vi.advanceTimersByTimeAsync(2999);
    expect(mocks.getAIJobApi).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await settle();
    expect(mocks.getAIJobApi).toHaveBeenCalledWith(21);

    const callsBeforeUnmount = mocks.getAIJobApi.mock.calls.length;
    secondMount.app.unmount();
    mountedApps.splice(mountedApps.indexOf(secondMount.app), 1);
    await vi.advanceTimersByTimeAsync(3000);
    expect(mocks.getAIJobApi).toHaveBeenCalledTimes(callsBeforeUnmount);
  });

  it('prevents another analysis submission while the current job is active', async () => {
    mocks.startAIAnalysisApi.mockResolvedValue({
      job: job('queued'),
      reused: false,
    });

    const { host } = mountComponent(AIWorkbench);
    await settle();
    const button = buttonByText(host, '开始分析');
    button?.click();
    await settle();

    expect(button?.disabled).toBe(true);
    button?.click();
    await settle();
    expect(mocks.startAIAnalysisApi).toHaveBeenCalledOnce();
  });

  it('renders model blocks as text, warns about review, and gates draft navigation', async () => {
    const existing = analysis();
    mocks.getAIAnalysesApi.mockResolvedValue({
      items: [existing],
      page: 1,
      pageSize: 20,
      total: 1,
    });
    mocks.getAIAnalysisApi.mockResolvedValue(existing);
    const firstGeneration = generation();
    const secondGeneration = generation(61);
    mocks.startAIGenerationApi
      .mockResolvedValueOnce({
        generation: firstGeneration,
        job: firstGeneration.job,
        reused: false,
      })
      .mockResolvedValueOnce({
        generation: secondGeneration,
        job: secondGeneration.job,
        reused: false,
      });

    const { host } = mountComponent(AIWorkbench);
    await settle();
    const audienceInput = host.querySelector<HTMLInputElement>(
      'input[placeholder="例如：产品经理"]',
    );
    expect(audienceInput).toBeTruthy();
    if (audienceInput) {
      audienceInput.value = '产品团队';
      audienceInput.dispatchEvent(new Event('input', { bubbles: true }));
    }
    await settle();

    buttonByText(host, '生成稿件')?.click();
    await settle();

    const result = host.querySelector('.generated-blocks');
    expect(result?.textContent).toContain('<img src=x onerror=alert(1)>');
    expect(result?.querySelectorAll('img')).toHaveLength(1);
    expect(result?.querySelector('img')?.getAttribute('src')).toBe(
      '/media/safe.jpg',
    );
    expect(host.textContent).toContain('AI 结果不会自动审核或发布，需人工检查');
    expect(buttonByText(host, '打开微信稿件')).toBeUndefined();

    buttonByText(host, '重新生成')?.click();
    await settle();
    buttonByText(host, '打开微信稿件')?.click();
    expect(mocks.push).toHaveBeenCalledWith('/drafts/61');
  });

  it('rejects an invalid article ID before loading remote data', async () => {
    mocks.routeParams.articleId = 'not-a-number';

    const { host } = mountComponent(AIWorkbench);
    await settle();

    expect(host.textContent).toContain('文章 ID 无效');
    expect(mocks.getArticleApi).not.toHaveBeenCalled();
    expect(mocks.getAIStatusApi).not.toHaveBeenCalled();
  });

  it('reuses a failed request key but replaces it after a parameter change', async () => {
    const existing = analysis();
    mocks.getAIAnalysesApi.mockResolvedValue({
      items: [existing],
      page: 1,
      pageSize: 20,
      total: 1,
    });
    mocks.getAIAnalysisApi.mockResolvedValue(existing);
    mocks.startAIGenerationApi
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockRejectedValueOnce(new Error('network unavailable'))
      .mockResolvedValueOnce({
        generation: generation(),
        job: generation().job,
        reused: false,
      });

    const { host } = mountComponent(AIWorkbench);
    await settle();
    const audienceInput = host.querySelector<HTMLInputElement>(
      'input[placeholder="例如：产品经理"]',
    );
    expect(audienceInput).toBeTruthy();
    if (!audienceInput) return;

    audienceInput.value = '产品团队';
    audienceInput.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    buttonByText(host, '生成稿件')?.click();
    await settle();
    const firstKey =
      mocks.startAIGenerationApi.mock.calls[0]?.[1]?.idempotencyKey;

    buttonByText(host, '重试提交')?.click();
    await settle();
    const retryKey =
      mocks.startAIGenerationApi.mock.calls[1]?.[1]?.idempotencyKey;
    expect(retryKey).toBe(firstKey);

    audienceInput.value = '技术团队';
    audienceInput.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    audienceInput.value = '产品团队';
    audienceInput.dispatchEvent(new Event('input', { bubbles: true }));
    await settle();
    buttonByText(host, '重试提交')?.click();
    await settle();
    const changedKey =
      mocks.startAIGenerationApi.mock.calls[2]?.[1]?.idempotencyKey;

    expect(changedKey).not.toBe(firstKey);
  });
});
