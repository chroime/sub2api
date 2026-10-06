<template>
  <section
    aria-labelledby="public-home-hero-title"
    class="relative isolate overflow-hidden border-b border-white/10 bg-[#0b0f14] text-slate-100"
  >
    <div class="relative mx-auto grid max-w-7xl gap-12 px-5 py-16 sm:px-8 sm:py-20 lg:grid-cols-[1.05fr_0.95fr] lg:items-center lg:gap-14 lg:px-10 lg:py-24">
      <div class="min-w-0 max-w-3xl">
        <div class="flex items-start gap-4">
          <h1
            id="public-home-hero-title"
            class="max-w-3xl text-white"
            :class="titleLines.length ? 'hero-title-text' : 'text-4xl font-semibold leading-[1.05] tracking-normal sm:text-6xl lg:text-7xl'"
          >
            <template v-if="titleLines.length">
              <span v-for="(line, index) in titleLines" :key="index" class="hero-title-line" :class="{ 'hero-title-accent': index === 1 }">{{ line }}</span>
            </template>
            <template v-else>{{ title || siteName }}</template>
          </h1>
        </div>

        <div class="mt-9 flex flex-col gap-3 sm:flex-row sm:flex-wrap sm:items-center">
          <a
            :href="primaryHref"
            class="inline-flex min-h-11 items-center justify-center gap-2 rounded-lg bg-cyan-300 px-5 py-3 text-sm font-bold text-[#071014] shadow-[0_0_24px_rgba(103,232,249,0.18)] transition hover:bg-cyan-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-200 focus-visible:ring-offset-2 focus-visible:ring-offset-[#0a1016]"
          >
            {{ resolvedPrimaryLabel }}
            <Icon name="arrowRight" size="sm" :stroke-width="2" aria-hidden="true" />
          </a>
          <a
            :href="secondaryHref"
            class="inline-flex min-h-11 items-center justify-center gap-2 rounded-lg border border-white/15 bg-white/[0.03] px-5 py-3 text-sm font-semibold text-slate-200 transition hover:border-cyan-300/50 hover:bg-cyan-300/10 hover:text-cyan-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-200 focus-visible:ring-offset-2 focus-visible:ring-offset-[#0a1016]"
          >
            <Icon name="book" size="sm" aria-hidden="true" />
            {{ resolvedSecondaryLabel }}
          </a>
        </div>

        <dl class="hero-stats">
          <div v-for="stat in resolvedStats" :key="stat.label" class="hero-stat">
            <dt>{{ stat.label }}</dt>
            <dd class="hero-stat-value">{{ stat.value }}<span v-if="stat.suffix" class="hero-stat-suffix">{{ stat.suffix }}</span></dd>
          </div>
        </dl>
      </div>

      <div class="relative mx-auto min-w-0 w-full max-w-xl lg:mx-0 lg:justify-self-end">
        <div class="terminal-container terminal-breathing relative overflow-hidden rounded-lg border bg-[#111a22]">
          <div class="flex items-center justify-between border-b border-white/10 px-4 py-3">
            <div class="flex items-center gap-2">
              <span class="h-2 w-2 rounded-full bg-emerald-300" aria-hidden="true" />
              <span class="font-mono text-xs text-slate-400">gateway / v1</span>
            </div>
            <span class="rounded border border-cyan-300/20 bg-cyan-300/10 px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wider text-cyan-200">{{ t('home.public.hero.gatewayPreview') }}</span>
          </div>
          <div class="space-y-5 p-5 sm:p-7">
            <div class="terminal-command rounded-lg border border-white/10 bg-[#080d12] p-4 font-mono text-xs leading-6 text-slate-300" aria-label="OpenAI 兼容接口请求示例">
              <div class="terminal-type-line"><span class="text-cyan-300">$</span> curl {{ endpoint }}</div>
              <div class="text-slate-500">  -H <span class="text-amber-200">"Authorization: Bearer sk-live_..."</span></div>
              <div class="text-slate-500">  -d <span class="text-amber-200">'{ "model": "{{ model }}" }'</span><span class="terminal-caret" aria-hidden="true" /></div>
            </div>
            <div class="terminal-response flex items-center justify-between rounded-lg border border-emerald-300/20 bg-emerald-300/[0.06] px-4 py-3">
              <div class="flex items-center gap-2 text-xs font-semibold text-emerald-100"><Icon name="checkCircle" size="sm" class="text-emerald-300" aria-hidden="true" />200 OK</div>
              <span class="font-mono text-[11px] text-emerald-200/70">request_id: 8f2c</span>
            </div>
            <div class="flex items-center gap-1.5 border-t border-white/10 pt-4 text-xs text-slate-500"><Icon name="shield" size="xs" class="text-cyan-300" aria-hidden="true" /> {{ t('home.public.hero.keysStayYours') }}</div>
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, toRefs } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'

interface Stat {
  label: string
  value: string
  suffix?: string
}

