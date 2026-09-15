<script setup lang="ts">
const structuredData = buildWebsiteStructuredData();

useSeoMeta({
  title: 'WeavePress｜公众号采集、AI 采编与微信排版平台',
  description: 'WeavePress 帮助内容团队集中采集微信公众号与网页文章，通过 AI 分析、辅助改写和微信排版，构建从内容发现到多平台发布的一体化工作流。',
  keywords: '微信公众号采集,微信文章采集,AI内容分析,AI辅助改写,微信公众号排版,内容管理系统,多平台发布',
  robots: 'index, follow',
  ogTitle: 'WeavePress｜让好内容从采集到发布顺畅流动',
  ogDescription: '集中采集公众号与网页内容，完成 AI 分析、辅助改写、微信排版与发布准备。',
  ogType: 'website',
  ogUrl: SITE_URL,
  ogImage: `${SITE_URL}og-cover.svg`,
  ogImageAlt: 'WeavePress 内容工作流平台',
  twitterCard: 'summary_large_image',
  twitterTitle: 'WeavePress｜公众号采集、AI 采编与微信排版平台',
  twitterDescription: '从内容采集到微信排版，把分散的内容工作集中到一个工作台。',
  twitterImage: `${SITE_URL}og-cover.svg`,
});

useHead({
  link: [{ rel: 'canonical', href: SITE_URL }],
  script: [{ type: 'application/ld+json', innerHTML: JSON.stringify(structuredData) }],
});

const features = [
  { icon: 'i-lucide-rss', title: '公众号与网页采集', text: '提交公开文章链接，异步提取标题、正文、图片与来源信息，集中沉淀到内容库。' },
  { icon: 'i-lucide-sparkles', title: 'AI 内容分析', text: '快速梳理主题、观点、结构和素材价值，为下一步选题与编辑提供清晰依据。' },
  { icon: 'i-lucide-wand-sparkles', title: 'AI 辅助改写', text: '基于原始资料生成新的表达框架，由编辑掌控语气、重点与最终内容。' },
  { icon: 'i-lucide-panels-top-left', title: '微信专业排版', text: '六套版式主题、素材管理、手机预览与版本恢复，让编辑结果更稳定。' },
  { icon: 'i-lucide-images', title: '图片素材本地化', text: '统一整理文章图片与封面，减少外链失效，方便后续复用和发布。' },
  { icon: 'i-lucide-send', title: '多平台发布准备', text: '围绕微信公众号构建首个闭环，并为小红书等平台预留内容适配能力。' },
];

const workflow = [
  { number: '01', title: '采集', text: '粘贴公众号或网页链接' },
  { number: '02', title: '理解', text: 'AI 分析内容与素材结构' },
  { number: '03', title: '编辑', text: '重组表达并完成微信排版' },
  { number: '04', title: '审核发布', text: '人工确认后进入平台草稿箱' },
];
</script>

