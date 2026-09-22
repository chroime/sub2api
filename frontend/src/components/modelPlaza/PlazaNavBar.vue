<template>
  <PublicSiteHeader :site-name="siteName" :site-logo="siteLogo" :subtitle="t('publicDocs.modelPlaza')" :destination="destination" :authenticated="authStore.isAuthenticated">
    <RouterLink to="/home" class="public-nav-link">{{ t('publicDocs.home') }}</RouterLink>
    <RouterLink to="/docs" class="public-nav-link">{{ t('publicDocs.title') }}</RouterLink>
  </PublicSiteHeader>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import PublicSiteHeader from '@/components/common/PublicSiteHeader.vue'

const { t } = useI18n()
const appStore = useAppStore()
const authStore = useAuthStore()

const settings = computed(() => appStore.cachedPublicSettings)
const siteName = computed(() => settings.value?.site_name || 'Sub2API')
const siteLogo = computed(() =>
  sanitizeUrl(settings.value?.site_logo || '', { allowRelative: true, allowDataUrl: true })
)
const destination = computed(() => authStore.isAuthenticated
  ? (authStore.isAdmin ? '/admin/dashboard' : '/dashboard')
  : { path: '/login', query: { redirect: '/model-plaza' } })
</script>
