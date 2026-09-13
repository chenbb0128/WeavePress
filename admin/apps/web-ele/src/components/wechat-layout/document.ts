import type {
  EditorDocument,
  EditorImageAttrs,
  EditorMark,
  EditorNode,
  WeChatLayoutTheme,
} from '#/api/content';

export interface DraftMetadata {
  author: string;
  coverAssetId?: number;
  digest: string;
  title: string;
}

const DEFAULT_COLORS = {
  accent: '#1677ff',
  border: '#d9d9d9',
  heading: '#1f2937',
  muted: '#6b7280',
  surface: '#f7f8fa',
  text: '#1f2937',
};
const DEFAULT_FONT_FAMILY =
  '-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif';
const SAFE_COLOR = /^#[\dA-Fa-f]{3}(?:[\dA-Fa-f]{3}|[\dA-Fa-f]{5})?$/;
const SAFE_FONT_FAMILY = /^[\w\s'",-]+$/;
const IMAGE_ATTRS = ['align', 'alt', 'caption', 'draftAssetId', 'width'];

export function normalizeDocument(value: EditorDocument): EditorDocument {
  const document = value as unknown as Record<string, unknown>;
  return {
    content: normalizeChildren(document.content, 'doc'),
    type: 'doc',
  };
}

export function documentFingerprint(
  document: EditorDocument,
  themeId: string,
  metadata: DraftMetadata,
): string {
  return JSON.stringify(
    canonicalize({
      document: normalizeDocument(document),
      metadata,
      themeId,
    }),
  );
}

export function renderPreviewHtml(
  document: EditorDocument,
  theme: WeChatLayoutTheme,
  assetURLs: Map<number, string>,
): string {
  const tokens = safeThemeTokens(theme.tokens);
  const content = (document.content ?? [])
    .map((node) => renderNode(node, tokens, assetURLs))
    .join('');
  const style = `color:${tokens.text};font-family:${tokens.fontFamily};font-size:16px;line-height:1.85`;

  return `<article style="${escapeHTML(style)}">${content}</article>`;
}

type ParentNodeType = 'doc' | EditorNode['type'];

function normalizeChildren(
  value: unknown,
  parent: ParentNodeType,
): EditorNode[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap((item) => {
    const node = normalizeNode(item, parent);
    return node ? [node] : [];
  });
}

function normalizeNode(
  value: unknown,
  parent: ParentNodeType,
): EditorNode | undefined {
  if (!isRecord(value) || typeof value.type !== 'string') return undefined;
  const type = value.type as EditorNode['type'];
  if (!allowsChild(parent, type)) return undefined;

  switch (type) {
    case 'paragraph':
    case 'blockquote':
    case 'bulletList':
    case 'orderedList':
    case 'listItem':
      return { content: normalizeChildren(value.content, type), type };
    case 'heading': {
      const attrs = isRecord(value.attrs) ? value.attrs : {};
      if (attrs.level !== 2 && attrs.level !== 3) return undefined;
      return {
        attrs: { level: attrs.level },
        content: normalizeChildren(value.content, type),
        type,
      };
    }
    case 'text': {
      if (typeof value.text !== 'string') return undefined;
      const marks = normalizeMarks(value.marks);
      return {
        ...(marks.length > 0 ? { marks } : {}),
        text: value.text,
        type,
      };
    }
    case 'hardBreak':
    case 'horizontalRule':
      return { type };
    case 'image': {
      const attrs = normalizeImageAttrs(value.attrs);
      return attrs ? { attrs, type } : undefined;
    }
    default:
      return undefined;
  }
}

function normalizeMarks(value: unknown): EditorMark[] {
  if (!Array.isArray(value)) return [];
  return value.flatMap<EditorMark>((item): EditorMark[] => {
    if (!isRecord(item) || typeof item.type !== 'string') return [];
    if (
      item.type === 'bold' ||
      item.type === 'italic' ||
      item.type === 'underline'
    ) {
      return [{ type: item.type }];
    }
    if (item.type !== 'link' || !isRecord(item.attrs)) return [];
    const { href, title } = item.attrs;
    if (typeof href !== 'string' || !isSafeHTTPURL(href)) return [];
    return [
      {
        attrs: {
          href,
          ...(typeof title === 'string' ? { title } : {}),
        },
        type: 'link' as const,
      },
    ];
  });
}

function normalizeImageAttrs(value: unknown): EditorImageAttrs | undefined {
  if (!isRecord(value)) return undefined;
  const draftAssetId = value.draftAssetId;
  if (!Number.isSafeInteger(draftAssetId) || Number(draftAssetId) <= 0) {
    return undefined;
  }
  const width =
    value.width === 50 || value.width === 75 || value.width === 100
      ? value.width
      : 100;
  return {
    align: 'center',
    alt: typeof value.alt === 'string' ? value.alt : '',
    caption: typeof value.caption === 'string' ? value.caption : '',
    draftAssetId: Number(draftAssetId),
    width,
  };
}

function allowsChild(parent: ParentNodeType, child: EditorNode['type']) {
  switch (parent) {
    case 'doc':
    case 'blockquote':
    case 'listItem':
      return (
        child === 'paragraph' ||
        child === 'heading' ||
        child === 'blockquote' ||
        child === 'bulletList' ||
        child === 'orderedList' ||
        child === 'image' ||
        child === 'horizontalRule'
      );
    case 'paragraph':
    case 'heading':
      return child === 'text' || child === 'hardBreak';
    case 'bulletList':
    case 'orderedList':
      return child === 'listItem';
    default:
      return false;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === 'object' && !Array.isArray(value);
}

function canonicalize(value: unknown): unknown {
  if (Array.isArray(value)) return value.map(canonicalize);
  if (value && typeof value === 'object') {
    return Object.fromEntries(
      Object.entries(value)
        .filter(([, item]) => item !== undefined)
        .sort(([left], [right]) => left.localeCompare(right))
        .map(([key, item]) => [key, canonicalize(item)]),
    );
  }
  return value;
}

function escapeHTML(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&#39;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;');
}

function safeColor(value: string, fallback: string): string {
  return SAFE_COLOR.test(value) ? value : fallback;
}

function safeThemeTokens(tokens: WeChatLayoutTheme['tokens']) {
  return {
    accent: safeColor(tokens.accent, DEFAULT_COLORS.accent),
    border: safeColor(tokens.border, DEFAULT_COLORS.border),
    fontFamily:
      tokens.fontFamily.length <= 160 &&
      SAFE_FONT_FAMILY.test(tokens.fontFamily)
        ? tokens.fontFamily
        : DEFAULT_FONT_FAMILY,
    heading: safeColor(tokens.heading, DEFAULT_COLORS.heading),
    muted: safeColor(tokens.muted, DEFAULT_COLORS.muted),
    surface: safeColor(tokens.surface, DEFAULT_COLORS.surface),
    text: safeColor(tokens.text, DEFAULT_COLORS.text),
  };
}

type SafeThemeTokens = ReturnType<typeof safeThemeTokens>;

function renderNode(
  node: EditorNode,
  tokens: SafeThemeTokens,
  assetURLs: Map<number, string>,
): string {
  switch (node.type) {
    case 'paragraph':
      return styledElement(
        'p',
        'margin:0 0 16px',
        renderChildren(node, tokens, assetURLs),
      );
    case 'heading': {
      const level = headingLevel(node);
      if (!level) return '';
      const style =
        level === 2
          ? `margin:32px 0 16px;padding-left:12px;border-left:4px solid ${tokens.accent};color:${tokens.heading};font-size:22px;line-height:1.4`
          : `margin:24px 0 12px;color:${tokens.heading};font-size:18px;line-height:1.5`;
      return styledElement(
        `h${level}`,
        style,
        renderChildren(node, tokens, assetURLs),
      );
    }
    case 'blockquote':
      return styledElement(
        'blockquote',
        `margin:20px 0;padding:12px 16px;border-left:4px solid ${tokens.accent};background:${tokens.surface};color:${tokens.text}`,
        renderChildren(node, tokens, assetURLs),
      );
    case 'bulletList':
      return styledElement(
        'ul',
        'margin:0 0 16px;padding-left:24px',
        renderChildren(node, tokens, assetURLs),
      );
    case 'orderedList':
      return styledElement(
        'ol',
        'margin:0 0 16px;padding-left:24px',
        renderChildren(node, tokens, assetURLs),
      );
    case 'listItem':
      return `<li>${renderChildren(node, tokens, assetURLs)}</li>`;
    case 'horizontalRule':
      return styledElement(
        'hr',
        `margin:28px 0;border:0;border-top:1px solid ${tokens.border}`,
      );
    case 'hardBreak':
      return '<br>';
    case 'text':
      return renderText(node, tokens);
    case 'image':
      return renderImage(node, tokens, assetURLs);
    default:
      return '';
  }
}

function renderChildren(
  node: EditorNode,
  tokens: SafeThemeTokens,
  assetURLs: Map<number, string>,
): string {
  return (node.content ?? [])
    .map((child) => renderNode(child, tokens, assetURLs))
    .join('');
}

function headingLevel(node: EditorNode): 2 | 3 | undefined {
  if (!node.attrs || !('level' in node.attrs)) return undefined;
  return node.attrs.level === 2 || node.attrs.level === 3
    ? node.attrs.level
    : undefined;
}

function renderText(node: EditorNode, tokens: SafeThemeTokens): string {
  let html = escapeHTML(node.text ?? '');
  for (const mark of node.marks ?? []) {
    html = renderMark(mark, html, tokens);
  }
  return html;
}

function renderMark(
  mark: EditorMark,
  html: string,
  tokens: SafeThemeTokens,
): string {
  switch (mark.type) {
    case 'bold':
      return `<strong>${html}</strong>`;
    case 'italic':
      return `<em>${html}</em>`;
    case 'underline':
      return `<u>${html}</u>`;
    case 'link': {
      const href = mark.attrs?.href;
      if (typeof href !== 'string' || !isSafeHTTPURL(href)) return html;
      const title = mark.attrs?.title;
      const titleAttribute =
        typeof title === 'string' ? ` title="${escapeHTML(title)}"` : '';
      return `<a href="${escapeHTML(href)}"${titleAttribute} style="color:${tokens.accent};text-decoration:underline">${html}</a>`;
    }
    default:
      return html;
  }
}

function renderImage(
  node: EditorNode,
  tokens: SafeThemeTokens,
  assetURLs: Map<number, string>,
): string {
  const attrs = imageAttrs(node);
  if (!attrs) return '';
  const source = assetURLs.get(attrs.draftAssetId);
  if (!source || !isSafeAssetURL(source)) return '';

  const imageStyle = `display:block;max-width:100%;height:auto;margin:0 auto;width:${attrs.width}%`;
  const caption = attrs.caption
    ? styledElement(
        'figcaption',
        `margin-top:8px;color:${tokens.muted};font-size:13px;line-height:1.6;text-align:center`,
        escapeHTML(attrs.caption),
      )
    : '';
  return `<figure style="margin:20px 0"><img src="${escapeHTML(source)}" alt="${escapeHTML(attrs.alt)}" style="${imageStyle}">${caption}</figure>`;
}

function imageAttrs(node: EditorNode): EditorImageAttrs | undefined {
  if (!node.attrs || !('draftAssetId' in node.attrs)) return undefined;
  const attrs = node.attrs as EditorImageAttrs & Record<string, unknown>;
  if (
    Object.keys(attrs).sort().join(',') !== IMAGE_ATTRS.join(',') ||
    !Number.isSafeInteger(attrs.draftAssetId) ||
    attrs.draftAssetId <= 0 ||
    ![50, 75, 100].includes(attrs.width) ||
    attrs.align !== 'center' ||
    typeof attrs.alt !== 'string' ||
    typeof attrs.caption !== 'string'
  ) {
    return undefined;
  }
  return attrs;
}

function isSafeHTTPURL(value: string): boolean {
  if (!/^https?:\/\//i.test(value)) return false;
  try {
    const url = new URL(value);
    return url.protocol === 'http:' || url.protocol === 'https:';
  } catch {
    return false;
  }
}

function isSafeAssetURL(value: string): boolean {
  return (
    (/^\/(?!\/)/.test(value) && !/[\u0000-\u001F]/.test(value)) ||
    isSafeHTTPURL(value)
  );
}

function styledElement(tag: string, style: string, content = ''): string {
  return `<${tag} style="${escapeHTML(style)}">${content}</${tag}>`;
}
