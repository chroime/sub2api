<template>
  <div class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 bg-white px-4 py-3 text-sm text-gray-700 dark:border-dark-700 dark:bg-dark-900 dark:text-gray-200">
      <span>{{ t('admin.settings.site.homepagePreviewNotice') }}</span>
      <RouterLink to="/admin/settings" class="font-medium text-primary-600 dark:text-primary-400">{{ t('admin.settings.site.homepageBackToSettings') }}</RouterLink>
    </div>
    <HomepageRenderer v-if="draft" :settings="draft" />
    <p v-else role="alert" class="mx-auto max-w-xl p-8 text-gray-700 dark:text-gray-200">{{ t('admin.settings.site.homepagePreviewExpired') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useRoute } from 'vue-router'
import HomepageRenderer from '@/components/home/HomepageRenderer.vue'
import { readHomepagePreview } from '@/utils/homepagePreview'
import { applyTheme, getInitialTheme } from '@/utils/theme'

const { t } = useI18n()
const route = useRoute()
// Keep an in-memory snapshot so a newer draft in another tab cannot alter this preview.
const draft = readHomepagePreview(route.query.draft)
applyTheme(getInitialTheme())
</script>
