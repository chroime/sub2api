<template>
  <div :class="monitoring ? 'key-group-monitor-grid groupOptionItemCompact' : 'flex min-w-0 flex-1 items-start justify-between gap-3'">
    <!-- Left: name + description -->
    <div
      class="flex min-w-0 flex-1 flex-col items-start"
      :title="description || undefined"
    >
      <!-- Row 1: platform badge (name bold) -->
      <GroupBadge
        :name="name"
        :identity="identity"
        :platform="platform"
        :subscription-type="subscriptionType"
        :show-rate="false"
        class="groupOptionItemBadge"
      />
      <!-- Row 2: description with top spacing -->
      <span
        v-if="description || monitoring"
        class="w-full text-left text-gray-500 dark:text-gray-400"
        :class="monitoring ? 'mt-1 h-4 truncate whitespace-nowrap text-[11px] leading-4' : 'mt-1.5 whitespace-pre-line [overflow-wrap:anywhere] text-xs leading-relaxed line-clamp-3'"
      >
        {{ description }}
      </span>
    </div>

    <template v-if="monitoring">
      <MiniMonitorBars :column="monitoring.availability" kind="availability" :group-name="name" />
      <MiniMonitorBars :column="monitoring.intelligence" kind="intelligence" :group-name="name" />
    </template>
    <!-- Right: rate pill + checkmark (vertically centered to first row) -->
    <div class="flex shrink-0 items-center gap-2" :class="monitoring ? 'justify-end gap-1 sm:gap-2' : 'pt-0.5'">
      <div class="flex shrink-0 flex-col items-end gap-1">
        <span v-if="monitoring && rateMultiplier !== undefined" class="text-[10px] font-semibold tabular-nums text-gray-600 sm:text-xs dark:text-gray-300"
          :title="`${rateMultiplier}x${hasCustomRate ? ` → ${userRateMultiplier}x` : ''}${hasPeakRate ? ` · ${peakRateTitle}` : ''}`">
          {{ hasCustomRate ? userRateMultiplier : rateMultiplier }}x
        </span>
        <!-- Rate pill (platform color) -->
        <span v-else-if="rateMultiplier !== undefined" :class="['inline-flex items-center whitespace-nowrap rounded-full px-3 py-1 text-xs font-semibold', ratePillClass]">
          <template v-if="hasCustomRate">
            <span class="mr-1 line-through opacity-50">{{ rateMultiplier }}x</span>
            <span class="font-bold">{{ userRateMultiplier }}x</span>
          </template>
          <template v-else>
            {{ rateMultiplier }}x {{ t('admin.groups.rateLabel') }}
          </template>
        </span>
        <span
          v-if="hasPeakRate && !monitoring"
          class="inline-flex items-center whitespace-nowrap rounded-full bg-amber-50 px-3 py-1 text-xs font-semibold text-amber-700 dark:bg-amber-900/20 dark:text-amber-300"
          :title="peakRateTitle"
        >
          {{ peakRateText }}
        </span>
      </div>
      <!-- Checkmark -->
      <span v-if="showCheckmark" :class="monitoring ? 'h-3 w-3 shrink-0 sm:h-4 sm:w-4' : 'contents'">
        <svg
          v-if="selected"
          class="shrink-0 text-primary-600 dark:text-primary-400"
          :class="monitoring ? 'h-full w-full' : 'h-4 w-4'"
          fill="none"
          stroke="currentColor"
          viewBox="0 0 24 24"
          stroke-width="2"
        >
          <path stroke-linecap="round" stroke-linejoin="round" d="M5 13l4 4L19 7" />
        </svg>
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from './GroupBadge.vue'
import type { SubscriptionType, GroupPlatform, GroupIdentity } from '@/types'
import { useAppStore } from '@/stores/app'
import { formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import MiniMonitorBars from '@/components/keys/MiniMonitorBars.vue'
import type { GroupMonitorState } from '@/utils/groupMonitor'
import '@/components/keys/group-monitor.css'

const { t } = useI18n()

interface Props {
  name: string
  identity?: GroupIdentity | null
  platform: GroupPlatform
  subscriptionType?: SubscriptionType
  rateMultiplier?: number
  userRateMultiplier?: number | null
  peakRateEnabled?: boolean
  peakStart?: string
  peakEnd?: string
  peakRateMultiplier?: number
  description?: string | null
  selected?: boolean
  showCheckmark?: boolean
  monitoring?: GroupMonitorState
}

const props = withDefaults(defineProps<Props>(), {
  subscriptionType: 'standard',
  selected: false,
  showCheckmark: true,
  userRateMultiplier: null,
  peakRateEnabled: false
})

// Whether user has a custom rate different from default
const hasCustomRate = computed(() => {
  return (
    props.userRateMultiplier !== null &&
    props.userRateMultiplier !== undefined &&
    props.rateMultiplier !== undefined &&
    props.userRateMultiplier !== props.rateMultiplier
  )
})

const appStore = useAppStore()

const hasPeakRate = computed(() => {
  return Boolean(props.peakRateEnabled && props.peakStart && props.peakEnd)
})

const peakRateText = computed(() => {
  return formatPeakRateWindow(
    {
      peak_rate_enabled: props.peakRateEnabled,
      peak_start: props.peakStart,
      peak_end: props.peakEnd,
      peak_rate_multiplier: props.peakRateMultiplier
    },
    serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset)
  )
})

const peakRateTitle = computed(() => {
  return t('common.peakRateTooltip', { window: peakRateText.value })
})

// Rate pill color matches platform badge color
const ratePillClass = computed(() => {
  switch (props.platform) {
    case 'anthropic':
      return 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-400'
    case 'openai':
      return 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400'
    case 'gemini':
      return 'bg-sky-50 text-sky-700 dark:bg-sky-900/20 dark:text-sky-400'
    default: // antigravity and others
      return 'bg-violet-50 text-violet-700 dark:bg-violet-900/20 dark:text-violet-400'
  }
})
</script>

<style scoped>
/* Bold the group name inside GroupBadge when used in dropdown option */
.groupOptionItemBadge :deep(span.truncate) {
  font-weight: 600;
}
.groupOptionItemCompact .groupOptionItemBadge { max-width: 100%; gap: 4px; padding: 0; background: transparent; }
@media (max-width: 639px) {
  .groupOptionItemCompact .groupOptionItemBadge { font-size: 11px; }
  .groupOptionItemCompact :deep([data-group-identity]) { padding: 1px 3px; }
  .groupOptionItemCompact :deep([data-group-identity] > span) { display: none; }
}
</style>
