import type { SystemUserRecord } from '#/api/system/user';

import { describe, expect, it } from 'vitest';

import { buildUserListParams, createUserForm, toUserForm } from './model';

describe('system user view model', () => {
  it('creates a predictable empty form', () => {
    expect(createUserForm()).toEqual({
      password: '',
      realName: '',
      role: 'editor',
      status: 'active',
      username: '',
    });
  });

  it('normalizes list query parameters', () => {
    expect(
      buildUserListParams({ keyword: '  nova  ', status: '' }, 2, 20),
    ).toEqual({
      keyword: 'nova',
      page: 2,
      pageSize: 20,
      status: undefined,
    });
  });

  it('maps a record to an editable payload', () => {
    const user: SystemUserRecord = {
      createdAt: '2026-01-01T00:00:00.000Z',
      id: 1,
      realName: 'Administrator',
      role: 'admin',
      status: 'active',
      username: 'admin',
    };

    expect(toUserForm(user)).toEqual({
      password: '',
      realName: 'Administrator',
      role: 'admin',
      status: 'active',
      username: 'admin',
    });
  });
});
