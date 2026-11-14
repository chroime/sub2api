<template>
  <button
    ref="trigger" type="button" class="mini-monitor-trigger flex h-8 w-full min-w-0 items-center rounded px-0.5 hover:bg-black/[0.025] focus-visible:outline focus-visible:outline-2 focus-visible:outline-primary-500 dark:hover:bg-white/5"
    :aria-label="`${groupName} · ${heading} · ${stateLabel}`"
    :aria-expanded="open" :aria-describedby="open ? tooltipId : undefined"
    :data-monitor-kind="kind" :data-monitor-state="column.state"
    @pointerenter="onEnter" @pointerleave="onLeave" @focus="show" @blur="onBlur"
    @click.stop.prevent="toggle" @keydown.enter.stop.prevent="toggle" @keydown.space.stop.prevent="toggle"
    @keydown.esc.stop.prevent="close"
  >
    <span class="mini-monitor-bars grid h-[18px] w-full min-w-0 items-end gap-[2px]" aria-hidden="true">
      <span v-for="(bar, index) in column.bars" :key="index" data-monitor-bar :data-status="bar.status"
        class="min-w-0 rounded-[1px]" :class="bar.colorClass" :style="{ height: `${bar.heightPct}%` }" />
    </span>
  </button>
  <Teleport to="body">
    <div v-if="open" :id="tooltipId" ref="tooltip" role="tooltip" data-group-monitor-detail
      class="fixed z-[100000030] max-h-[min(320px,calc(100dvh-16px))] overflow-y-auto rounded-xl border border-gray-200 bg-white p-3 text-xs text-gray-700 shadow-xl dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200"
      :style="tooltipStyle" @click.stop @keydown.esc.stop.prevent="close">
      <div class="mb-2 flex items-center justify-between gap-3">
        <span class="min-w-0 truncate font-semibold" :title="groupName">{{ groupName }} · {{ heading }}</span>
        <button type="button" class="shrink-0 rounded p-1 text-gray-400 hover:bg-gray-100 dark:hover:bg-dark-700" :aria-label="t('common.close')" @click.stop="close">
          <Icon name="x" size="xs" />
        </button>
      </div>
      <p v-if="column.state !== 'ready'" class="mb-2 text-gray-500 dark:text-gray-400">{{ stateLabel }}</p>
      <div v-for="(detail, index) in column.details" :key="`${detail.model}:${detail.effort}:${index}`" class="space-y-1 border-t border-gray-100 py-2 first:border-0 dark:border-dark-700">
        <div class="flex items-baseline justify-between gap-2">
          <span class="min-w-0 truncate font-mono text-[11px]" :title="detail.model">{{ detail.model }}</span>
          <span class="shrink-0 text-[10px]">{{ detail.state === 'ready' ? verdict(detail.status) : t(`keys.groupMonitoring.states.${detail.state}`) }}</span>
        </div>
        <p v-if="kind === 'intelligence'" class="text-[10px] text-gray-500 dark:text-gray-400">{{ t('monitorCommon.intelligence.thinkingEffort') }} · {{ detail.effort || t('monitorCommon.intelligence.defaultEffort') }}</p>
        <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[10px] text-gray-500 dark:text-gray-400">
          <time v-if="detail.checkedAt" :datetime="detail.checkedAt">{{ formatDateTime(detail.checkedAt) }}</time>
          <span v-if="validLatency(detail.latencyMs)">{{ detail.latencyMs }} ms</span>
        </div>
      </div>
      <p class="mt-1 border-t border-gray-100 pt-2 text-[10px] text-gray-500 dark:border-dark-700 dark:text-gray-400">
        {{ t('keys.groupMonitoring.checkWindow', { count: visibleCount }) }}
      </p>
      <p v-if="column.details.length > 1" class="mt-1 text-[10px] text-gray-500 dark:text-gray-400">{{ t('keys.groupMonitoring.aggregate') }}</p>
    </div>
  </Teleport>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import { useMediaQuery } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { formatDateTime } from '@/utils/format'
import type { GroupMonitorColumn } from '@/utils/groupMonitor'

const props = defineProps<{ column: GroupMonitorColumn; kind: 'availability' | 'intelligence'; groupName: string }>()
const { t } = useI18n()
const trigger = ref<HTMLElement | null>(null)
const tooltip = ref<HTMLElement | null>(null)
const open = ref(false)
const pinned = ref(false)
const tooltipId = `group-monitor-${useId()}`
const mobile = useMediaQuery('(max-width: 639px)')
const visibleCount = computed(() => mobile.value ? 12 : 20)
const heading = computed(() => t(`keys.groupMonitoring.${props.kind}`))
const verdict = (status: string) => props.kind === 'intelligence'
  ? t(`monitorCommon.intelligence.legendLabels.${status}`)
  : t(`monitorCommon.status.${status === 'empty' ? 'unknown' : status}`)
const stateLabel = computed(() => props.column.state === 'ready'
  ? verdict(props.column.bars.at(-1)?.status || (props.kind === 'intelligence' ? 'unknown' : 'empty'))
  : t(`keys.groupMonitoring.states.${props.column.state}`))
const tooltipStyle = ref<Record<string, string>>({ top: '0px', left: '0px', width: '288px' })
const validLatency = (value?: number | null) => typeof value === 'number' && Number.isFinite(value) && value >= 0

function position() {
  if (!trigger.value || !tooltip.value) return
  const rect = trigger.value.getBoundingClientRect()
  const width = Math.max(0, Math.min(288, window.innerWidth - 16))
  const height = tooltip.value.getBoundingClientRect().height
  tooltipStyle.value = {
    width: `${width}px`, left: `${Math.max(8, Math.min(window.innerWidth - width - 8, rect.left + rect.width / 2 - width / 2))}px`,
    top: `${Math.max(8, rect.top >= height + 16 ? rect.top - height - 8 : Math.min(rect.bottom + 8, window.innerHeight - height - 8))}px`,
  }
}
function show() { open.value = true; void nextTick(position) }
function close() { open.value = false; pinned.value = false }
function toggle() { if (pinned.value) close(); else { pinned.value = true; show() } }
function onEnter(event: PointerEvent) { if (event.pointerType !== 'touch') show() }
function onLeave(event: PointerEvent) { if (!pinned.value && !(event.relatedTarget instanceof Node && tooltip.value?.contains(event.relatedTarget))) close() }
function onBlur() { if (!pinned.value) close() }
function outside(event: Event) {
  const target = event.target
  if (target instanceof Node && !trigger.value?.contains(target) && !tooltip.value?.contains(target)) close()
}
watch(open, (shown, _previous, cleanup) => {
  if (!shown) return
  document.addEventListener('pointerdown', outside, true)
  window.addEventListener('scroll', close, true)
  window.addEventListener('resize', close)
  cleanup(() => {
    document.removeEventListener('pointerdown', outside, true)
    window.removeEventListener('scroll', close, true)
    window.removeEventListener('resize', close)
  })
})
watch(() => props.column, close)
</script>

<style scoped>
.mini-monitor-bars { grid-template-columns: repeat(20, minmax(0, 1fr)); }
@media (max-width: 639px) {
  .mini-monitor-bars { grid-template-columns: repeat(12, minmax(0, 1fr)); gap: 1px; height: 16px; }
  .mini-monitor-bars > :nth-child(-n+8) { display: none; }
}
</style>
