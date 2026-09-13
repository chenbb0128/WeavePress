import type { EditorDocument, WeChatLayoutTheme } from '#/api/content';

import { describe, expect, it } from 'vitest';

import {
  documentFingerprint,
  normalizeDocument,
  renderPreviewHtml,
} from './document';

const theme: WeChatLayoutTheme = {
  id: 'clear-blue',
  name: '清爽蓝',
  preview: '#1677ff',
  tokens: {
    accent: '#1677ff',
    border: '#91caff',
    fontFamily: '-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif',
    heading: '#1f2937',
    muted: '#6b7280',
    surface: '#f0f7ff',
    text: '#1f2937',
  },
  version: 1,
};

const document: EditorDocument = {
  type: 'doc',
  content: [{ type: 'paragraph', content: [{ type: 'text', text: '正文' }] }],
};

const metadata = {
  author: '作者',
  coverAssetId: 7,
  digest: '摘要',
  title: '标题',
};

describe('微信排版文档工具', () => {
  it('normalizes a missing root content collection', () => {
    const value: EditorDocument = { type: 'doc' };

    expect(normalizeDocument(value)).toEqual({ content: [], type: 'doc' });
    expect(value).toEqual({ type: 'doc' });
  });

  it('renders escaped themed preview and draft asset URL', () => {
    const html = renderPreviewHtml(
      {
        type: 'doc',
        content: [
          {
            type: 'paragraph',
            content: [{ type: 'text', text: '<正文>' }],
          },
          {
            type: 'image',
            attrs: {
              draftAssetId: 18,
              width: 75,
              align: 'center',
              alt: '图',
              caption: '说明',
            },
          },
        ],
      },
      theme,
      new Map([[18, '/media/draft-assets/18?signed=1']]),
    );

    expect(html).toContain('&lt;正文&gt;');
    expect(html).toContain('/media/draft-assets/18?signed=1');
    expect(html).toContain('color:#1f2937');
    expect(html).not.toContain('<正文>');
  });

  it('omits unknown nodes and unsafe links or theme tokens', () => {
    const unsafeDocument = {
      type: 'doc',
      content: [
        {
          type: 'paragraph',
          content: [
            {
              type: 'text',
              text: '链接',
              marks: [
                {
                  type: 'link',
                  attrs: { href: 'javascript:alert(1)', title: '危险"标题' },
                },
              ],
            },
          ],
        },
        {
          type: 'script',
          content: [{ type: 'text', text: '不可见' }],
        },
      ],
    } as unknown as EditorDocument;
    const unsafeTheme: WeChatLayoutTheme = {
      ...theme,
      tokens: {
        ...theme.tokens,
        accent: 'red;position:fixed',
        fontFamily: 'sans-serif;background:url(javascript:alert(1))',
      },
    };

    const html = renderPreviewHtml(unsafeDocument, unsafeTheme, new Map());

    expect(html).toContain('链接');
    expect(html).not.toContain('javascript:');
    expect(html).not.toContain('position:fixed');
    expect(html).not.toContain('不可见');
  });

  it('uses a stable fingerprint for dirty checking', () => {
    expect(documentFingerprint(document, 'clear-blue', metadata)).toBe(
      documentFingerprint(
        structuredClone(document),
        'clear-blue',
        Object.assign({}, metadata),
      ),
    );
  });
});