const props = withDefaults(defineProps<{
  siteName?: string
  logo?: string
  title?: string
  titleLines?: string[]
  subtitle?: string
  eyebrow?: string
  primaryHref?: string
  primaryLabel?: string
  secondaryHref?: string
  secondaryLabel?: string
  endpoint?: string
  model?: string
  stats?: Stat[]
}>(), {
  siteName: 'Sub2API',
  logo: '',
  contact: 'QQ 群 123123',
  title: '',
  titleLines: () => [],
  subtitle: '',
  eyebrow: '',
  primaryHref: '/login',
  primaryLabel: '',
  secondaryHref: '/docs',
  secondaryLabel: '',
  endpoint: 'https://api.example.com/v1/chat/completions',
  model: 'gpt-5.6-sol',
  stats: () => [],
})

const { t } = useI18n()
const { siteName, title, primaryHref, primaryLabel, secondaryHref, secondaryLabel, endpoint, model, stats } = toRefs(props)
const resolvedPrimaryLabel = computed(() => primaryLabel.value || t('home.public.hero.createApiKey'))
const resolvedSecondaryLabel = computed(() => secondaryLabel.value || t('home.public.hero.readDocs'))
const resolvedStats = computed<Stat[]>(() => stats.value.length ? stats.value : [
  { label: t('home.public.hero.providers'), value: '—' },
  { label: t('home.public.hero.compatibility'), value: t('home.public.hero.openAiApi') },
  { label: t('home.public.hero.routing'), value: t('home.public.hero.smartSticky') },
])
</script>

<style scoped>
.hero-stats { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; margin-top: 40px; padding-top: 28px; border-top: 1px solid rgb(255 255 255 / .07); }
.hero-stat { display: flex; flex-direction: column; min-width: 0; gap: 10px; padding: 2px 0; }
.hero-stat dt { order: 1; color: #94a3b8; font-size: 12px; line-height: 20px; }
.hero-stat-value { color: #f8fafc; font-size: clamp(2.5rem, 5vw, 3.75rem); font-weight: 700; line-height: .95; font-variant-numeric: tabular-nums; letter-spacing: 0; text-shadow: 0 0 24px rgb(94 234 212 / .12); }
.hero-stat-suffix { color: #34d399; }
:global(.public-home-light) .hero-stat-value { color: #0f172a; }
:global(.public-home-light) .hero-stat dt { color: #64748b; }
:global(.public-home-light) .hero-stat-suffix { color: #047857; }
@media (max-width: 639px) { .hero-stats { gap: 12px; margin-top: 32px; padding-top: 22px; } }
.hero-title-text { width: 100%; font-family: "Microsoft YaHei", "PingFang SC", sans-serif; font-size: 36px; font-weight: 700; line-height: 1.25; letter-spacing: 0; overflow-wrap: anywhere; }
.hero-title-line { display: block; }
.hero-title-line + .hero-title-line { margin-top: 8px; }
.hero-title-accent { color: #10c888; font-size: 28px; }
:global(.public-home-light) .hero-title-accent { color: #05845b; }
@media (min-width: 640px) {
  .hero-title-text { font-size: 56px; }
  .hero-title-accent { font-size: 44px; }
}
@media (min-width: 1024px) {
  .hero-title-text { font-size: 64px; }
  .hero-title-accent { font-size: 48px; }
}
@media (min-width: 1280px) {
  .hero-title-text { font-size: 72px; }
  .hero-title-accent { font-size: 60px; }
}
.terminal-container { min-width: 0; }
.terminal-breathing { border-color: rgb(45 212 191 / .3); animation: terminal-breathe 4s ease-in-out infinite; }
.terminal-command { min-width: 0; max-width: 100%; overflow-x: auto; }
.terminal-type-line { width: max-content; min-width: 100%; clip-path: inset(0 100% 0 0); animation: terminal-reveal 1.8s steps(38, end) .15s forwards; }
.terminal-caret { display: inline-block; width: .45rem; height: 1rem; margin-left: .2rem; vertical-align: -.2rem; background: rgb(103 232 249); animation: terminal-blink 1s steps(1, end) infinite; }
@keyframes terminal-reveal { to { clip-path: inset(0 0 0 0); } }
@keyframes terminal-blink { 50% { opacity: 0; } }
@keyframes terminal-breathe {
  0%, 100% { border-color: rgb(45 212 191 / .22); box-shadow: 0 0 8px rgb(34 211 238 / .03), 0 0 20px rgb(20 184 166 / .02); }
  50% { border-color: rgb(45 212 191 / .7); box-shadow: 0 0 20px rgb(34 211 238 / .23), 0 0 44px rgb(20 184 166 / .16); }
}
@media (prefers-reduced-motion: reduce) {
  .terminal-breathing { animation: none; box-shadow: 0 0 16px rgb(34 211 238 / .08); }
  .terminal-type-line { animation: none; clip-path: none; }
  .terminal-caret { animation: none; }
}
</style>

<style>
/* The theme class lives on HomeView, so these cross-component overrides must
   remain unscoped to reach the Hero subtree. */
.public-home-light .hero-stats { border-top-color: #cbd5e1; }
.public-home-light .hero-stat-value { color: #0f172a; }
.public-home-light .hero-stat dt { color: #64748b; }
.public-home-light .hero-stat-suffix { color: #047857; }
.public-home-light .hero-title-accent { color: #05845b; }
</style>
