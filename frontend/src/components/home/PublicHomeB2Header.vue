<template>
  <header class="site-header shell">
    <RouterLink to="/home" class="wordmark">
      <SiteLogo :src="siteLogo" :alt="siteName" class="brand-logo" :width="28" :height="28" />
      <span class="wordmark-name">{{ siteName }}</span>
      <span v-if="subtitle" class="site-subtitle">{{ subtitle }}</span>
    </RouterLink>
    <nav class="header-links">
      <RouterLink v-if="modelPlazaHref" :to="modelPlazaHref" class="desktop-link">{{ t('home.public.nav.modelPlaza') }}</RouterLink>
      <a :href="docsHref" class="desktop-link">{{ t('home.public.nav.docs') }}</a>
      <button id="theme-toggle" class="icon-button" type="button" :aria-label="themeLabel" :title="themeLabel" @click="toggleTheme">
        <Icon class="icon" :name="isDark ? 'sun' : 'moon'" :data-theme-icon="isDark ? 'sun' : 'moon'" aria-hidden="true" />
      </button>
      <LocaleSwitcher class="b2-locale" />
      <RouterLink :to="destination" class="login">{{ authenticated ? t('home.dashboard') : t('home.login') }}</RouterLink>
    </nav>
  </header>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SiteLogo from '@/components/common/SiteLogo.vue'
import Icon from '@/components/icons/Icon.vue'
import { setTheme } from '@/utils/theme'

defineProps<{
  siteName: string
  siteLogo?: string
  subtitle?: string
  destination: string
  authenticated: boolean
  docsHref: string
  modelPlazaHref?: string
}>()

const { t } = useI18n()
const isDark = ref(document.documentElement.classList.contains('dark'))
const themeLabel = computed(() => t(isDark.value ? 'home.switchToLight' : 'home.switchToDark'))
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
}

onMounted(() => {
  syncTheme()
  themeObserver = new MutationObserver(syncTheme)
  themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
})

onBeforeUnmount(() => themeObserver?.disconnect())
</script>
