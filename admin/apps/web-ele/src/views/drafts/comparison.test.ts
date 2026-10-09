import type { App } from 'vue';

import type { Article, EditorDocument } from '#/api';

import { createApp, h, nextTick, ref } from 'vue';

import { afterEach, describe, expect, it, vi } from 'vitest';

import {
  applyRewrite,
  comparisonSourceBlocks,
  highlightText,
  qualityBlocks,
} from './comparison';
import DraftComparison from './comparison.vue';

const mocks = vi.hoisted(() => ({
  checkAIQualityApi: vi.fn(),
  rewriteAIBlockApi: vi.fn(),
}));
vi.mock('#/api', () => mocks);

const source: Article = {
  id: 1,
  title: '原文标题',
  author: '',
  sourceName: '来源',
  sourceType: 'wechat',
  status: 'ready',
  blocks: [{ type: 'paragraph', text: '原作者自述经历' }],
  plainText: '原作者自述经历',
  canonicalUrl: '',
  originalUrl: '',
  language: 'zh',
  createdAt: '',
  updatedAt: '',
};
const issue = {
  code: 'AUTHOR_ATTRIBUTION',
  severity: 'warning',
  message: '经历未归因',
  blockIndex: 0,
  excerpt: '我住院了',
  sourceBlockId: 'B1',
  sourceExcerpt: '自述经历',
};
const apps: App[] = [];
function button(host: ParentNode, text: string) {
  return [...host.querySelectorAll('button')].find((item) =>
    item.textContent?.includes(text),
  );
}
async function settle() {
  for (let i = 0; i < 8; i += 1) {
    await Promise.resolve();
    await nextTick();
  }
}
async function mountComparison() {
  const document = ref<EditorDocument>({
    type: 'doc',
    content: [
      { type: 'paragraph', content: [{ type: 'text', text: '我住院了' }] },
    ],
  });
  const title = ref('当前标题');
  const host = window.document.createElement('div');
  window.document.body.append(host);
  const app = createApp({
    setup: () => () =>
      h(DraftComparison, {
        article: source,
        assets: [],
        document: document.value,
        title: title.value,
        editable: true,
        canCheck: true,
        canRewrite: true,
        'onUpdate:document': (value) => {
          document.value = value;
        },
        'onUpdate:title': (value) => {
          title.value = value;
        },
      }),
  });
  apps.push(app);
  app.mount(host);
  await settle();
  return { host, document, title };
}

afterEach(() => {
  for (const app of apps.splice(0)) app.unmount();
  window.document.body.textContent = '';
  vi.resetAllMocks();
});

describe('draft comparison', () => {
  it('discards a semantic report after a title edit', async () => {
    let resolve!: (value: unknown) => void;
    mocks.checkAIQualityApi
      .mockResolvedValueOnce({
        issues: [],
        semanticChecked: false,
        totalTokens: 0,
      })
      .mockReturnValueOnce(
        new Promise((done) => {
          resolve = done;
        }),
      );
    const state = await mountComparison();
    button(state.host, '检查当前内容')?.click();
    await settle();
    state.title.value = '手工修改的标题';
    await settle();
    resolve({ issues: [issue], semanticChecked: true, totalTokens: 12 });
    await settle();
    expect(state.host.textContent).not.toContain('经历未归因');
    expect(state.host.textContent).toContain('内容已变更');
  });

  it('discards a rewrite after a manual paragraph edit', async () => {
    let resolve!: (value: unknown) => void;
    mocks.checkAIQualityApi.mockResolvedValue({
      issues: [issue],
      semanticChecked: false,
      totalTokens: 0,
    });
    mocks.rewriteAIBlockApi.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    const state = await mountComparison();
    button(state.host, '快速检查')?.click();
    await settle();
    button(state.host, '改写此段')?.click();
    await settle();
    state.document.value = {
      type: 'doc',
      content: [
        {
          type: 'paragraph',
          content: [{ type: 'text', text: '保留手工修改' }],
        },
      ],
    };
    await settle();
    resolve({
      targetIndex: 0,
      type: 'paragraph',
      text: '过期的建议',
      totalTokens: 8,
    });
    await settle();
    expect(button(state.host, '应用建议')).toBeUndefined();
    expect(state.host.textContent).toContain('保留手工修改');
    expect(state.host.textContent).not.toContain('过期的建议');
  });

  it('keeps rule findings when the model review fails', async () => {
    mocks.checkAIQualityApi
      .mockResolvedValueOnce({
        issues: [issue],
        semanticChecked: false,
        totalTokens: 0,
      })
      .mockRejectedValueOnce(new Error('timeout'));
    const { host } = await mountComparison();
    button(host, '检查当前内容')?.click();
    await settle();
    expect(host.textContent).toContain('经历未归因');
    expect(host.textContent).toContain('已保留快速检查结果');
    expect(host.textContent).toContain('事实与身份归因尚未核对');
  });

  it('highlights whitespace-normalized evidence without breaking emoji', () => {
    expect(highlightText('前🙂 原 文后', '🙂原文')).toEqual([
      { matched: false, text: '前' },
      { matched: true, text: '🙂 原 文' },
      { matched: false, text: '后' },
    ]);
  });

  it('uses stable fallback source ids for incomplete legacy blocks', () => {
    expect(
      comparisonSourceBlocks({
        ...source,
        blocks: [
          { type: 'paragraph', text: '尾部' },
          { type: 'image', assetId: 7 },
        ],
      }),
    ).toEqual([
      { type: 'paragraph', text: '原作者自述经历', id: 'B1' },
      { type: 'image', assetId: 7, id: 'B2' },
    ]);
  });

  it('keeps image positions and untouched formatting when applying a paraphrase', () => {
    const document: EditorDocument = {
      type: 'doc',
      content: [
        {
          type: 'image',
          attrs: {
            align: 'center',
            alt: '',
            caption: '',
            draftAssetId: 7,
            width: 100,
          },
        },
        {
          type: 'blockquote',
          content: [
            { type: 'paragraph', content: [{ type: 'text', text: '原话' }] },
          ],
        },
        {
          type: 'paragraph',
          content: [
            { type: 'text', text: '保留加粗', marks: [{ type: 'bold' }] },
          ],
        },
      ],
    };
    expect(qualityBlocks(document).map((block) => block.type)).toEqual([
      'image',
      'quote',
      'paragraph',
    ]);
    const result = applyRewrite(document, 1, '转述文字', 'paragraph');
    expect(result?.content?.[1]).toEqual({
      type: 'paragraph',
      content: [{ type: 'text', text: '转述文字' }],
    });
    expect(result?.content?.[0]?.attrs).toEqual({
      align: 'center',
      alt: '',
      caption: '',
      draftAssetId: 7,
      width: 100,
    });
    expect(result?.content?.[2]?.content?.[0]?.marks).toEqual([
      { type: 'bold' },
    ]);
    expect(document.content?.[1]?.type).toBe('blockquote');
  });
});
