<template>
  <AppLayout>
    <MonitorHero
      :loading="loading"
      :auto-refresh="autoRefresh"
      @refresh="refresh()"
    />

    <MonitorCardGrid
      :items="displayItems"
      :countdown-seconds="countdown"
      :loading="loading"
      :intelligence-loading="analyticsLoading"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onBeforeUnmount, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'
import {
  list as listChannelMonitorViews,
  analytics as fetchChannelMonitorAnalytics,
  type UserMonitorAnalytics,
  type UserMonitorView,
} from '@/api/channelMonitor'
import AppLayout from '@/components/layout/AppLayout.vue'
import MonitorHero from '@/components/user/monitor/MonitorHero.vue'
import MonitorCardGrid from '@/components/user/monitor/MonitorCardGrid.vue'
import { DEFAULT_INTERVAL_SECONDS } from '@/constants/channelMonitor'
import { useAutoRefresh } from '@/composables/useAutoRefresh'

const { t } = useI18n()
const appStore = useAppStore()

// ── State ──
const items = ref<UserMonitorView[]>([])
const analyticsByID = ref<Record<number, UserMonitorAnalytics>>({})
const loading = ref(false)
const analyticsLoading = ref(false)

let abortController: AbortController | null = null
let analyticsAbortController: AbortController | null = null
let analyticsGeneration = 0

const autoRefresh = useAutoRefresh({
  storageKey: 'channel-status-auto-refresh',
  intervals: [30, 60, 120] as const,
  defaultInterval: DEFAULT_INTERVAL_SECONDS,
  defaultEnabled: true,
  onRefresh: () => refresh(true),
  shouldPause: () => document.hidden || loading.value,
})
const countdown = autoRefresh.countdown

// ── Computed ──
const displayItems = computed(() => items.value.map(item => {
  const analytics = analyticsByID.value[item.id]
  return {
    ...item,
    intelligence: analytics?.intelligence ?? item.intelligence ?? null,
    usage_metrics: analytics?.usage_metrics ?? item.usage_metrics,
  }
}))

// ── Loaders ──
async function reload(silent = false) {
  if (abortController) abortController.abort()
  const ctrl = new AbortController()
  abortController = ctrl
  if (!silent) loading.value = true
  try {
    const res = await listChannelMonitorViews({ signal: ctrl.signal })
    if (ctrl.signal.aborted || abortController !== ctrl) return
    items.value = res.items || []
    void reloadAnalytics(items.value.map(item => item.id))
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED') return
    appStore.showError(extractApiErrorMessage(err, t('channelStatus.loadError')))
  } finally {
    if (abortController === ctrl) {
      if (!silent) loading.value = false
      autoRefresh.resetCountdown()
      abortController = null
    }
  }
}

async function reloadAnalytics(ids: number[]) {
  analyticsGeneration++
  const generation = analyticsGeneration
  if (analyticsAbortController) analyticsAbortController.abort()
  if (ids.length === 0) {
    analyticsByID.value = {}
    analyticsLoading.value = false
    analyticsAbortController = null
    return
  }

  const ctrl = new AbortController()
  analyticsAbortController = ctrl
  analyticsLoading.value = true
  try {
    const response = await fetchChannelMonitorAnalytics({ signal: ctrl.signal })
    if (ctrl.signal.aborted || analyticsGeneration !== generation) return
    const next: Record<number, UserMonitorAnalytics> = {}
    for (const item of response.items || []) next[item.id] = item
    analyticsByID.value = next
  } catch (err: unknown) {
    const e = err as { name?: string; code?: string }
    if (e?.name === 'AbortError' || e?.code === 'ERR_CANCELED' || ctrl.signal.aborted) return
  } finally {
    if (analyticsAbortController === ctrl) {
      analyticsAbortController = null
      analyticsLoading.value = false
    }
  }
}

async function refresh(silent = false) {
  await reload(silent)
}

watch(
  () => appStore.cachedPublicSettings?.channel_monitor_enabled,
  (enabled) => {
    if (enabled === false) autoRefresh.stop()
    else if (autoRefresh.enabled.value) autoRefresh.start()
  },
)

onMounted(() => {
  void reload(false)
  if (appStore.cachedPublicSettings?.channel_monitor_enabled !== false) {
    autoRefresh.setEnabled(autoRefresh.enabled.value)
  }
})

onBeforeUnmount(() => {
  if (abortController) abortController.abort()
  if (analyticsAbortController) analyticsAbortController.abort()
})
</script>
