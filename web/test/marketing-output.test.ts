import assert from 'node:assert/strict';
import { access, readFile } from 'node:fs/promises';
import test from 'node:test';

const outputRoot = new URL('../.output/public/', import.meta.url);

test('首页生成可被搜索引擎直接读取的产品介绍和 SEO 元数据', async () => {
  const html = await readFile(new URL('index.html', outputRoot), 'utf8');
  const visibleText = html.replace(/<[^>]+>/g, '').replace(/\s+/g, ' ');

  assert.match(html, /WeavePress｜公众号采集、AI 采编与微信排版平台/);
  assert.match(visibleText, /让好内容，从采集到发布，顺畅流动。/);
  assert.match(visibleText, /进入内容前台/);
  assert.match(visibleText, /进入管理后台/);
  assert.match(html, /rel="canonical" href="https:\/\/wp\.pdurl\.cn\/"/);
  assert.match(html, /application\/ld\+json/);
  assert.match(html, /SoftwareApplication/);
});

test('搜索引擎规则只收录公开官网', async () => {
  const robots = await readFile(new URL('robots.txt', outputRoot), 'utf8');
  const sitemap = await readFile(new URL('sitemap.xml', outputRoot), 'utf8');

  assert.match(robots, /Allow: \/$/m);
  assert.match(robots, /Disallow: \/articles/m);
  assert.match(robots, /Disallow: \/login/m);
  assert.match(robots, /Disallow: \/admin\//m);
  assert.match(sitemap, /<loc>https:\/\/wp\.pdurl\.cn\/<\/loc>/);
  assert.doesNotMatch(sitemap, /\/articles|\/login|\/admin\//);
});

test('Nuxt 前台产物不会覆盖独立管理后台目录', async () => {
  await assert.rejects(access(new URL('admin/index.html', outputRoot)));
});
