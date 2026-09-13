<template>
  <div v-if="hasHomeContent" class="min-h-screen bg-slate-950">
    <iframe v-if="isHomeContentUrl" :src="homeContent.trim()" class="h-screen w-full border-0" allowfullscreen />
    <div v-else v-html="homeContent" />
  </div>

  <div v-else-if="compactHomeEnabled" data-testid="compact-home" class="flex min-h-screen flex-col bg-gray-50 text-gray-900 dark:bg-dark-950 dark:text-white">
    <header class="border-b border-gray-200 px-4 py-4 dark:border-dark-800">
      <nav class="mx-auto flex max-w-5xl flex-wrap items-center justify-between gap-3">
        <div class="flex min-w-0 items-center gap-3">
          <SiteLogo :src="siteLogo" :alt="siteName" class="h-9 w-9 rounded-lg" />
          <span class="truncate text-base font-semibold">{{ siteName }}</span>
        </div>
        <div class="flex items-center gap-2">
          <LocaleSwitcher />
          <a :href="docUrl || '/docs'" :target="docUrl ? '_blank' : undefined" rel="noopener noreferrer" class="icon-button" :title="t('home.viewDocs')"><Icon name="book" size="md" /></a>
          <router-link v-if="showModelPlazaEntry" to="/model-plaza" class="compact-link"><Icon name="grid" size="md" /><span class="hidden sm:inline">{{ t('nav.modelPlaza') }}</span></router-link>
          <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="compact-cta">{{ isAuthenticated ? t('home.dashboard') : t('home.login') }}</router-link>
        </div>
      </nav>
    </header>
    <main class="flex flex-1 items-center justify-center px-4 py-16">
      <div class="max-w-2xl text-center">
        <SiteLogo :src="siteLogo" :alt="siteName" class="mx-auto mb-6 h-20 w-20 rounded-2xl" />
        <h1 class="text-3xl font-bold md:text-4xl">{{ siteName }}</h1>
        <p class="mt-4 whitespace-pre-wrap text-base text-gray-600 dark:text-dark-300">{{ siteSubtitle }}</p>
        <router-link :to="isAuthenticated ? dashboardPath : '/login'" class="mt-8 inline-flex min-h-10 items-center justify-center rounded-lg bg-primary-600 px-5 py-2.5 text-sm font-medium text-white hover:bg-primary-700">{{ isAuthenticated ? t('home.goToDashboard') : t('home.login') }}</router-link>
      </div>
    </main>
    <footer class="border-t border-gray-200 px-4 py-5 text-center text-sm text-gray-500 dark:border-dark-800 dark:text-dark-400">&copy; {{ currentYear }} {{ siteName }}</footer>
  </div>

  <div v-else class="public-home public-home-b2">
    <PublicHomeB2Header :site-name="siteName" :site-logo="siteLogo" :subtitle="siteSubtitle" :destination="isAuthenticated ? dashboardPath : '/login'" :authenticated="isAuthenticated" :docs-href="integrationHref" :model-plaza-href="showModelPlazaEntry ? '/model-plaza' : ''" />

    <main id="top" class="shell">
      <PublicHomeStoryB2 :site-name="siteName" primary-href="/register" :primary-label="t('home.public.hero.createApiKey')" :secondary-href="integrationHref" :secondary-label="t('home.public.nav.docs')" :details-href="showModelPlazaEntry ? '/model-plaza' : ''" />

      <PublicHomeB2Integration :base-url="gatewayBaseUrl" :model="featuredModel" :docs-href="integrationHref" />
    </main>

    <footer class="footer shell">
      <p>&copy; {{ currentYear }} {{ siteName }}</p>
      <div v-if="contacts.length" class="footer-contacts" :aria-label="t('home.public.b2.contacts')">
        <span v-for="(contact, index) in contacts" :key="`${contact.label}-${index}`">{{ contact.label }}<b>{{ contact.value }}</b></span>
      </div>
    </footer>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore, useAppStore } from '@/stores'
