export type MockSystemUserRole = 'admin' | 'editor' | 'viewer';
export type MockSystemUserStatus = 'active' | 'disabled';

export interface MockSystemUser {
  createdAt: string;
  id: number;
  realName: string;
  role: MockSystemUserRole;
  status: MockSystemUserStatus;
  username: string;
}

export type MockSystemUserPayload = Omit<MockSystemUser, 'createdAt' | 'id'>;

const INITIAL_SYSTEM_USERS: MockSystemUser[] = [
  {
    createdAt: '2026-01-01T08:00:00.000Z',
    id: 1,
    realName: 'Administrator',
    role: 'admin',
    status: 'active',
    username: 'admin',
  },
  {
    createdAt: '2026-02-12T09:30:00.000Z',
    id: 2,
    realName: 'Content Editor',
    role: 'editor',
    status: 'active',
    username: 'editor',
  },
  {
    createdAt: '2026-03-18T13:20:00.000Z',
    id: 3,
    realName: 'Data Viewer',
    role: 'viewer',
    status: 'active',
    username: 'viewer',
  },
  {
    createdAt: '2026-04-22T05:40:00.000Z',
    id: 4,
    realName: 'Disabled Account',
    role: 'viewer',
    status: 'disabled',
    username: 'disabled-user',
  },
];

let systemUsers = cloneUsers(INITIAL_SYSTEM_USERS);

function cloneUsers(users: MockSystemUser[]) {
  return users.map((user) => ({ ...user }));
}

function listSystemUsers(filters: {
  keyword?: string;
  status?: MockSystemUserStatus;
}) {
  const keyword = filters.keyword?.trim().toLowerCase();
  return systemUsers
    .filter((user) => {
      const matchesKeyword =
        !keyword ||
        user.username.toLowerCase().includes(keyword) ||
        user.realName.toLowerCase().includes(keyword);
      const matchesStatus = !filters.status || user.status === filters.status;
      return matchesKeyword && matchesStatus;
    })
    .map((user) => ({ ...user }));
}

function findSystemUser(id: number) {
  const user = systemUsers.find((item) => item.id === id);
  return user ? { ...user } : null;
}

function hasSystemUsername(username: string, exceptId?: number) {
  const normalized = username.trim().toLowerCase();
  return systemUsers.some(
    (user) =>
      user.id !== exceptId && user.username.toLowerCase() === normalized,
  );
}

function createSystemUser(payload: MockSystemUserPayload) {
  const nextId = Math.max(0, ...systemUsers.map((user) => user.id)) + 1;
  const user: MockSystemUser = {
    ...payload,
    createdAt: new Date().toISOString(),
    id: nextId,
    realName: payload.realName.trim(),
    username: payload.username.trim(),
  };
  systemUsers = [user, ...systemUsers];
  return { ...user };
}

function updateSystemUser(id: number, payload: MockSystemUserPayload) {
  const index = systemUsers.findIndex((user) => user.id === id);
  if (index === -1) return null;

  const updated: MockSystemUser = {
    ...systemUsers[index],
    ...payload,
    id,
    realName: payload.realName.trim(),
    username: payload.username.trim(),
  };
  systemUsers[index] = updated;
  return { ...updated };
}

function deleteSystemUser(id: number) {
  const previousLength = systemUsers.length;
  systemUsers = systemUsers.filter((user) => user.id !== id);
  return systemUsers.length < previousLength;
}

function resetSystemUsers() {
  systemUsers = cloneUsers(INITIAL_SYSTEM_USERS);
}

function isSystemUserRole(value: unknown): value is MockSystemUserRole {
  return value === 'admin' || value === 'editor' || value === 'viewer';
}

function isSystemUserStatus(value: unknown): value is MockSystemUserStatus {
  return value === 'active' || value === 'disabled';
}

export {
  createSystemUser,
  deleteSystemUser,
  findSystemUser,
  hasSystemUsername,
  isSystemUserRole,
  isSystemUserStatus,
  listSystemUsers,
  resetSystemUsers,
  updateSystemUser,
};
