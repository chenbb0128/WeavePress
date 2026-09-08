import { eventHandler, getRouterParam, readBody, setResponseStatus } from 'h3';
import { verifyAccessToken } from '~/utils/jwt-utils';
import {
  unAuthorizedResponse,
  useResponseError,
  useResponseSuccess,
} from '~/utils/response';
import {
  findSystemUser,
  hasSystemUsername,
  isSystemUserRole,
  isSystemUserStatus,
  updateSystemUser,
} from '~/utils/system-user-store';

export default eventHandler(async (event) => {
  if (!verifyAccessToken(event)) {
    return unAuthorizedResponse(event);
  }

  const id = Number(getRouterParam(event, 'id'));
  const existing = findSystemUser(id);
  if (!Number.isInteger(id) || !existing) {
    setResponseStatus(event, 404);
    return useResponseError('User not found');
  }

  const body = await readBody<Record<string, unknown>>(event);
  const username =
    typeof body.username === 'string' ? body.username.trim() : '';
  const realName =
    typeof body.realName === 'string' ? body.realName.trim() : '';

  if (
    username.length < 3 ||
    !realName ||
    !isSystemUserRole(body.role) ||
    !isSystemUserStatus(body.status)
  ) {
    setResponseStatus(event, 400);
    return useResponseError('Invalid user payload');
  }

  if (hasSystemUsername(username, id)) {
    setResponseStatus(event, 409);
    return useResponseError('Username already exists');
  }

  const user = updateSystemUser(id, {
    realName,
    role: body.role,
    status: body.status,
    username,
  });
  return useResponseSuccess(user);
});
