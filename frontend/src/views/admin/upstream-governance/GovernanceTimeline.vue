<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import api, { type OperationsPage, type TimelineFilter, type TimelineItem } from '@/api/admin/upstream-operations'
import { formatGovernanceGroupName, formatGovernanceRate, formatGovernanceTime } from './format'
import { operationReasonKey, timelineStatusKey, timelineTitleKey } from './operations-feedback'

// remoteGroups names the groups that event records only carry by id.
const props = withDefaults(defineProps<{ siteId: number; disabled?: boolean; remoteGroups?: { id: string; name: string }[] }>(), { disabled: false, remoteGroups: () => [] })
const { t } = useI18n()
const result = ref<OperationsPage<TimelineItem> | null>(null)
// Collection and operations events are what administrators read first.
const kind = ref<TimelineFilter>('event')
const requestedPage = ref(1)
const loading = ref(false)
const failed = ref(false)
let generation = 0
let timer: ReturnType<typeof setInterval> | undefined
const shownPage = computed(() => result.value?.page ?? requestedPage.value)
const pages = computed(() => Math.max(1, Math.ceil((result.value?.total ?? 0) / (result.value?.page_size || 20))))
const kinds: TimelineFilter[] = ['all', 'event', 'pricing', 'notification']
const options = computed(() => kinds.map(value => ({ value, label: t(`governance.timeline.kinds.${value}`) })))
const visible = () => document.visibilityState !== 'hidden'

