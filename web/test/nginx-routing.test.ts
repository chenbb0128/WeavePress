import assert from 'node:assert/strict';
import test from 'node:test';

const origin = process.env.WEAVEPRESS_GATEWAY_TEST_ORIGIN || 'http://127.0.0.1:4174';

test('网关分别提供 SEO 官网和内部前台客户端壳', async () => {
  const [homepage, reader] = await Promise.all([
    fetch(`${origin}/`).then(response => response.text()),
    fetch(`${origin}/articles`).then(response => response.text()),
  ]);

  assert.match(homepage, /让好内容/);
  assert.doesNotMatch(reader, /让好内容/);
  assert.match(reader, /data-ssr="false"/);
});
