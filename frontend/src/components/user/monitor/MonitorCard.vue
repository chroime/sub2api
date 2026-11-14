<template>
  <button
    type="button"
    class="group flex min-w-0 w-full flex-col rounded-3xl border border-gray-200 bg-white p-4 text-left shadow-sm transition-[border-color,box-shadow] duration-200 hover:border-primary-300 hover:shadow-card-hover focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:border-dark-700 dark:bg-dark-800 dark:hover:border-primary-500/50 dark:focus-visible:ring-offset-dark-950"
    :aria-label="`${item.name} · ${statusLabel(item.primary_status)}`"
    :title="formatMonitorModel(item.primary_model)"
    @click="emit('click')"
  >
    <div class="grid min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-x-2 gap-y-1">
      <span
        class="row-span-2 grid h-9 w-9 shrink-0 place-items-center rounded-xl ring-1 ring-black/5 dark:ring-white/5"
        :class="providerBadgeClass(item.provider)"
      >
        <ProviderIcon :provider="item.provider" :size="21" />
      </span>
      <h3 data-testid="monitor-name" class="min-w-0 truncate text-base font-semibold leading-5 text-gray-900 dark:text-gray-100" :title="item.name">{{ item.name }}</h3>
      <span
        class="inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-[10px] font-semibold leading-4"
        :class="statusBadgeClass(item.primary_status)"
      >
        <span class="h-1.5 w-1.5 rounded-full bg-current" aria-hidden="true"></span>
        {{ statusLabel(item.primary_status) }}
      </span>
      <div class="col-span-2 col-start-2 flex min-w-0 flex-wrap items-center gap-1">
        <span
          class="inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[10px] font-medium leading-3"
          :class="providerBadgeClass(item.provider)"
        >
          {{ providerLabel(item.provider) }}
        </span>
        <span
          v-if="item.group_name && item.group_name !== item.name"
          class="max-w-full truncate rounded bg-gray-100 px-1.5 py-0.5 text-[10px] leading-3 text-gray-600 dark:bg-dark-700 dark:text-gray-300"
          :title="item.group_name"
        >
          {{ item.group_name }}
        </span>
        <span v-if="userRate !== null" data-testid="monitor-user-rate" class="inline-flex shrink-0 items-center gap-1 rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[10px] leading-3 tabular-nums text-gray-600 dark:bg-dark-700 dark:text-gray-300">
          <span class="font-sans">{{ t('monitorCommon.userRate') }}</span>{{ userRate }}x
        </span>
      </div>
    </div>

    <dl class="monitor-card-metrics mt-4 grid w-full min-w-0 grid-cols-3 gap-1.5" :aria-label="t(`channelStatus.windowTab.${window}`)">
      <div v-for="metric in metrics" :key="metric.key" :data-metric="metric.key" class="flex min-w-0 flex-col justify-between gap-3 rounded-xl border border-gray-200/80 bg-gray-50/80 px-2 py-2.5 dark:border-dark-700 dark:bg-dark-900/40" :title="`${metric.label} · ${t(`channelStatus.windowTab.${window}`)}`">
        <dt class="text-[10px] font-semibold leading-4 text-gray-500 dark:text-gray-400">{{ metric.label }}</dt>
        <dd class="whitespace-nowrap font-mono text-base font-bold leading-none tabular-nums" :class="metric.color">
          {{ metric.value }}<span v-if="metric.unit" class="ml-0.5 text-[0.6em] font-medium">{{ metric.unit }}</span>
        </dd>
      </div>
    </dl>

    <!-- 配额模式：最新用量/余额快照（服务端已按系统开关剥离，此处 flag 为纵深防御） -->
    <MonitorQuotaView v-if="quotaVisible" :snapshot="item.latest_quota" class="mt-2" />

    <div class="w-full">
      <MonitorTimeline :buckets="item.timeline" :length="timelineLength" :countdown-seconds="countdownSeconds" />
      <MonitorIntelligence :summary="item.intelligence" />
    </div>
  </button>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserMonitorView } from '@/api/channelMonitor'
import { useChannelMonitorFormat } from '@/composables/useChannelMonitorFormat'
import { isChannelMonitorQuotaVisible } from '@/utils/featureFlags'
import ProviderIcon from './ProviderIcon.vue'
import MonitorTimeline from './MonitorTimeline.vue'
import MonitorIntelligence from './MonitorIntelligence.vue'
import MonitorQuotaView from '@/components/common/MonitorQuotaView.vue'

const props = defineProps<{
  item: UserMonitorView
  window: '7d' | '15d' | '30d'
  availabilityValue: number | null
  countdownSeconds: number
}>()

const emit = defineEmits<{
  (e: 'click'): void
}>()

const { t } = useI18n()
const {
  statusLabel,
  statusBadgeClass,
  providerLabel,
  providerBadgeClass,
  formatMonitorModel,
} = useChannelMonitorFormat()

const quotaVisible = computed(
  () => isChannelMonitorQuotaVisible() && !!props.item.latest_quota
)

const hasAvailability = computed(() => !!props.item.primary_status && props.availabilityValue !== null && Number.isFinite(props.availabilityValue))
const availabilityText = computed(() => hasAvailability.value ? props.availabilityValue!.toFixed(1) : t('monitorCommon.latencyEmpty'))
const availabilityClass = computed(() => {
  if (!hasAvailability.value) return 'text-gray-400 dark:text-gray-500'
  if (props.availabilityValue! >= 95) return 'text-emerald-600 dark:text-emerald-300'
  if (props.availabilityValue! >= 80) return 'text-amber-600 dark:text-amber-300'
  return 'text-red-600 dark:text-red-300'
})

function latencyMetric(ms: number | null | undefined) {
  if (typeof ms !== 'number' || !Number.isFinite(ms) || ms < 0) return { value: t('monitorCommon.latencyEmpty'), unit: '' }
  return ms >= 1000
    ? { value: (ms / 1000).toFixed(1), unit: 's' }
    : { value: String(Math.round(ms)), unit: 'ms' }
}

const userRate = computed(() => {
  const rate = props.item.user_rate_multiplier
  return typeof rate === 'number' && Number.isFinite(rate) && rate >= 0
    ? rate.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 8, useGrouping: false })
    : null
})

const metrics = computed(() => {
  const usage = props.item.usage_metrics?.[props.window]
  const cache = usage?.cache_rate_pct
  const hasCache = typeof cache === 'number' && Number.isFinite(cache) && cache >= 0 && cache <= 100
  const color = 'text-gray-900 dark:text-gray-100'
  return [
    { key: 'cache-rate', label: t('monitorCommon.cacheRate'), value: hasCache ? cache.toFixed(1) : t('monitorCommon.latencyEmpty'), unit: hasCache ? '%' : '', color },
    { key: 'availability', label: t('monitorCommon.availabilityPrefix'), value: availabilityText.value, unit: hasAvailability.value ? '%' : '', color: availabilityClass.value },
    { key: 'first-token', label: t('monitorCommon.firstToken'), ...latencyMetric(usage?.first_token_ms), color },
  ]
})

const timelineLength = 18
</script>

<style scoped>
.monitor-card-metrics {
  container-type: inline-size;
}

.monitor-card-metrics dd {
  font-size: clamp(1rem, 5cqi, 1.125rem);
}
</style>
