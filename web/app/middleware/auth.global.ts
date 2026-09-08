export default defineNuxtRouteMiddleware(async (to) => {
  if (!import.meta.client) return;
  const auth = useAuthStore();
  if (to.path === '/login') { if (await auth.ensure()) return navigateTo('/'); return; }
  if (!await auth.ensure()) return navigateTo({ path: '/login', query: { redirect: to.fullPath } });
});
