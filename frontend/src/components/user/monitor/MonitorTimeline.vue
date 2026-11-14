<template>
  <div class="mt-3 w-full border-t border-gray-200/70 pt-3 dark:border-dark-700">
    <div
      class="mb-2 flex flex-wrap items-center justify-between gap-x-2 gap-y-1 text-[10px] leading-4 text-gray-500 dark:text-gray-400"
    >
      <span>{{ t('monitorCommon.history60pts', { n: recordCount }) }}</span>
      <span class="tabular-nums">{{ t('monitorCommon.nextUpdateIn', { n: countdownSeconds }) }}</span>
    </div>

    <div
      v-if="maintenance"
      class="flex h-5 w-full items-center justify-center rounded border border-dashed border-gray-300 dark:border-dark-600 text-[10px] uppercase tracking-widest text-gray-400"
    >
      {{ t('monitorCommon.maintenancePaused') }}
    </div>
    <div v-else class="flex h-6 w-full items-end gap-1">
      <div
        v-for="(bar, idx) in displayBars"
        :key="idx"
        class="min-w-0 flex-1 rounded-[3px]"
        data-testid="history-bar"
        :data-status="bar.status"
        :class="bar.colorClass"
        :style="{ height: bar.heightPct + '%' }"
        :title="bar.title"
      ></div>
    </div>

    <div
      class="mt-1.5 flex justify-between text-[9px] leading-3 uppercase tracking-wider text-gray-500 dark:text-gray-400"
    >
      <span>{{ t('monitorCommon.past') }}</span>
      <span>{{ t('monitorCommon.now') }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorTimelinePoint } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { availabilityBars } from '@/utils/monitorBars'

const props = withDefaults(defineProps<{
  buckets?: MonitorTimelinePoint[]
  countdownSeconds: number
  length?: number
  maintenance?: boolean
}>(), {
  buckets: () => [],
  length: 60,
  maintenance: false,
})

const { t } = useI18n()
const { statusLabel, formatLatency, formatRelativeTime } = useChannelMonitorFormat()

interface Bar {
  status: string
  colorClass: string
  heightPct: number
  title: string
}

const recordCount = computed(() => Math.min(props.buckets.length, props.length))

const displayBars = computed<Bar[]>(() => availabilityBars(props.buckets, props.length).map(bar => {
    const point = bar.point
    if (!point) return { ...bar, title: '' }
    const hasLatency = typeof point.latency_ms === 'number' && Number.isFinite(point.latency_ms) && point.latency_ms >= 0
    const latency = hasLatency ? `${formatLatency(point.latency_ms)}ms` : t('monitorCommon.latencyEmpty')
    const relative = formatRelativeTime(point.checked_at)
    const label = statusLabel(bar.status as MonitorTimelinePoint['status'])
    return {
      status: bar.status,
      colorClass: bar.colorClass,
      heightPct: bar.heightPct,
      title: `${relative} · ${label} · ${latency}`,
    }
}))
</script>
