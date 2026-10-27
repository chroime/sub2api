<template>
  <div data-testid="brand-home" class="public-home public-home-b2 public-theme">
    <PublicHomeB2Header :site-name="siteName" :site-logo="siteLogo" :subtitle="subtitle" :destination="destination" :authenticated="authenticated" :docs-href="docUrl" :model-plaza-href="showModelPlazaEntry ? '/model-plaza' : ''" />

    <main id="top" class="shell">
      <PublicHomeStoryB2 :site-name="siteName" primary-href="/keys" :primary-label="t('home.public.hero.createApiKey')" :secondary-href="docUrl" :secondary-label="t('home.public.nav.docs')" :details-href="showModelPlazaEntry ? '/model-plaza' : ''" />

      <PublicHomeB2Integration :base-url="gatewayBaseUrl" :model="featuredModel" :docs-href="docUrl" />
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
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import PublicHomeB2Header from './PublicHomeB2Header.vue'
import PublicHomeStoryB2 from './PublicHomeStoryB2.vue'
import PublicHomeB2Integration from './PublicHomeB2Integration.vue'
import { parseContactInfo } from '@/utils/contactInfo'
import { usePublicPlatformHome } from '@/composables/usePublicPlatformHome'
import type { HomepagePresentationProps } from '@/utils/homepageTemplate'

const props = defineProps<HomepagePresentationProps & { contactInfo: string }>()
const { t } = useI18n()
const homeData = usePublicPlatformHome({ includeChannels: false })
const contacts = computed(() => parseContactInfo(props.contactInfo))
const gatewayBaseUrl = computed(() => {
  const base = (props.apiBaseUrl || window.location.origin).replace(/\/+$/, '')
  return /\/v1$/i.test(base) ? base : `${base}/v1`
})
const featuredModel = computed(() => homeData.pricingRows.value[0]?.model || 'gpt-5.6-sol')
onMounted(() => { void homeData.load() })
// usePublicPlatformHome cancels its requests when this template unmounts.
</script>

<style src="./publicHomeB2.css"></style>
