import type { EventHandlerRequest, H3Event } from 'h3';

import type { UserInfo } from './mock-data';

import { getHeader } from 'h3';
import jwt from 'jsonwebtoken';

import { MOCK_USERS } from './mock-data';

const ACCESS_TOKEN_SECRET =
  process.env.ACCESS_TOKEN_SECRET || 'local_access_token_secret';
const REFRESH_TOKEN_SECRET =
  process.env.REFRESH_TOKEN_SECRET || 'local_refresh_token_secret';

type UserTokenPayload = Omit<UserInfo, 'password'>;

interface JwtPayload extends UserTokenPayload {
  exp: number;
  iat: number;
}

function createPayload(user: UserInfo): UserTokenPayload {
  const { password: _password, ...payload } = user;
  return payload;
}

export function generateAccessToken(user: UserInfo) {
  return jwt.sign(createPayload(user), ACCESS_TOKEN_SECRET, {
    expiresIn: '7d',
  });
}

export function generateRefreshToken(user: UserInfo) {
  return jwt.sign(createPayload(user), REFRESH_TOKEN_SECRET, {
    expiresIn: '30d',
  });
}

export function verifyAccessToken(
  event: H3Event<EventHandlerRequest>,
): null | UserTokenPayload {
  const authHeader = getHeader(event, 'Authorization');
  if (!authHeader?.startsWith('Bearer ')) {
    return null;
  }

  const token = authHeader.slice('Bearer '.length);
  try {
    const decoded = jwt.verify(token, ACCESS_TOKEN_SECRET) as JwtPayload;
    return findUserPayload(decoded.username);
  } catch {
    return null;
  }
}

export function verifyRefreshToken(token: string): null | UserTokenPayload {
  try {
    const decoded = jwt.verify(token, REFRESH_TOKEN_SECRET) as JwtPayload;
    return findUserPayload(decoded.username);
  } catch {
    return null;
  }
}

function findUserPayload(username: string): null | UserTokenPayload {
  const user = MOCK_USERS.find((item) => item.username === username);
  return user ? createPayload(user) : null;
}
