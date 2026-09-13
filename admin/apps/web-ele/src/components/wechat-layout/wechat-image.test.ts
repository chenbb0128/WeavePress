import { Editor } from '@tiptap/vue-3';
import StarterKit from '@tiptap/starter-kit';

import { afterEach, describe, expect, it } from 'vitest';

import { WechatImage } from './wechat-image';

const editors: Editor[] = [];

function createEditor(content: string) {
  const editor = new Editor({
    content,
    extensions: [StarterKit, WechatImage],
  });
  editors.push(editor);
  return editor;
}

describe('WechatImage', () => {
  afterEach(() => {
    for (const editor of editors.splice(0)) editor.destroy();
  });

  it('imports only the draft asset ID through the real DOM parser', () => {
    const editor = createEditor(`
      <figure
        data-draft-asset-id="18"
        width="50"
        align="left"
        alt="恶意说明"
        caption="恶意标题"
        class="outside-class"
        style="position:fixed"
        src="https://evil.example/tracker.png"
      ><img src="https://evil.example/tracker.png"></figure>
    `);

    expect(editor.getJSON()).toEqual({
      type: 'doc',
      content: [
        {
          type: 'image',
          attrs: {
            draftAssetId: 18,
            width: 100,
            align: 'center',
            alt: '',
            caption: '',
          },
        },
      ],
    });
    expect(editor.getHTML()).toContain('data-draft-asset-id="18"');
    expect(editor.getHTML()).not.toContain('evil.example');
    expect(editor.getHTML()).not.toContain('outside-class');
    expect(editor.getHTML()).not.toContain('position:fixed');
  });
});
