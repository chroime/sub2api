<template>
  <section v-if="rows.length" data-testid="monitor-intelligence" class="mt-3 w-full min-w-0 border-t border-gray-200/70 pt-3 dark:border-dark-700">
    <div class="flex min-w-0 items-center justify-between gap-2 whitespace-nowrap leading-4">
      <h4 class="text-xs font-semibold text-gray-800 dark:text-gray-100">{{ t('monitorCommon.intelligence.title') }}</h4>
      <span v-if="rows.length === 1" class="inline-flex items-center gap-1 text-[10px] font-medium" :class="textColors[rows[0].latest?.status || 'unknown']">
        <span class="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true"></span>
        {{ t(`monitorCommon.intelligence.${rows[0].latest?.status || 'unknown'}`) }}
      </span>
    </div>
    <div v-for="row in rows" :key="row.key" data-testid="intelligence-series" class="mt-1 min-w-0 [&+&]:mt-2">
      <p data-testid="intelligence-description" class="flex min-w-0 flex-nowrap items-baseline gap-1 overflow-hidden whitespace-nowrap text-[10px] leading-4 text-gray-500 dark:text-gray-400" :title="row.description">
        <span v-if="rows.length > 1" class="h-1.5 w-1.5 shrink-0 self-center rounded-full" :class="colors[row.latest?.status || 'unknown']" :title="t(`monitorCommon.intelligence.${row.latest?.status || 'unknown'}`)"></span>
        <span data-testid="intelligence-model" class="min-w-0 truncate font-mono" :title="row.model">{{ row.model }}</span>
        <span aria-hidden="true" class="shrink-0">·</span>
        <span data-testid="intelligence-effort" class="shrink-0">{{ row.effortLabel }}</span>
        <span aria-hidden="true" class="shrink-0">·</span>
        <span class="shrink-0">{{ t('monitorCommon.intelligence.windowShort') }}</span>
      </p>
      <div class="mt-2 grid h-5 w-full min-w-0 grid-cols-[repeat(60,minmax(0,1fr))] gap-px" :aria-label="row.description">
        <span v-for="(bar, index) in row.bars" :key="index" data-testid="intelligence-bar" :data-status="bar.status" class="min-w-0 rounded-[2px]" :class="colors[bar.status]" :title="bar.title"></span>
      </div>
    </div>
    <div class="mt-1.5 flex items-start justify-between gap-1 whitespace-nowrap text-[9px] leading-3 text-gray-500 dark:text-gray-400">
      <span class="flex min-w-0 flex-nowrap items-center gap-x-1.5" :title="t('monitorCommon.intelligence.legend')">
        <span v-for="status in legendStatuses" :key="status" class="inline-flex items-center gap-1">
          <span class="h-1 w-1 shrink-0 rounded-full" :class="colors[status]" aria-hidden="true"></span>{{ t(`monitorCommon.intelligence.legendLabels.${status}`) }}
        </span>
      </span>
      <time v-if="latest" :datetime="latest.checked_at" class="shrink-0 tabular-nums" :title="dateTime(latest.checked_at)">{{ time(latest.checked_at) }}</time>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { MonitorIntelligence, MonitorIntelligenceSeries } from '@/api/channelMonitor'
import { intelligenceRows, INTELLIGENCE_COLORS, INTELLIGENCE_STATUSES } from '@/utils/monitorBars'

const props = defineProps<{ summary?: MonitorIntelligence | null }>()
const { t } = useI18n()
type Point = MonitorIntelligenceSeries['points'][number]
type Status = Point['status'] | 'unknown'
const legendStatuses = INTELLIGENCE_STATUSES
const colors = INTELLIGENCE_COLORS
const textColors: Record<Status, string> = {
  pass: 'text-emerald-600 dark:text-emerald-300',
  fail: 'text-amber-600 dark:text-amber-300',
  error: 'text-orange-600 dark:text-orange-300',
  pending: 'text-sky-600 dark:text-sky-300',
  unknown: 'text-gray-400 dark:text-gray-500',
}
const time = (value: string) => new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })
const dateTime = (value: string) => new Date(value).toLocaleString()
const rows = computed(() => {
  return intelligenceRows(props.summary).map(series => {
    const bars = series.bars.map(bar => ({ ...bar,
      title: bar.point ? `${dateTime(bar.point.checked_at)} · ${t(`monitorCommon.intelligence.${bar.status}`)}` : t('monitorCommon.intelligence.unknown'),
    }))
    const effortLabel = `${t('monitorCommon.intelligence.thinkingEffort')} ${series.effort || t('monitorCommon.intelligence.defaultEffort')}`
    return {
      key: JSON.stringify([series.model, series.effort]),
      model: series.model, effortLabel, bars,
      description: [series.model, effortLabel, t('monitorCommon.intelligence.window')].join(' · '),
      latest: series.latest,
    }
  })
})
const latest = computed(() => rows.value.map(row => row.latest).filter((point): point is Point => !!point)
  .sort((a, b) => Date.parse(b.checked_at) - Date.parse(a.checked_at))[0])
</script>
