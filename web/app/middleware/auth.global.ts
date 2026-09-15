export default defineNuxtRouteMiddleware(async (to) => {
  if (!import.meta.client) return;
  const auth = useAuthStore();
  if (isPublicWebRoute(to.path)) {
    if (to.path === '/login' && await auth.ensure()) {
      return navigateTo(resolvePostLoginPath(typeof to.query.redirect === 'string' ? to.query.redirect : undefined));
    }
    return;
  }
  if (!await auth.ensure()) return navigateTo({ path: '/login', query: { redirect: to.fullPath } });
});
