<template>
  <HomepageRenderer v-if="appStore.publicSettingsLoaded" :settings="appStore.cachedPublicSettings || {}" />
  <div v-else class="min-h-screen bg-gray-50 dark:bg-dark-950" aria-busy="true" />
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useAuthStore, useAppStore } from '@/stores'
import HomepageRenderer from '@/components/home/HomepageRenderer.vue'
import { applyTheme, getInitialTheme } from '@/utils/theme'

const appStore = useAppStore()
const authStore = useAuthStore()
onMounted(() => {
  applyTheme(getInitialTheme())
  authStore.checkAuth()
  if (!appStore.publicSettingsLoaded) void appStore.fetchPublicSettings()
})
</script>
