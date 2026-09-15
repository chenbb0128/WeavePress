import assert from 'node:assert/strict';
import test from 'node:test';

import {
  READER_HOME,
  buildWebsiteStructuredData,
  isPublicWebRoute,
  resolvePostLoginPath,
} from '../app/utils/site.ts';

test('公开官网不会被内部账号鉴权拦截', () => {
  assert.equal(isPublicWebRoute('/'), true);
  assert.equal(isPublicWebRoute('/login'), true);
  assert.equal(isPublicWebRoute('/articles'), false);
  assert.equal(isPublicWebRoute('/articles/1'), false);
});

test('登录后默认进入内容前台而不是返回官网', () => {
  assert.equal(READER_HOME, '/articles');
  assert.equal(resolvePostLoginPath(undefined), '/articles');
  assert.equal(resolvePostLoginPath('/'), '/articles');
  assert.equal(resolvePostLoginPath('/articles/12'), '/articles/12');
});

test('官网结构化数据描述正式站点和在线内容工具', () => {
  const data = buildWebsiteStructuredData();

  assert.equal(data['@context'], 'https://schema.org');
  assert.equal(data['@graph'][0]?.['@type'], 'WebSite');
  assert.equal(data['@graph'][0]?.url, 'https://wp.pdurl.cn/');
  assert.equal(data['@graph'][1]?.['@type'], 'SoftwareApplication');
  assert.equal(data['@graph'][1]?.applicationCategory, 'BusinessApplication');
});
