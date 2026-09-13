import Image from '@tiptap/extension-image';

const IMAGE_WIDTHS = [50, 75, 100];

export const WechatImage = Image.extend({
  name: 'image',

  group: 'block',
  inline: false,
  atom: true,
  draggable: true,

  addAttributes() {
    return {
      draftAssetId: { default: 0 },
      width: { default: 100 },
      align: { default: 'center' },
      alt: { default: '' },
      caption: { default: '' },
    };
  },

  parseHTML() {
    return [
      {
        tag: 'figure[data-draft-asset-id]',
        getAttrs: (element) => {
          if (typeof element === 'string') return false;
          const draftAssetId = Number(
            element.getAttribute('data-draft-asset-id'),
          );
          return Number.isSafeInteger(draftAssetId) && draftAssetId > 0
            ? { draftAssetId }
            : false;
        },
      },
    ];
  },

  renderHTML({ node }) {
    const draftAssetId = Number(node.attrs.draftAssetId);
    const width = Number(node.attrs.width);
    const align = node.attrs.align === 'center' ? 'center' : 'center';
    const alt = typeof node.attrs.alt === 'string' ? node.attrs.alt : '';
    const caption =
      typeof node.attrs.caption === 'string' ? node.attrs.caption : '';

    const figureAttrs = {
      'data-align': align,
      'data-draft-asset-id':
        Number.isSafeInteger(draftAssetId) && draftAssetId > 0
          ? String(draftAssetId)
          : '0',
      'data-width': IMAGE_WIDTHS.includes(width) ? String(width) : '100',
    };
    const image = ['img', { alt }] as const;

    return caption
      ? ['figure', figureAttrs, image, ['figcaption', {}, caption]]
      : ['figure', figureAttrs, image];
  },

  addInputRules() {
    return [];
  },
});

export default WechatImage;
