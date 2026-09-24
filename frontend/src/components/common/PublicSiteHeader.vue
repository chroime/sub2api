<template>
  <header class="public-site-header public-site-header-dark" :class="{ 'public-site-header-story': variant === 'story' }">
    <nav class="public-header-inner">
      <RouterLink to="/home" class="public-brand">
        <SiteLogo :id="variant === 'story' ? 'brand-logo' : undefined" :src="siteLogo" :alt="siteName" class="public-brand-icon" />
        <span class="public-brand-name">{{ siteName }}</span>
        <span v-if="subtitle" class="public-brand-subtitle">{{ subtitle }}</span>
      </RouterLink>
      <div class="public-header-actions">
        <slot />
        <button :id="variant === 'story' ? 'theme-toggle' : undefined" type="button" class="public-theme-toggle" :aria-label="isDark ? t('home.switchToLight') : t('home.switchToDark')" :title="isDark ? t('home.switchToLight') : t('home.switchToDark')" @click="toggleTheme">
          <Icon v-if="isDark" name="sun" size="md" aria-hidden="true" />
          <Icon v-else name="moon" size="md" aria-hidden="true" />
        </button>
        <LocaleSwitcher class="public-header-locale" />
        <RouterLink :to="destination" class="public-nav-cta">
          {{ authenticated ? t('home.dashboard') : t('home.login') }}
        </RouterLink>
      </div>
    </nav>
  </header>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { onBeforeUnmount, onMounted, ref } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import LocaleSwitcher from './LocaleSwitcher.vue'
import SiteLogo from './SiteLogo.vue'
import Icon from '@/components/icons/Icon.vue'
import { setTheme } from '@/utils/theme'

defineProps<{
  siteName: string
  siteLogo?: string
  subtitle?: string
  destination: RouteLocationRaw
  authenticated: boolean
  animatedMascot?: boolean
  variant?: 'default' | 'story'
}>()
const { t } = useI18n()
const emit = defineEmits<{ 'theme-change': [isDark: boolean] }>()
const isDark = ref(document.documentElement.classList.contains('dark'))
let themeObserver: MutationObserver | undefined

function syncTheme() {
  isDark.value = document.documentElement.classList.contains('dark')
}

function toggleTheme() {
  const nextTheme = document.documentElement.classList.contains('dark') ? 'light' : 'dark'
  try {
    setTheme(nextTheme)
  } catch {
    // The document theme still changes when preference storage is unavailable.
  }
  syncTheme()
  emit('theme-change', isDark.value)
}

