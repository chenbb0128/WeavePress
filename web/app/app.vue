<script setup lang="ts">
const auth = useAuthStore();
</script>

<template>
  <UApp>
    <header v-if="$route.path !== '/login'" class="sticky top-0 z-20 border-b border-black/10 bg-[#f5f3eb]/90 backdrop-blur">
      <div class="wp-container flex h-16 items-center justify-between"><NuxtLink to="/" class="flex items-center gap-3 font-bold"><span class="grid size-9 place-items-center rounded-xl bg-emerald-700 text-white">织</span><span>WeavePress</span></NuxtLink><nav class="flex items-center gap-5 text-sm"><NuxtLink to="/" active-class="text-emerald-700">概览</NuxtLink><NuxtLink to="/articles" active-class="text-emerald-700">内容库</NuxtLink><span class="hidden text-gray-500 sm:inline">{{ auth.user?.realName || auth.user?.username }}</span><button class="text-gray-500 hover:text-red-600" @click="auth.logout">退出</button></nav></div>
    </header>
    <NuxtPage />
  </UApp>
</template>
