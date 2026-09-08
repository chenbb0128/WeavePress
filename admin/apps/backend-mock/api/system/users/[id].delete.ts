import { eventHandler, getRouterParam, setResponseStatus } from 'h3';
import { verifyAccessToken } from '~/utils/jwt-utils';
import {
  unAuthorizedResponse,
  useResponseError,
  useResponseSuccess,
} from '~/utils/response';
import { deleteSystemUser, findSystemUser } from '~/utils/system-user-store';

export default eventHandler((event) => {
  if (!verifyAccessToken(event)) {
    return unAuthorizedResponse(event);
  }

  const id = Number(getRouterParam(event, 'id'));
  if (!Number.isInteger(id) || !findSystemUser(id)) {
    setResponseStatus(event, 404);
    return useResponseError('User not found');
  }

  if (id === 1) {
    setResponseStatus(event, 400);
    return useResponseError('The administrator account cannot be deleted');
  }

  return useResponseSuccess(deleteSystemUser(id));
});
