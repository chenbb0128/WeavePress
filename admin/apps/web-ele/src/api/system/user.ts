import { requestClient } from '#/api/request';

export type SystemUserRole = 'admin' | 'editor';
export type SystemUserStatus = 'active' | 'disabled';
export interface SystemUserRecord {
  createdAt: string;
  id: number;
  realName: string;
  role: SystemUserRole;
  status: SystemUserStatus;
  username: string;
}
export interface SystemUserPayload {
  password: string;
  realName: string;
  role: SystemUserRole;
  status: SystemUserStatus;
  username: string;
}
export interface SystemUserListParams {
  keyword?: string;
  page: number;
  pageSize: number;
  status?: SystemUserStatus;
}
interface UserPageResult<T> {
  items: T[];
  page: number;
  pageSize: number;
  total: number;
}

export function getSystemUsersApi(params: SystemUserListParams) {
  return requestClient.get<UserPageResult<SystemUserRecord>>('/users', {
    params,
  });
}
export function createSystemUserApi(data: SystemUserPayload) {
  return requestClient.post<SystemUserRecord>('/users', {
    password: data.password,
    realName: data.realName,
    role: data.role,
    username: data.username,
  });
}
export function updateSystemUserApi(id: number, data: SystemUserPayload) {
  return requestClient.put<SystemUserRecord>(`/users/${id}`, {
    realName: data.realName,
    role: data.role,
    status: data.status,
  });
}
export function resetSystemUserPasswordApi(id: number, password: string) {
  return requestClient.put<boolean>(`/users/${id}/password`, { password });
}
