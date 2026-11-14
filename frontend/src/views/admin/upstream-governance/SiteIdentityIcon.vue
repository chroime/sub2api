<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import type { Site } from '@/api/admin/upstream-governance'
import sub2apiLogo from '@/assets/governance/sub2api.svg'
import newapiLogo from '@/assets/governance/newapi.png'
import { loadSiteIcon } from './site-icon'

const props = defineProps<{ site?: Pick<Site, 'id' | 'version' | 'name' | 'platform' | 'base_url' | 'proxy_id'> }>()
const failedSource = ref('')
const favicon = ref('')
let generation = 0
const platformLogo = computed(() => props.site?.platform === 'sub2api' ? sub2apiLogo : newapiLogo)
const platformName = computed(() => props.site?.platform === 'sub2api' ? 'Sub2API' : 'New API')
const sourceIdentity = computed(() => {
  try {
    const url = new URL(props.site?.base_url || '')
    if (url.protocol !== 'https:' || url.username || url.password || !props.site?.id) return ''
    return JSON.stringify([props.site.id, props.site.version, url.origin, props.site.platform, props.site.proxy_id])
  } catch { return '' }
})
watch(sourceIdentity, async identity => {
  const current = ++generation
  favicon.value = ''; failedSource.value = ''
  if (!identity || !props.site) return
  const source = await loadSiteIcon(props.site)
  if (current === generation) favicon.value = source
}, { immediate: true })
onUnmounted(() => { generation++ })
</script>

<template>
  <span class="relative inline-flex h-6 w-7 shrink-0 items-center">
    <img v-if="favicon && failedSource !== favicon" :key="favicon" data-test="site-favicon" :src="favicon" :alt="site?.name || ''" width="20" height="20" class="h-5 w-5 rounded object-contain" loading="lazy" decoding="async" referrerpolicy="no-referrer" @error="failedSource = favicon" />
    <img v-else data-test="site-icon-fallback" :src="platformLogo" alt="" width="20" height="20" class="h-5 w-5 rounded object-contain" />
    <img data-test="site-platform-logo" :src="platformLogo" :alt="platformName" :title="platformName" width="12" height="12" class="absolute bottom-0 right-0 h-3 w-3 rounded-sm bg-white object-contain ring-1 ring-white dark:bg-dark-800 dark:ring-dark-800" />
  </span>
</template>
