import { beforeEach, describe, expect, it } from 'vitest';

import {
  createSystemUser,
  deleteSystemUser,
  findSystemUser,
  hasSystemUsername,
  listSystemUsers,
  resetSystemUsers,
  updateSystemUser,
} from './system-user-store';

describe('system user store', () => {
  beforeEach(resetSystemUsers);

  it('filters users by keyword and status', () => {
    expect(
      listSystemUsers({ keyword: 'editor', status: 'active' }),
    ).toHaveLength(1);
    expect(listSystemUsers({ status: 'disabled' })).toHaveLength(1);
  });

  it('creates and updates a user', () => {
    const created = createSystemUser({
      realName: 'Template User',
      role: 'viewer',
      status: 'active',
      username: 'template-user',
    });

    expect(hasSystemUsername('template-user')).toBe(true);
    expect(
      updateSystemUser(created.id, {
        ...created,
        realName: 'Updated User',
      })?.realName,
    ).toBe('Updated User');
  });

  it('deletes an existing user', () => {
    expect(deleteSystemUser(2)).toBe(true);
    expect(findSystemUser(2)).toBeNull();
    expect(deleteSystemUser(999)).toBe(false);
  });
});
