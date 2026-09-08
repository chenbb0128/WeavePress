import { eventHandler, getQuery } from 'h3';
import { verifyAccessToken } from '~/utils/jwt-utils';
import { unAuthorizedResponse, usePageResponseSuccess } from '~/utils/response';
import { isSystemUserStatus, listSystemUsers } from '~/utils/system-user-store';

export default eventHandler((event) => {
  if (!verifyAccessToken(event)) {
    return unAuthorizedResponse(event);
  }

  const query = getQuery(event);
  const page = Math.max(1, Number(query.page) || 1);
  const pageSize = Math.min(100, Math.max(1, Number(query.pageSize) || 10));
  const status = isSystemUserStatus(query.status) ? query.status : undefined;
  const users = listSystemUsers({
    keyword: typeof query.keyword === 'string' ? query.keyword : undefined,
    status,
  });

  return usePageResponseSuccess(page, pageSize, users);
});
