<template>
  <p v-if="loadFailed" role="alert" class="bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-100">
    {{ t('admin.settings.site.homepageLoadFallback') }}
  </p>
  <component :is="presentation" v-bind="presentationProps" />
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, ref, watch, type AsyncComponentLoader } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores'
import type { PublicSettings } from '@/types'
import CompactHome from './CompactHome.vue'
import HomepageLoading from './HomepageLoading.vue'
import { resolveHomepageTemplate } from '@/utils/homepageTemplate'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, resolveFeatureFlag } from '@/utils/featureFlags'

const props = defineProps<{ settings: Partial<PublicSettings> }>()
const authStore = useAuthStore()
const { t } = useI18n()
const loadFailed = ref(false)
const template = computed(() => resolveHomepageTemplate(props.settings))
watch(template, () => { loadFailed.value = false })
function lazyTemplate(loader: AsyncComponentLoader) {
  return defineAsyncComponent({
    loader,
    loadingComponent: HomepageLoading,
    errorComponent: CompactHome,
    delay: 0,
    timeout: 20000,
    onError(_error, _retry, fail) { loadFailed.value = true; fail() },
  })
}
const templates = {
  classic: lazyTemplate(() => import('./PublicHomeExperience.vue')),
  brand: lazyTemplate(() => import('./BrandHome.vue')),
  compact: CompactHome,
  custom: lazyTemplate(() => import('./CustomHome.vue')),
}
const presentation = computed(() => templates[template.value])
const presentationProps = computed(() => {
  const settings = props.settings
  const shared = {
    siteName: settings.site_name || 'Sub2API',
    siteLogo: sanitizeUrl(settings.site_logo || '', { allowRelative: true, allowDataUrl: true }),
    subtitle: settings.site_subtitle || t('home.public.nav.apiGateway'),
    docUrl: sanitizeUrl(settings.doc_url || '') || '/docs',
    apiBaseUrl: sanitizeUrl(settings.api_base_url || '', { allowRelative: true }),
    destination: authStore.isAuthenticated ? (authStore.isAdmin ? '/admin/dashboard' : '/dashboard') : '/login',
    authenticated: authStore.isAuthenticated,
    showModelPlazaEntry: resolveFeatureFlag(settings, FeatureFlags.modelPlaza)
      && (!settings.model_plaza_require_auth || authStore.isAuthenticated)
      && (!settings.backend_mode_enabled || (authStore.isAuthenticated && authStore.isAdmin)),
    currentYear: new Date().getFullYear(),
  }
  if (template.value === 'brand') return { ...shared, contactInfo: settings.contact_info || '' }
  if (template.value === 'custom') return { ...shared, homeContent: settings.home_content || '' }
  return shared
})
</script>
