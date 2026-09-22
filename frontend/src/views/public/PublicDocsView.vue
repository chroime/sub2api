<template>
  <div class="public-theme public-docs-page min-h-screen">
    <PublicSiteHeader :site-name="siteName" :site-logo="siteLogo" :subtitle="t('publicDocs.title')" :destination="destination" :authenticated="authStore.isAuthenticated">
      <RouterLink to="/home" class="public-nav-link">{{ t('publicDocs.home') }}</RouterLink>
      <RouterLink to="/model-plaza" class="public-nav-link">{{ t('publicDocs.modelPlaza') }}</RouterLink>
    </PublicSiteHeader>
    <main class="mx-auto max-w-7xl px-5 py-8 sm:px-8 sm:py-12 lg:px-10">
      <div v-if="loading" class="docs-loading min-h-80 py-20 text-center text-sm" role="status" aria-busy="true">{{ t('publicDocs.loading') }}</div>
      <div v-else-if="loadError" class="min-h-80 py-20 text-center" role="alert">
        <h1 class="docs-error-title text-xl font-semibold">{{ t('publicDocs.loadFailed') }}</h1>
        <button type="button" class="docs-retry mt-6 inline-flex items-center gap-2 rounded-lg border px-4 py-2 text-sm" @click="loadSettings">
          <Icon name="refresh" size="sm" />{{ t('publicDocs.retry') }}
        </button>
      </div>
      <PublicDocsContent v-else :title="settings?.docs_title" :content="documentContent" />
    </main>
    <footer class="docs-footer mx-auto mt-10 flex max-w-7xl flex-wrap items-center justify-between gap-4 border-t px-5 py-6 text-xs sm:px-8 lg:px-10">
      <span>&copy; {{ new Date().getFullYear() }} {{ siteName }}</span>
      <RouterLink to="/home" class="docs-footer-link inline-flex items-center gap-2"><Icon name="arrowLeft" size="xs" />{{ t('publicDocs.home') }}</RouterLink>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import PublicSiteHeader from '@/components/common/PublicSiteHeader.vue'
import Icon from '@/components/icons/Icon.vue'
import PublicDocsContent from '@/components/docs/PublicDocsContent.vue'
import { sanitizeUrl } from '@/utils/url'
import { buildIntegrationGuide } from '@/utils/integrationGuide'

const { t, locale } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()
const settings = computed(() => appStore.cachedPublicSettings)
const documentContent = computed(() => settings.value?.docs_content?.trim()
  ? settings.value.docs_content
  : buildIntegrationGuide(locale.value, settings.value?.api_base_url))
const siteName = computed(() => settings.value?.site_name || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(settings.value?.site_logo || '', { allowRelative: true, allowDataUrl: true }))
const destination = computed(() => authStore.isAuthenticated ? (authStore.isAdmin ? '/admin/dashboard' : '/dashboard') : '/login')
const loading = ref(!settings.value)
const loadError = ref(false)
async function loadSettings() {
  loading.value = true
  loadError.value = false
  try {
    const result = await appStore.fetchPublicSettings()
    loadError.value = !result && !settings.value
  } catch {
    loadError.value = !settings.value
  } finally {
    loading.value = false
  }
}

onMounted(() => { if (!settings.value) void loadSettings() })
</script>

<style scoped>
.public-docs-page { background: var(--public-bg); color: var(--public-text); }
.docs-loading, .docs-footer { color: var(--public-muted); }
.docs-error-title { color: var(--public-heading); }
.docs-retry { border-color: var(--public-border); background: var(--public-surface); color: var(--public-accent); transition: border-color 160ms ease, background-color 160ms ease; }
.docs-retry:hover { border-color: var(--public-accent); background: var(--public-accent-soft); }
.docs-retry:focus-visible, .docs-footer-link:focus-visible { outline: 2px solid var(--public-accent); outline-offset: 4px; }
.docs-footer { border-color: var(--public-border); }
.docs-footer-link { color: var(--public-accent); }
.docs-footer-link:hover { color: var(--public-accent-hover); }
</style>