<template>
  <div class="marketing-page">
    <header class="marketing-nav">
      <div class="marketing-container flex h-18 items-center justify-between">
        <NuxtLink to="/" class="flex items-center gap-3 font-bold text-slate-950">
          <span class="brand-mark">织</span>
          <span class="text-lg tracking-tight">WeavePress</span>
        </NuxtLink>
        <nav class="hidden items-center gap-8 text-sm text-slate-600 md:flex" aria-label="官网导航">
          <a href="#features">核心能力</a>
          <a href="#workflow">工作流程</a>
          <a href="#scenarios">适用场景</a>
        </nav>
        <div class="flex items-center gap-2 sm:gap-3">
          <NuxtLink class="marketing-link-button hidden sm:inline-flex" to="/articles">内容前台</NuxtLink>
          <a class="marketing-primary-button px-4 py-2.5 text-sm" href="/admin/">管理后台</a>
        </div>
      </div>
    </header>

    <main>
      <section class="marketing-container grid min-h-[760px] items-center gap-14 py-20 lg:grid-cols-[1.08fr_.92fr] lg:py-28">
        <div>
          <div class="marketing-eyebrow"><span class="size-2 rounded-full bg-indigo-500"/>公众号内容工作流 · 从采集到发布</div>
          <h1 class="mt-7 max-w-3xl text-5xl font-black leading-[1.08] tracking-[-.045em] text-slate-950 sm:text-6xl lg:text-7xl">让好内容，<br><span class="marketing-gradient-text">从采集到发布</span>，<br>顺畅流动。</h1>
          <p class="mt-7 max-w-2xl text-lg leading-8 text-slate-600 sm:text-xl">把散落在微信公众号和网页里的信息，变成可分析、可编辑、可排版、可发布的内容资产。一个工作台，串起内容团队的完整流程。</p>
          <div class="mt-9 flex flex-col gap-3 sm:flex-row">
            <NuxtLink class="marketing-primary-button justify-center px-6 py-3.5" to="/articles">进入内容前台 <UIcon name="i-lucide-arrow-right" class="size-4" /></NuxtLink>
            <a class="marketing-secondary-button justify-center px-6 py-3.5" href="/admin/">进入管理后台</a>
          </div>
          <div class="mt-9 flex flex-wrap gap-x-6 gap-y-3 text-sm text-slate-500">
            <span class="flex items-center gap-2"><UIcon name="i-lucide-check" class="text-indigo-600" />公开内容合规采集</span>
            <span class="flex items-center gap-2"><UIcon name="i-lucide-check" class="text-indigo-600" />私有素材存储</span>
            <span class="flex items-center gap-2"><UIcon name="i-lucide-check" class="text-indigo-600" />人工审核后发布</span>
          </div>
        </div>

        <div class="relative mx-auto w-full max-w-xl">
          <div class="hero-glow"/>
          <div class="product-window relative">
            <div class="flex items-center justify-between border-b border-slate-200/80 px-5 py-4">
              <div class="flex gap-1.5"><span class="size-2.5 rounded-full bg-rose-300"/><span class="size-2.5 rounded-full bg-amber-300"/><span class="size-2.5 rounded-full bg-emerald-300"/></div>
              <span class="text-xs font-medium text-slate-400">内容工作台</span>
              <UIcon name="i-lucide-more-horizontal" class="text-slate-400" />
            </div>
            <div class="grid gap-4 p-5 sm:grid-cols-[.72fr_1.28fr] sm:p-6">
              <div class="space-y-3">
                <div class="rounded-2xl bg-indigo-600 p-4 text-white shadow-lg shadow-indigo-200">
                  <div class="flex items-center gap-2 text-xs text-indigo-100"><UIcon name="i-lucide-inbox" />采集中心</div>
                  <p class="mt-5 text-2xl font-bold">128</p><p class="text-xs text-indigo-100">篇内容已沉淀</p>
                </div>
                <div class="rounded-2xl border border-slate-200 bg-white p-4">
                  <p class="text-xs text-slate-400">今日进度</p>
                  <div class="mt-4 h-2 overflow-hidden rounded-full bg-slate-100"><div class="h-full w-3/4 rounded-full bg-violet-500"/></div>
                  <p class="mt-3 text-xs font-medium text-slate-600">6 篇待编辑</p>
                </div>
              </div>
              <div class="rounded-2xl border border-slate-200 bg-white p-5">
                <div class="flex items-center justify-between"><span class="rounded-full bg-violet-50 px-2.5 py-1 text-xs font-semibold text-violet-600">AI 已分析</span><span class="text-xs text-slate-400">刚刚</span></div>
                <div class="mt-5 h-3 w-3/4 rounded-full bg-slate-800"/>
                <div class="mt-3 h-2 w-full rounded-full bg-slate-100"/><div class="mt-2 h-2 w-5/6 rounded-full bg-slate-100"/>
                <div class="mt-6 grid grid-cols-2 gap-3"><div class="rounded-xl bg-slate-50 p-3"><p class="text-xs text-slate-400">内容价值</p><p class="mt-2 font-bold text-slate-800">深度观点</p></div><div class="rounded-xl bg-slate-50 p-3"><p class="text-xs text-slate-400">推荐动作</p><p class="mt-2 font-bold text-slate-800">重组表达</p></div></div>
                <div class="mt-5 flex items-center gap-3 rounded-xl border border-indigo-100 bg-indigo-50/70 p-3"><span class="grid size-9 place-items-center rounded-lg bg-indigo-600 text-white"><UIcon name="i-lucide-layout-template" /></span><div><p class="text-xs text-slate-400">微信排版</p><p class="text-sm font-semibold text-slate-700">手机预览已就绪</p></div><UIcon name="i-lucide-circle-check" class="ml-auto text-indigo-600" /></div>
              </div>
            </div>
          </div>
          <div class="floating-chip -left-5 top-24 hidden sm:flex"><UIcon name="i-lucide-image" class="text-violet-600" /><span>图片已本地化</span></div>
          <div class="floating-chip -right-4 bottom-16 hidden sm:flex"><UIcon name="i-lucide-shield-check" class="text-indigo-600" /><span>等待人工审核</span></div>
        </div>
      </section>

      <section id="features" class="border-y border-slate-200/80 bg-white/80 py-24">
        <div class="marketing-container">
          <div class="max-w-2xl"><p class="section-kicker">CORE CAPABILITIES</p><h2 class="section-title">把分散的内容工具，织成一条工作流</h2><p class="section-lead">减少复制、下载、改写和排版之间的反复切换，让编辑把时间留给判断与创作。</p></div>
          <div class="mt-12 grid gap-4 md:grid-cols-2 lg:grid-cols-3">
            <article v-for="feature in features" :key="feature.title" class="feature-card group"><span class="feature-icon"><UIcon :name="feature.icon" class="size-6" /></span><h3 class="mt-6 text-xl font-bold text-slate-900">{{ feature.title }}</h3><p class="mt-3 leading-7 text-slate-600">{{ feature.text }}</p><div class="mt-7 h-px w-10 bg-indigo-500 transition-all group-hover:w-20"/></article>
          </div>
        </div>
      </section>

      <section id="workflow" class="marketing-container py-24">
        <div class="text-center"><p class="section-kicker">SIMPLE WORKFLOW</p><h2 class="section-title mx-auto">四步完成一次内容生产</h2><p class="section-lead mx-auto">从公开来源进入内容库，到人工审核后的发布准备，每一步都可追踪、可恢复。</p></div>
        <div class="relative mt-14 grid gap-4 md:grid-cols-4">
          <div class="absolute left-[12%] right-[12%] top-7 hidden h-px bg-gradient-to-r from-indigo-200 via-violet-300 to-indigo-200 md:block"/>
          <article v-for="item in workflow" :key="item.number" class="relative text-center"><span class="relative z-10 mx-auto grid size-14 place-items-center rounded-2xl border border-indigo-200 bg-white font-mono text-sm font-bold text-indigo-600 shadow-sm">{{ item.number }}</span><h3 class="mt-5 text-lg font-bold text-slate-900">{{ item.title }}</h3><p class="mt-2 text-sm leading-6 text-slate-500">{{ item.text }}</p></article>
        </div>
      </section>

      <section id="scenarios" class="marketing-container pb-24">
        <div class="overflow-hidden rounded-[2rem] bg-slate-950 px-6 py-14 text-white sm:px-12 lg:grid lg:grid-cols-[.9fr_1.1fr] lg:gap-16 lg:px-16 lg:py-18">
          <div><p class="text-sm font-bold tracking-[.18em] text-indigo-300">BUILT FOR CONTENT TEAMS</p><h2 class="mt-5 text-3xl font-black leading-tight sm:text-4xl">从个人选题库，成长为团队内容中枢</h2><p class="mt-5 leading-7 text-slate-300">适合运营团队、自媒体工作室、品牌市场和研究型内容创作者。先把微信内容闭环跑通，再逐步扩展到更多平台。</p></div>
          <div class="mt-10 grid gap-3 sm:grid-cols-2 lg:mt-0"><div class="scenario-card"><UIcon name="i-lucide-library" /><span>建立统一内容资料库</span></div><div class="scenario-card"><UIcon name="i-lucide-users" /><span>多人协作与审核</span></div><div class="scenario-card"><UIcon name="i-lucide-history" /><span>版本留痕与恢复</span></div><div class="scenario-card"><UIcon name="i-lucide-blocks" /><span>持续扩展发布渠道</span></div></div>
        </div>
      </section>

      <section class="border-t border-slate-200 bg-white py-20">
        <div class="marketing-container text-center"><p class="section-kicker">START YOUR WORKFLOW</p><h2 class="mx-auto mt-4 max-w-3xl text-3xl font-black tracking-tight text-slate-950 sm:text-5xl">把下一篇值得留下的内容，交给 WeavePress</h2><p class="mx-auto mt-5 max-w-2xl text-slate-600">进入内容前台阅读已沉淀的文章，或前往管理后台开始采集、分析和排版。</p><div class="mt-8 flex flex-col justify-center gap-3 sm:flex-row"><NuxtLink class="marketing-primary-button justify-center px-6 py-3.5" to="/articles">进入内容前台</NuxtLink><a class="marketing-secondary-button justify-center px-6 py-3.5" href="/admin/">进入管理后台</a></div></div>
      </section>
    </main>

    <footer class="border-t border-slate-200 bg-white py-8">
      <div class="marketing-container flex flex-col items-center justify-between gap-4 text-sm text-slate-500 sm:flex-row"><div class="flex items-center gap-2 font-semibold text-slate-700"><span class="grid size-7 place-items-center rounded-lg bg-indigo-600 text-xs text-white">织</span>WeavePress</div><p>公开内容合规采集 · AI 辅助创作 · 人工审核发布</p><p>© {{ new Date().getFullYear() }} WeavePress</p></div>
    </footer>
  </div>
</template>