import LocaleSwitcher from '@/components/common/LocaleSwitcher.vue'
import SiteLogo from '@/components/common/SiteLogo.vue'
import PublicHomeB2Header from '@/components/home/PublicHomeB2Header.vue'
import Icon from '@/components/icons/Icon.vue'
import PublicHomeStoryB2 from '@/components/home/PublicHomeStoryB2.vue'
import PublicHomeB2Integration from '@/components/home/PublicHomeB2Integration.vue'
import '@/components/home/publicHomeB2.css'
import { parseContactInfo } from '@/utils/contactInfo'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import { usePublicPlatformHome } from '@/composables/usePublicPlatformHome'

const { t } = useI18n()
const authStore = useAuthStore()
const appStore = useAppStore()
const homeData = usePublicPlatformHome({ includeChannels: false })
const siteName = computed(() => appStore.cachedPublicSettings?.site_name || appStore.siteName || 'Sub2API')
const siteLogo = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.site_logo || appStore.siteLogo || '', { allowRelative: true, allowDataUrl: true }))
const siteSubtitle = computed(() => appStore.cachedPublicSettings?.site_subtitle || t('home.public.nav.apiGateway'))
const contactInfo = computed(() => appStore.cachedPublicSettings?.contact_info || '')
const contacts = computed(() => parseContactInfo(contactInfo.value))
const docUrl = computed(() => sanitizeUrl(appStore.cachedPublicSettings?.doc_url || appStore.docUrl || ''))
const homeContent = computed(() => appStore.cachedPublicSettings?.home_content || '')
const hasHomeContent = computed(() => homeContent.value.trim().length > 0)
const compactHomeEnabled = computed(() => appStore.cachedPublicSettings?.compact_home_enabled === true)
const modelPlazaEnabled = computed(() => isFeatureFlagEnabled(FeatureFlags.modelPlaza))
const isHomeContentUrl = computed(() => /^https?:\/\//i.test(homeContent.value.trim()))
const isAuthenticated = computed(() => authStore.isAuthenticated)
const isAdmin = computed(() => authStore.isAdmin)
const dashboardPath = computed(() => (isAdmin.value ? '/admin/dashboard' : '/dashboard'))
const showModelPlazaEntry = computed(() => modelPlazaEnabled.value && (!appStore.cachedPublicSettings?.model_plaza_require_auth || isAuthenticated.value))
const currentYear = computed(() => new Date().getFullYear())
const gatewayBaseUrl = computed(() => {
  const configured = typeof appStore.cachedPublicSettings?.api_base_url === 'string'
    ? sanitizeUrl(appStore.cachedPublicSettings.api_base_url, { allowRelative: true })
    : ''
  const base = (configured || window.location.origin).replace(/\/+$/, '')
  return /\/v1$/i.test(base) ? base : `${base}/v1`
})
const integrationHref = computed(() => docUrl.value || '/docs')
const pricingRows = computed(() => homeData.pricingRows.value)
const featuredModel = computed(() => pricingRows.value[0]?.model || 'gpt-5.6-sol')
onMounted(async () => {
  await authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) appStore.fetchPublicSettings()
  if (!hasHomeContent.value && !compactHomeEnabled.value) homeData.load()
})

onBeforeUnmount(() => homeData.abort())
</script>

<style scoped>
.compact-link { display:inline-flex; align-items:center; gap:.375rem; min-height:2.5rem; border-radius:.5rem; padding:.5rem .625rem; color:rgb(148 163 184); font-size:.75rem; font-weight:600; }
.compact-link { transition:color 180ms ease,background-color 180ms ease; }
.compact-link:hover { background:rgb(255 255 255 / .06); color:rgb(226 232 240); }
.compact-cta { display:inline-flex; min-height:2.5rem; align-items:center; justify-content:center; border-radius:.5rem; background:rgb(103 232 249); padding:.5rem .875rem; color:rgb(7 16 20); font-size:.75rem; font-weight:800; transition:background-color 180ms ease,box-shadow 180ms ease; }
.compact-cta:hover { background:rgb(165 243 252); box-shadow:0 0 22px rgb(103 232 249 / .2); }
.icon-button { display:inline-flex; height:2.5rem; width:2.5rem; align-items:center; justify-content:center; border-radius:.5rem; color:rgb(100 116 139); transition:color 180ms ease,background-color 180ms ease; }
.icon-button:hover { background:rgb(148 163 184 / .12); color:rgb(226 232 240); }
@media (prefers-reduced-motion: reduce) { .icon-button { transition:none; } }
</style>
