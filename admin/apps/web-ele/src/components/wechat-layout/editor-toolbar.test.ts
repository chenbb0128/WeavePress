import type { App, ShallowRef } from 'vue';

import { Editor } from '@tiptap/vue-3';
import StarterKit from '@tiptap/starter-kit';
import { createApp, defineComponent, h, nextTick, shallowRef } from 'vue';

import { afterEach, describe, expect, it, vi } from 'vitest';

import EditorToolbar from './editor-toolbar.vue';

const mocks = vi.hoisted(() => ({ prompt: vi.fn() }));

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal<typeof import('element-plus')>();
  return {
    ...actual,
    ElMessageBox: { prompt: mocks.prompt },
  };
});

const apps: App[] = [];
const editors: Editor[] = [];

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve;
  });
  return { promise, resolve };
}

function createEditor(text: string) {
  const editor = new Editor({
    content: `<p>${text}</p>`,
    extensions: [StarterKit],
  });
  editor.commands.setTextSelection({ from: 1, to: text.length + 1 });
  editors.push(editor);
  return editor;
}

function mountToolbar(editor: ShallowRef<Editor | null>) {
  const host = document.createElement('div');
  document.body.append(host);
  const root = defineComponent(
    () => () => h(EditorToolbar, { editor: editor.value }),
  );
  const app = createApp(root);
  apps.push(app);
  app.mount(host);
  return host;
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

describe('EditorToolbar', () => {
  afterEach(() => {
    for (const app of apps.splice(0)) app.unmount();
    for (const editor of editors.splice(0)) editor.destroy();
    document.body.textContent = '';
    vi.resetAllMocks();
  });

  it('does not apply a pending link after the editor instance changes', async () => {
    const original = createEditor('原编辑器');
    const current = shallowRef<Editor | null>(original);
    const host = mountToolbar(current);
    const prompt = deferred<{ value: string }>();
    mocks.prompt.mockReturnValue(prompt.promise);

    buttonByText(host, '链接')?.click();
    await settle();
    expect(mocks.prompt).toHaveBeenCalledOnce();

    const replacement = createEditor('新编辑器');
    current.value = replacement;
    await settle();
    prompt.resolve({ value: 'https://example.com/article' });
    await settle();

    expect(original.getHTML()).not.toContain('<a');
    expect(replacement.getHTML()).not.toContain('<a');
  });
});
