import type {
  SystemUserListParams,
  SystemUserPayload,
  SystemUserRecord,
  SystemUserStatus,
} from '#/api/system/user';

interface UserQueryModel {
  keyword: string;
  status: '' | SystemUserStatus;
}

function createUserForm(): SystemUserPayload {
  return {
    password: '',
    realName: '',
    role: 'editor',
    status: 'active',
    username: '',
  };
}

function toUserForm(user: SystemUserRecord): SystemUserPayload {
  return {
    password: '',
    realName: user.realName,
    role: user.role,
    status: user.status,
    username: user.username,
  };
}

function buildUserListParams(
  query: UserQueryModel,
  page: number,
  pageSize: number,
): SystemUserListParams {
  return {
    keyword: query.keyword.trim() || undefined,
    page,
    pageSize,
    status: query.status || undefined,
  };
}

export { buildUserListParams, createUserForm, toUserForm };
export type { UserQueryModel };
