<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import api, { type OperationsPage, type TimelineFilter, type TimelineItem } from '@/api/admin/upstream-operations'
import { formatGovernanceGroupName, formatGovernanceRate, formatGovernanceTime } from './format'
import { operationReasonKey, timelineKindKey, timelineStatusKey } from './operations-feedback'

const props = withDefaults(defineProps<{ siteId: number; disabled?: boolean }>(), { disabled: false })
const { t } = useI18n()
const result = ref<OperationsPage<TimelineItem> | null>(null)
const kind = ref<TimelineFilter>('all')
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
function resource(item: TimelineItem): string {
  if (!item.resource_name && !item.resource_id) return t('governance.workbench.siteResource')
  const name = item.resource_name || t('governance.workbench.resourceFallback', { id: item.resource_id })
  return item.kind === 'pricing' || /^(group_|rate_|price_|models_)/.test(item.reason) ? formatGovernanceGroupName(name) : name
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
  <section data-test="operations-timeline" class="min-w-0 space-y-4" :aria-busy="loading">
    <header class="flex flex-wrap items-start justify-between gap-3"><div class="min-w-0 flex-1"><h3 class="font-semibold">{{ t('governance.timeline.title') }}</h3><p class="mt-2 text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ t('governance.timeline.description') }}</p></div><button data-test="timeline-refresh" class="btn btn-secondary shrink-0 text-sm" type="button" :disabled="loading || disabled" @click="refresh"><Icon name="refresh" size="sm" class="mr-2" aria-hidden="true" />{{ t('governance.timeline.refresh') }}</button></header>
    <div class="flex flex-wrap items-end justify-between gap-3"><div class="w-full sm:w-64"><label :for="`timeline-kind-${siteId}`" class="mb-1 block text-xs font-medium text-gray-600 dark:text-gray-300">{{ t('governance.timeline.filterLabel') }}</label><Select :id="`timeline-kind-${siteId}`" :model-value="kind" :options="options" :disabled="loading || disabled" :aria-label="t('governance.timeline.filterLabel')" @update:model-value="changeKind" /></div><time v-if="result" class="text-xs text-gray-500 dark:text-gray-400" :datetime="result.evaluated_at">{{ t('governance.workbench.evaluatedAt', { time: formatGovernanceTime(result.evaluated_at) }) }}</time></div>
    <p v-if="failed" role="alert" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm leading-relaxed text-amber-900 dark:border-amber-800 dark:bg-amber-900/10 dark:text-amber-200">{{ t(result ? 'governance.timeline.refreshFailed' : 'governance.timeline.loadFailed') }}<span v-if="result && requestedPage !== shownPage" class="mt-1 block">{{ t('governance.workbench.oldPage') }}</span></p>
    <p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('governance.timeline.loading') }}</p>
    <div v-if="result && !result.items.length && !failed" data-test="timeline-empty" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm leading-relaxed text-gray-600 dark:border-dark-600 dark:text-gray-300"><p>{{ t('governance.timeline.emptyAsOf', { time: formatGovernanceTime(result.evaluated_at) }) }}</p><p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('governance.timeline.emptyCaveat') }}</p></div>
    <ol class="space-y-3">
      <li v-for="item in result?.items || []" :key="item.id" data-test="timeline-record" class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
        <div class="flex flex-wrap items-center gap-2"><span class="text-xs font-semibold text-gray-700 dark:text-gray-200">{{ t(timelineKindKey(item.kind)) }}</span><span data-test="timeline-record-status" class="rounded-full px-2 py-0.5 text-xs" :class="['failed', 'conflict', 'protected', 'rejected'].includes(item.status) ? 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(timelineStatusKey(item.kind, item.status)) }}</span><time class="ml-auto text-xs tabular-nums text-gray-500 dark:text-gray-400" :datetime="item.created_at">{{ formatGovernanceTime(item.created_at) }}</time></div>
        <h4 class="mt-3 break-words text-sm font-semibold">{{ resource(item) }}</h4><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ item.site_name }} · {{ item.base_url }}</p>
        <p class="mt-3 text-sm leading-relaxed text-gray-700 dark:text-gray-200">{{ t(operationReasonKey(item.reason)) }}</p>
        <p v-if="item.shared" class="mt-2 text-xs leading-relaxed text-amber-800 dark:text-amber-300">{{ t('governance.timeline.sharedHint') }}</p>
        <p v-if="item.kind === 'notification' && item.status === 'sent'" class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t('governance.timeline.acceptedHint') }}</p>
        <p v-if="item.kind === 'event' && item.acknowledged !== null" class="mt-2 text-xs leading-relaxed text-gray-500 dark:text-gray-400">{{ t(item.acknowledged ? 'governance.timeline.read' : 'governance.timeline.unread') }} · {{ t('governance.workbench.readDoesNotResolve') }}</p>
        <dl v-if="item.before_rate !== null || item.after_rate !== null || item.before_cost !== null || item.after_cost !== null" class="mt-3 grid grid-cols-2 gap-3 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900"><div v-if="item.before_rate !== null"><dt class="text-gray-500 dark:text-gray-400">{{ t(item.kind === 'event' && item.reason === 'rate_changed' ? 'governance.timeline.upstreamBeforeRate' : 'governance.timeline.beforeRate') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatGovernanceRate(item.before_rate) }}</dd></div><div v-if="item.after_rate !== null"><dt class="text-gray-500 dark:text-gray-400">{{ t(item.kind === 'event' && item.reason === 'rate_changed' ? 'governance.timeline.upstreamAfterRate' : 'governance.timeline.afterRate') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatGovernanceRate(item.after_rate) }}</dd></div><div v-if="item.before_cost !== null"><dt class="text-gray-500 dark:text-gray-400">{{ t('governance.timeline.beforeCost') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatGovernanceRate(item.before_cost) }}</dd></div><div v-if="item.after_cost !== null"><dt class="text-gray-500 dark:text-gray-400">{{ t('governance.timeline.afterCost') }}</dt><dd class="mt-1 font-medium tabular-nums">{{ formatGovernanceRate(item.after_cost) }}</dd></div></dl>
        <div v-if="item.kind === 'notification'" class="mt-3 flex flex-wrap gap-3 text-xs text-gray-500 dark:text-gray-400"><p v-if="item.attempts !== null">{{ t('governance.timeline.attempts', { count: item.attempts }) }}</p><p v-if="item.next_attempt_at">{{ t('governance.workbench.nextAttempt') }}：{{ formatGovernanceTime(item.next_attempt_at) }}</p><p v-if="item.sent_at">{{ t('governance.timeline.sentAt') }}：{{ formatGovernanceTime(item.sent_at) }}</p></div>
        <details class="mt-3 text-xs text-gray-500 dark:text-gray-400"><summary class="cursor-pointer">{{ t('governance.timeline.technicalDetails') }}</summary><p class="mt-2 break-all">{{ t('governance.timeline.recordId') }}：{{ item.record_id }}</p><p class="mt-1 break-all">{{ item.kind }} · {{ item.status }} · {{ item.reason }}</p><p v-if="item.related_record_id" data-test="timeline-related" class="mt-1 break-all">{{ t('governance.timeline.relatedRecord') }}：{{ item.related_record_id }}</p></details>
      </li>
    </ol>
    <nav v-if="result && result.total > result.page_size" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400" :aria-label="t('governance.timeline.title')"><span>{{ t('governance.workbench.pageSummary', { page: shownPage, pages, total: result.total }) }}</span><div class="flex gap-2"><button data-test="timeline-previous" class="btn btn-secondary text-xs" type="button" :disabled="loading || disabled || shownPage <= 1" @click="changePage(shownPage - 1)">{{ t('governance.workbench.previous') }}</button><button data-test="timeline-next" class="btn btn-secondary text-xs" type="button" :disabled="loading || disabled || shownPage >= pages" @click="changePage(shownPage + 1)">{{ t('governance.workbench.next') }}</button></div></nav>
    <footer class="space-y-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400"><p>{{ t('governance.timeline.independent') }}</p><p>{{ t('governance.workbench.autoRefresh') }}</p></footer>
  </section>
</template>