async function load() {
  const request = ++generation
  loading.value = true
  try {
    let page = requestedPage.value
    for (let attempt = 0; attempt < 2; attempt++) {
      const value = await api.timeline(props.siteId, { kind: kind.value, page, page_size: 20 })
      if (request !== generation) return
      if (!value || !Array.isArray(value.items) || value.page_size <= 0) throw new Error('Invalid timeline response')
      const lastPage = Math.max(1, Math.ceil(value.total / value.page_size))
      if (value.page < 1 || value.page > lastPage) { page = lastPage; continue }
      result.value = value
      requestedPage.value = value.page
      failed.value = false
      return
    }
    throw new Error('Timeline pagination changed during recovery')
  } catch {
    if (request === generation) failed.value = true
  } finally {
    if (request === generation) loading.value = false
  }
}
function refresh() { if (!loading.value && !props.disabled) void load() }
function automaticRefresh() { if (visible() && !loading.value && !props.disabled) void load() }
function resetScope() {
  generation++
  result.value = null
  failed.value = false
  requestedPage.value = 1
  loading.value = false
  if (visible()) void load()
}
function changeKind(value: unknown) {
  if (props.disabled || loading.value || !kinds.includes(value as TimelineFilter)) return
  kind.value = value as TimelineFilter
  resetScope()
}
function changePage(page: number) {
  if (loading.value || props.disabled || page < 1 || page > pages.value) return
  requestedPage.value = page
  void load()
}
const remoteNames = computed(() => new Map(props.remoteGroups.map(group => [group.id, group.name])))
function resource(item: TimelineItem): string {
  if (!item.resource_name && !item.resource_id) return t('governance.workbench.siteResource')
  const name = item.resource_name || (item.kind === 'event' ? remoteNames.value.get(item.resource_id) : '') || t('governance.workbench.resourceFallback', { id: item.resource_id })
  return item.kind === 'pricing' || /^(group_|rate_|price_|models_)/.test(item.reason) ? formatGovernanceGroupName(name) : name
}
// The second line of a row: the measured change when there is one, else the reason.
function changes(item: TimelineItem): string[] {
  const list: string[] = []
  if (item.before_rate !== null || item.after_rate !== null) list.push(t(item.kind === 'pricing' ? 'governance.timeline.saleRateChange' : 'governance.timeline.rateChange', { before: formatGovernanceRate(item.before_rate), after: formatGovernanceRate(item.after_rate) }))
  if (item.before_cost !== null || item.after_cost !== null) list.push(t('governance.timeline.costChange', { before: formatGovernanceRate(item.before_cost), after: formatGovernanceRate(item.after_cost) }))
  return list
}
// Events without a measured change are fully named by their heading.
function detail(item: TimelineItem): string {
  const reason = t(operationReasonKey(item.reason))
  return reason === t(timelineTitleKey(item)) ? '' : reason
}
function hasDetail(item: TimelineItem): boolean {
  return changes(item).length > 0 || !!detail(item) || item.shared || item.next_attempt_at !== null || (item.kind === 'notification' && item.attempts !== null)
}
watch(() => props.siteId, resetScope)
watch(() => props.disabled, value => { if (!value && !result.value && !loading.value && visible()) void load() })
onMounted(() => {
  if (visible()) void load()
  timer = setInterval(automaticRefresh, 60_000)
  document.addEventListener('visibilitychange', automaticRefresh)
})
onUnmounted(() => {
  generation++
  if (timer) clearInterval(timer)
  document.removeEventListener('visibilitychange', automaticRefresh)
})
</script>
<template>
  <section data-test="operations-timeline" class="min-w-0 space-y-3" :aria-busy="loading">
    <header class="flex flex-wrap items-end justify-between gap-3"><div class="w-full sm:w-64"><label :for="`timeline-kind-${siteId}`" class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('governance.timeline.filterLabel') }}</label><Select :id="`timeline-kind-${siteId}`" :model-value="kind" :options="options" :disabled="loading || disabled" :aria-label="t('governance.timeline.filterLabel')" @update:model-value="changeKind" /></div><div class="flex flex-wrap items-center gap-3"><time v-if="result" class="text-xs text-gray-500 dark:text-gray-400" :datetime="result.evaluated_at">{{ t('governance.workbench.evaluatedAt', { time: formatGovernanceTime(result.evaluated_at) }) }}</time><button data-test="timeline-refresh" class="btn btn-secondary shrink-0 text-sm" type="button" :disabled="loading || disabled" @click="refresh"><Icon name="refresh" size="sm" class="mr-2" aria-hidden="true" />{{ t('governance.timeline.refresh') }}</button></div></header>
    <p v-if="failed" role="alert" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm leading-relaxed text-amber-900 dark:border-amber-800 dark:bg-amber-900/10 dark:text-amber-200">{{ t(result ? 'governance.timeline.refreshFailed' : 'governance.timeline.loadFailed') }}<span v-if="result && requestedPage !== shownPage" class="mt-1 block">{{ t('governance.workbench.oldPage') }}</span></p>
    <p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('governance.timeline.loading') }}</p>
    <div v-if="result && !result.items.length && !failed" data-test="timeline-empty" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm leading-relaxed text-gray-600 dark:border-dark-600 dark:text-gray-300"><p>{{ t('governance.timeline.emptyAsOf', { time: formatGovernanceTime(result.evaluated_at) }) }}</p><p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('governance.timeline.emptyCaveat') }}</p></div>
    <ol class="divide-y divide-gray-100 overflow-hidden rounded-xl border border-gray-200 bg-white dark:divide-dark-700 dark:border-dark-600 dark:bg-dark-800" :class="result?.items.length ? '' : 'hidden'">
      <li v-for="item in result?.items || []" :key="item.id" data-test="timeline-record" class="min-w-0 px-4 py-2.5">
        <div class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-sm">
          <time class="shrink-0 text-xs tabular-nums text-gray-500 dark:text-gray-400" :datetime="item.created_at">{{ formatGovernanceTime(item.created_at) }}</time>
          <span class="font-medium" :class="item.severity === 'critical' ? 'text-red-600 dark:text-red-400' : item.severity === 'warning' ? 'text-amber-700 dark:text-amber-300' : 'text-gray-800 dark:text-gray-100'">{{ t(timelineTitleKey(item)) }}</span>
          <span class="min-w-0 truncate text-gray-600 dark:text-gray-300">{{ resource(item) }}</span>
          <span v-if="item.kind !== 'event'" data-test="timeline-record-status" class="ml-auto rounded-full px-2 py-0.5 text-xs" :class="['failed', 'conflict', 'protected', 'rejected'].includes(item.status) ? 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(timelineStatusKey(item.kind, item.status)) }}</span>
        </div>
        <p v-if="hasDetail(item)" data-test="timeline-change" class="mt-0.5 break-words text-xs leading-relaxed text-gray-600 dark:text-gray-300">
          <template v-if="changes(item).length"><span v-for="(change, index) in changes(item)" :key="index" class="tabular-nums"><template v-if="index"> · </template>{{ change }}</span></template>
          <template v-else>{{ detail(item) }}</template>
          <template v-if="item.kind === 'notification' && item.attempts !== null"> · {{ t('governance.timeline.attempts', { count: item.attempts }) }}</template>
          <template v-if="item.next_attempt_at"> · {{ t('governance.workbench.nextAttempt') }}：{{ formatGovernanceTime(item.next_attempt_at) }}</template>
          <span v-if="item.shared" class="text-amber-700 dark:text-amber-300"> · {{ t('governance.timeline.sharedHint') }}</span>
        </p>
      </li>
    </ol>
    <nav v-if="result && result.total > result.page_size" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400" :aria-label="t('governance.timeline.title')"><span>{{ t('governance.workbench.pageSummary', { page: shownPage, pages, total: result.total }) }}</span><div class="flex gap-2"><button data-test="timeline-previous" class="btn btn-secondary text-xs" type="button" :disabled="loading || disabled || shownPage <= 1" @click="changePage(shownPage - 1)">{{ t('governance.workbench.previous') }}</button><button data-test="timeline-next" class="btn btn-secondary text-xs" type="button" :disabled="loading || disabled || shownPage >= pages" @click="changePage(shownPage + 1)">{{ t('governance.workbench.next') }}</button></div></nav>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('governance.workbench.autoRefresh') }}</p>
  </section>
</template>
