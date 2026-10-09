import type { AIQualityBlock } from '#/api/ai';
import type {
  Article,
  ContentBlock,
  EditorDocument,
  EditorNode,
} from '#/api/content';

export function nodeText(node: EditorNode): string {
  if (node.type === 'hardBreak') return '\n';
  if (node.type === 'text') return node.text ?? '';
  const separator = [
    'blockquote',
    'bulletList',
    'listItem',
    'orderedList',
  ].includes(node.type)
    ? '\n'
    : '';
  return (node.content ?? []).map((child) => nodeText(child)).join(separator);
}

export function qualityBlocks(document: EditorDocument): AIQualityBlock[] {
  return (document.content ?? []).map((node) => {
    let type: AIQualityBlock['type'] = 'paragraph';
    if (node.type === 'blockquote') type = 'quote';
    else if (node.type === 'bulletList' || node.type === 'orderedList')
      type = 'list';
    else if (node.type === 'heading' || node.type === 'image') type = node.type;
    else if (node.type === 'horizontalRule') type = 'separator';
    return { text: nodeText(node), type };
  });
}

export function comparisonSourceBlocks(
  article: Article,
): (ContentBlock & { id: string })[] {
  let blocks = article.blocks ?? [];
  const compact = (value: string) => value.replaceAll(/\s/g, '');
  const structured = blocks.map((block) => block.text ?? '').join('');
  if (article.plainText && compact(structured) !== compact(article.plainText)) {
    blocks = [
      { type: 'paragraph', text: article.plainText },
      ...blocks.filter((block) => block.type === 'image'),
    ];
  }
  return blocks.map((block, index) => ({ ...block, id: `B${index + 1}` }));
}

export function highlightText(text: string, excerpt = '') {
  if (!excerpt) return [{ matched: false, text }];
  const offsets: number[] = [];
  let compact = '';
  let offset = 0;
  for (const character of text) {
    if (!/\s/.test(character)) {
      compact += character;
      for (let i = 0; i < character.length; i += 1) offsets.push(offset);
    }
    offset += character.length;
  }
  const needle = excerpt.replaceAll(/\s/g, '');
  const start = needle ? compact.indexOf(needle) : -1;
  if (start < 0) return [{ matched: false, text }];
  const from = offsets[start] ?? 0;
  const endOffset = offsets[start + needle.length - 1] ?? from;
  const lastCharacter = String.fromCodePoint(text.codePointAt(endOffset) ?? 0);
  const to = endOffset + lastCharacter.length;
  return [
    { matched: false, text: text.slice(0, from) },
    { matched: true, text: text.slice(from, to) },
    { matched: false, text: text.slice(to) },
  ].filter((segment) => segment.text);
}

export function applyRewrite(
  document: EditorDocument,
  index: number,
  text: string,
  type: string,
): EditorDocument | undefined {
  const nodes = document.content ?? [];
  const current = nodes[index];
  if (
    !current ||
    !['blockquote', 'heading', 'paragraph'].includes(current.type) ||
    !text.trim()
  )
    return;
  if (current.type === 'heading' && type !== 'heading') return;
  if (current.type !== 'heading' && type !== 'paragraph') return;
  const replacement: EditorNode = {
    type: current.type === 'heading' ? 'heading' : 'paragraph',
    content: [{ type: 'text', text }],
  };
  if (current.type === 'heading') replacement.attrs = current.attrs;
  return {
    ...document,
    content: nodes.map((node, position) =>
      position === index ? replacement : node,
    ),
  };
}
