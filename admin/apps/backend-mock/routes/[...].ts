import { defineEventHandler } from 'h3';

export default defineEventHandler(() => {
  return `
<h1>Nova Admin Mock</h1>
<p>The local mock service is running.</p>
<ul>
  <li>POST /api/auth/login</li>
  <li>POST /api/auth/refresh</li>
  <li>POST /api/auth/logout</li>
  <li>GET /api/auth/codes</li>
  <li>GET /api/user/info</li>
  <li>GET /api/menu/all</li>
</ul>
`;
});
