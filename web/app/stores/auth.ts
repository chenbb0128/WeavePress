import type { Envelope, UserInfo } from '~/types/api';
import { defineStore } from 'pinia';

export const useAuthStore = defineStore('auth', () => {
  const user = ref<UserInfo | null>(null); const busy = ref(false); const { accessToken, refresh, request } = useApi();
  async function loadUser() { user.value = await request<UserInfo>('/user/info'); return user.value; }
  async function ensure() { if (user.value) return true; if (!accessToken.value && import.meta.client) accessToken.value = sessionStorage.getItem('wp-access-token'); if (!accessToken.value && !await refresh()) return false; try { await loadUser(); return true; } catch { return false; } }
  async function login(username: string, password: string) { busy.value = true; try { const config = useRuntimeConfig(); const response = await $fetch<Envelope<{ accessToken: string }>>(`${config.public.apiBase}/auth/login`, { method: 'POST', body: { username, password }, credentials: 'include' }); accessToken.value = response.data.accessToken; sessionStorage.setItem('wp-access-token', accessToken.value); await loadUser(); } finally { busy.value = false; } }
  async function logout() { try { await request('/auth/logout', { method: 'POST' }, false); } catch { /* 即使服务端会话已失效，也继续清理本地登录状态。 */ } accessToken.value = null; user.value = null; if (import.meta.client) sessionStorage.removeItem('wp-access-token'); await navigateTo('/login'); }
  return { busy, ensure, login, logout, user };
});
