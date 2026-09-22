<template>
  <div v-if="open" class="mt-2 flex min-h-6 flex-wrap items-center gap-x-2 gap-y-1 text-xs">
    <span
      role="status"
      aria-live="polite"
      data-testid="streaming-ack-status"
      :class="statusClass"
    >{{ t(`admin.accounts.openai.syntheticFirstResponseStatus${status}`) }}</span>
    <button
      type="button"
      data-testid="streaming-ack-refresh"
      class="inline-flex h-6 w-6 shrink-0 items-center justify-center rounded text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 disabled:opacity-50 dark:text-gray-400 dark:hover:bg-dark-700 dark:hover:text-gray-100"
      :disabled="loading"
      :title="t('admin.accounts.openai.syntheticFirstResponseRefresh')"
      :aria-label="t('admin.accounts.openai.syntheticFirstResponseRefresh')"
      @click="loadGlobalSetting"
    >
      <Icon name="refresh" size="xs" :class="{ 'animate-spin motion-reduce:animate-none': loading }" />
    </button>
    <a
      v-if="globalEnabled === false || failed"
      href="/admin/settings#streaming-ack-settings"
      target="_blank"
      rel="noopener noreferrer"
      data-testid="streaming-ack-settings-link"
      class="inline-flex items-center gap-1 text-primary-600 underline-offset-2 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 dark:text-primary-400"
    >
      {{ t('admin.accounts.openai.syntheticFirstResponseSettings') }}
      <Icon name="externalLink" size="xs" aria-hidden="true" />
    </a>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getStreamingACKSettings } from '@/api/admin/settings'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{
  open: boolean
  enabled: boolean
  savedEnabled: boolean | null
}>()

const { t } = useI18n()
const globalEnabled = ref<boolean | null>(null)
const loading = ref(false)
const failed = ref(false)
let requestVersion = 0

const status = computed(() => {
  if (loading.value) return 'Loading'
  if (failed.value || globalEnabled.value === null) return 'Unavailable'
  if (props.enabled !== (props.savedEnabled ?? false)) return 'Pending'
  if (!props.enabled) return 'AccountOff'
  return globalEnabled.value ? 'Enabled' : 'GlobalOff'
})

const statusClass = computed(() => {
  if (status.value === 'Enabled') return 'text-emerald-700 dark:text-emerald-400'
  if (['GlobalOff', 'Unavailable', 'Pending'].includes(status.value)) return 'text-amber-700 dark:text-amber-400'
  return 'text-gray-500 dark:text-gray-400'
})

async function loadGlobalSetting() {
  if (!props.open) return
  const version = ++requestVersion
  loading.value = true
  failed.value = false
  globalEnabled.value = null
  try {
    const settings = await getStreamingACKSettings()
    if (version !== requestVersion) return
    if (typeof settings.enabled !== 'boolean') throw new Error('Invalid streaming ACK setting')
    globalEnabled.value = settings.enabled
  } catch {
    if (version === requestVersion) failed.value = true
  } finally {
    if (version === requestVersion) loading.value = false
  }
}

function refreshOnFocus() {
  if (props.open && !loading.value) void loadGlobalSetting()
}

watch(() => props.open, (open) => {
  requestVersion++
  if (open) {
    void loadGlobalSetting()
  } else {
    loading.value = false
    globalEnabled.value = null
    failed.value = false
  }
}, { immediate: true })

onMounted(() => window.addEventListener('focus', refreshOnFocus))
onUnmounted(() => {
  requestVersion++
  window.removeEventListener('focus', refreshOnFocus)
})
</script>