onMounted(() => {
  syncTheme()
  themeObserver = new MutationObserver(syncTheme)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

onBeforeUnmount(() => themeObserver?.disconnect())
</script>

<style scoped>
.public-site-header {
  --header-text: var(--public-muted);
  --header-strong: var(--public-heading);
  --header-border: var(--public-border);
  --header-hover: var(--public-hover);
  position: sticky;
  top: 0;
  z-index: 30;
  border-bottom: 1px solid var(--header-border);
  background: var(--public-bg);
  color: var(--header-text);
  backdrop-filter: blur(24px);
  font-family: ui-sans-serif, system-ui, sans-serif;
  font-synthesis: none;
  font-size: 14px;
  font-weight: 400;
  line-height: 20px;
  letter-spacing: 0;
}
.public-site-header-story {
  position: relative;
  border-bottom: 0;
  background: var(--public-bg);
  backdrop-filter: none;
}
.public-site-header-story .public-header-inner {
  width: min(1160px, calc(100% - 72px));
  max-width: none;
  min-height: 76px;
  padding: 16px 0;
}
.public-site-header-story .public-brand { gap: 9px; }
.public-site-header-story .public-brand-icon { width: 28px; height: 28px; border-radius: 0; }
.public-site-header-story .public-brand-name { font-size: 21px; font-weight: 720; line-height: 1; }
.public-site-header-story .public-brand-subtitle { max-width: 180px; padding-left: 11px; font-size: 12px; line-height: 1.2; }
.public-header-inner { display: flex; align-items: center; justify-content: space-between; gap: 12px; max-width: 1280px; min-height: 72px; margin: 0 auto; padding: 16px 20px; }
.public-brand { display: flex; min-width: 0; align-items: center; gap: 12px; color: var(--header-strong); }
.public-brand-icon { width: 36px; height: 36px; flex-shrink: 0; border-radius: 8px; }
.public-brand-name { max-width: 140px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 16px; font-weight: 600; line-height: 24px; }
.public-brand-subtitle { display: none; max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; border-left: 1px solid var(--header-border); padding-left: 12px; color: var(--header-text); font-size: 12px; }
.public-header-actions { display: flex; flex-shrink: 0; align-items: center; gap: 8px; }
.public-theme-toggle { display: inline-flex; min-height: 40px; width: 40px; align-items: center; justify-content: center; border: 0; border-radius: 8px; background: transparent; color: var(--header-text); cursor: pointer; transition: background-color 160ms ease, color 160ms ease; }
.public-theme-toggle:hover { background: var(--header-hover); color: var(--header-strong); }
.public-theme-toggle:focus-visible { outline: 2px solid var(--public-accent); outline-offset: 3px; }
.public-header-actions :deep(.public-nav-link) { display: none; align-items: center; min-height: 40px; padding: 0; color: var(--header-text); background: transparent; border-radius: 0; font-size: 14px; font-weight: 400; line-height: 20px; letter-spacing: 0; transition: none; }
.public-header-actions :deep(.public-nav-link:hover) { color: var(--header-strong); background: transparent; }
.public-header-locale :deep(button) { font-family: inherit; font-size: 14px; font-weight: 400; letter-spacing: 0; }
.public-header-locale :deep(> button) { min-height: 40px; color: var(--header-text); }
.public-header-locale :deep(> button:hover) { background: var(--header-hover); color: var(--header-strong); }
.public-header-locale :deep(> div) { background: var(--public-surface); border-color: var(--public-border); }
.public-header-locale :deep(> div > button) { background: transparent; color: var(--public-muted); }
.public-header-locale :deep(> div > button:hover), .public-header-locale :deep(> div > button.bg-primary-50) { background: var(--public-hover); color: var(--public-heading); }
.public-header-locale :deep(svg) { color: inherit; }
.public-nav-cta { display: inline-flex; min-width: 88px; min-height: 44px; flex-shrink: 0; align-items: center; justify-content: center; padding: 10px 18px; border: 1px solid transparent; border-radius: 8px; background: var(--public-cta-bg); color: var(--public-cta-ink); font-family: "Microsoft YaHei", "PingFang SC", ui-sans-serif, system-ui, sans-serif; font-size: 15px; font-weight: 600; line-height: 22px; white-space: nowrap; text-shadow: none; transition: background-color 160ms ease, border-color 160ms ease, color 160ms ease, box-shadow 160ms ease; }
.public-nav-cta:hover { background: var(--public-cta-hover); color: var(--public-cta-ink); }
.public-nav-cta:active { background: var(--public-cta-hover); box-shadow: inset 0 2px 4px rgb(0 0 0 / .12); }
.public-nav-cta:focus-visible { outline: 2px solid var(--public-accent); outline-offset: 3px; }
@media (prefers-reduced-motion: reduce) { .public-nav-cta { transition: none; } }
@media (max-width: 639px) {
  .public-nav-cta { padding-right: 12px; padding-left: 12px; }
}
@media (min-width: 640px) {
  .public-header-inner { padding-right: 32px; padding-left: 32px; }
  .public-header-actions { gap: 16px; }
  .public-brand-name { max-width: 240px; }
}
@media (min-width: 768px) {
  .public-header-actions :deep(.public-nav-link) { display: inline-flex; }
}
@media (min-width: 1024px) {
  .public-header-inner { padding-right: 40px; padding-left: 40px; }
  .public-brand-subtitle { display: inline; }
}
@media (max-width: 1200px) {
  .public-site-header-story .public-header-inner { width: calc(100% - 48px); }
}
@media (max-width: 720px) {
  .public-site-header-story .public-header-inner { width: calc(100% - 40px); min-height: 66px; padding: 16px 0; }
  .public-site-header-story .public-brand-name { font-size: 19px; }
  .public-site-header-story .public-brand-subtitle { max-width: 150px; font-size: 11px; }
}
@media (max-width: 359px) {
  .public-site-header-story .public-header-inner { width: calc(100% - 32px); }
  .public-site-header-story .public-brand-subtitle { max-width: 80px; padding-left: 7px; font-size: 10px; }
}
</style>
