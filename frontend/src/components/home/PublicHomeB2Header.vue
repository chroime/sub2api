<template>
  <PublicSiteHeader
    :site-name="siteName"
    :site-logo="siteLogo"
    :subtitle="subtitle"
    :destination="destination"
    :authenticated="authenticated"
  >
    <RouterLink v-if="modelPlazaHref" :to="modelPlazaHref" class="public-nav-link">{{ t('home.public.nav.modelPlaza') }}</RouterLink>
    <RouterLink v-if="docsHref === '/docs'" to="/docs" class="public-nav-link">{{ t('home.public.nav.docs') }}</RouterLink>
    <a v-else :href="docsHref" class="public-nav-link">{{ t('home.public.nav.docs') }}</a>
  </PublicSiteHeader>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import PublicSiteHeader from '@/components/common/PublicSiteHeader.vue'

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
</script>
