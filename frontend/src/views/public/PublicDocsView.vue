<template>
  <div class="min-h-screen bg-[#070c11] text-slate-100">
    <PublicSiteHeader :site-name="siteName" :site-logo="siteLogo" :subtitle="t('publicDocs.title')" :destination="destination" :authenticated="authStore.isAuthenticated">
      <RouterLink to="/home" class="public-nav-link">{{ t('publicDocs.home') }}</RouterLink>
      <RouterLink to="/model-plaza" class="public-nav-link">{{ t('publicDocs.modelPlaza') }}</RouterLink>
    </PublicSiteHeader>
    <main class="mx-auto max-w-7xl px-5 py-8 sm:px-8 sm:py-12 lg:px-10">
      <div v-if="loading" class="min-h-80 py-20 text-center text-sm text-slate-400" role="status" aria-busy="true">{{ t('publicDocs.loading') }}</div>
      <div v-else-if="loadError" class="min-h-80 py-20 text-center" role="alert">
        <h1 class="text-xl font-semibold text-white">{{ t('publicDocs.loadFailed') }}</h1>
        <button type="button" class="mt-6 inline-flex items-center gap-2 rounded-lg border border-white/15 px-4 py-2 text-sm text-cyan-200 hover:border-cyan-300" @click="loadSettings">
          <Icon name="refresh" size="sm" />{{ t('publicDocs.retry') }}
        </button>
      </div>
      <PublicDocsContent v-else :title="settings?.docs_title" :content="documentContent" />
    </main>
    <footer class="mx-auto mt-10 flex max-w-7xl flex-wrap items-center justify-between gap-4 border-t border-white/10 px-5 py-6 text-xs text-slate-500 sm:px-8 lg:px-10">
      <span>&copy; {{ new Date().getFullYear() }} {{ siteName }}</span>
      <RouterLink to="/home" class="inline-flex items-center gap-2 text-cyan-200"><Icon name="arrowLeft" size="xs" />{{ t('publicDocs.home') }}</RouterLink>
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
