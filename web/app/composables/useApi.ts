import type { Envelope } from '~/types/api';

export function useApi() {
  const config = useRuntimeConfig();
  const accessToken = useState<string | null>('access-token', () => null);
  let refreshing: Promise<boolean> | null = null;

  async function refresh() {
    if (!refreshing) {
      refreshing = $fetch<Envelope<{ accessToken: string }>>(`${config.public.apiBase}/auth/refresh`, { method: 'POST', credentials: 'include' })
        .then((response) => { accessToken.value = response.data.accessToken; if (import.meta.client) sessionStorage.setItem('wp-access-token', accessToken.value); return true; })
        .catch(() => { accessToken.value = null; if (import.meta.client) sessionStorage.removeItem('wp-access-token'); return false; })
        .finally(() => { refreshing = null; });
    }
    return refreshing;
  }

  async function request<T>(path: string, options: Parameters<typeof $fetch>[1] = {}, retry = true): Promise<T> {
    if (!accessToken.value && import.meta.client) accessToken.value = sessionStorage.getItem('wp-access-token');
    try {
      const response = await $fetch<Envelope<T>>(`${config.public.apiBase}${path}`, { ...options, credentials: 'include', headers: { ...options.headers, ...(accessToken.value ? { Authorization: `Bearer ${accessToken.value}` } : {}) } });
      return response.data;
    } catch (error: unknown) {
      if (retry && typeof error === 'object' && error !== null && 'statusCode' in error && error.statusCode === 401 && await refresh()) return request<T>(path, options, false);
      throw error;
    }
  }
  return { accessToken, refresh, request };
}
