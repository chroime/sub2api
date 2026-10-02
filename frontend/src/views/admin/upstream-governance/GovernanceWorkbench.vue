<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import api, { type OperationsSection, type WorkbenchItem, type WorkbenchPage } from '@/api/admin/upstream-operations'
import { formatGovernanceGroupName, formatGovernanceTime } from './format'
import { operationReasonKey, operationsSeverityKey, targetSection, workbenchKindKey, workbenchStatusKey } from './operations-feedback'

const props = withDefaults(defineProps<{ siteId?: number; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{ navigate: [value: { siteId: number; section: OperationsSection }] }>()
const { t } = useI18n()
const result = ref<WorkbenchPage | null>(null)
const requestedPage = ref(1)
const loading = ref(false)
const failed = ref(false)
let generation = 0
let timer: ReturnType<typeof setInterval> | undefined
const risk = { critical: 0, warning: 1, info: 2 }
const items = computed(() => [...(result.value?.items ?? [])].sort((a, b) => (risk[a.severity] ?? 3) - (risk[b.severity] ?? 3)))
const shownPage = computed(() => result.value?.page ?? requestedPage.value)
const pages = computed(() => Math.max(1, Math.ceil((result.value?.total ?? 0) / (result.value?.page_size || 20))))
const visible = () => document.visibilityState !== 'hidden'

async function load() {
  const request = ++generation
  loading.value = true
  try {
    let page = requestedPage.value
    for (let attempt = 0; attempt < 2; attempt++) {
      const value = await api.workbench({ ...(props.siteId ? { site_id: props.siteId } : {}), page, page_size: 20 })
      if (request !== generation) return
      if (!value || !Array.isArray(value.items) || !value.summary || value.page_size <= 0) throw new Error('Invalid workbench response')
      const lastPage = Math.max(1, Math.ceil(value.total / value.page_size))
      if (value.page < 1 || value.page > lastPage) { page = lastPage; continue }
      result.value = value
      requestedPage.value = value.page
      failed.value = false
      return
    }
    // If data changes again during recovery, retain the last successful page
    // as stale rather than displaying a false empty scope or looping forever.
    throw new Error('Workbench pagination changed during recovery')
  } catch {
    if (request === generation) failed.value = true
  } finally {
    if (request === generation) loading.value = false
  }
}
function refresh() { if (!loading.value && !props.disabled) void load() }
function automaticRefresh() { if (visible() && !loading.value && !props.disabled) void load() }
function changePage(page: number) {
  if (loading.value || props.disabled || page < 1 || page > pages.value) return
  requestedPage.value = page
  void load()
}
function resetScope() {
  generation++
  result.value = null
  failed.value = false
  requestedPage.value = 1
  loading.value = false
  // Reading never executes a business action; a navigation lock must not leave
  // the initial scope blank until the next polling interval.
  if (visible()) void load()
}
function navigate(item: WorkbenchItem) {
  const section = targetSection(item.target_tab)
  if (!props.disabled && section && Number.isSafeInteger(item.site_id) && item.site_id > 0) emit('navigate', { siteId: item.site_id, section })
}
function resource(item: WorkbenchItem): string {
  const name = item.resource_name || (item.resource_id ? t('governance.workbench.resourceFallback', { id: item.resource_id }) : t('governance.workbench.siteResource'))
  return item.kind === 'pricing' ? formatGovernanceGroupName(name) : name
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
  <section data-test="operations-workbench" class="min-w-0 space-y-4" :aria-busy="loading">
    <header class="flex flex-wrap items-start justify-between gap-3">
      <div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><h3 class="font-semibold">{{ t('governance.workbench.title') }}</h3><span class="rounded-full bg-gray-100 px-2 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t(siteId ? 'governance.workbench.scopeSite' : 'governance.workbench.scopeAll') }}</span></div><p class="mt-2 text-sm leading-relaxed text-gray-500 dark:text-gray-400">{{ t('governance.workbench.description') }}</p></div>
      <button data-test="workbench-refresh" class="btn btn-secondary shrink-0 text-sm" type="button" :disabled="loading || disabled" @click="refresh"><Icon name="refresh" size="sm" class="mr-2" aria-hidden="true" />{{ t('governance.workbench.refresh') }}</button>
    </header>
    <p v-if="failed" role="alert" class="rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm leading-relaxed text-amber-900 dark:border-amber-800 dark:bg-amber-900/10 dark:text-amber-200">{{ t(result ? 'governance.workbench.refreshFailed' : 'governance.workbench.loadFailed') }}<span v-if="result && requestedPage !== shownPage" class="mt-1 block">{{ t('governance.workbench.oldPage') }}</span></p>
    <p v-if="loading" role="status" class="text-sm text-gray-500 dark:text-gray-400">{{ t('governance.workbench.loading') }}</p>
    <div v-if="result" data-test="workbench-summary" class="flex flex-wrap items-center gap-2 text-xs">
      <strong class="mr-1 text-gray-700 dark:text-gray-200">{{ t('governance.workbench.count', { count: result.total }) }}</strong>
      <span v-for="severity in (['critical', 'warning', 'info'] as const)" :key="severity" class="rounded-full px-2.5 py-1" :class="severity === 'critical' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : severity === 'warning' ? 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(operationsSeverityKey(severity)) }} {{ result.summary[severity] }}</span>
      <time class="w-full pt-1 text-gray-500 dark:text-gray-400" :datetime="result.evaluated_at">{{ t('governance.workbench.evaluatedAt', { time: formatGovernanceTime(result.evaluated_at) }) }}</time>
    </div>
    <div v-if="result && !items.length && !failed" data-test="workbench-empty" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm leading-relaxed text-gray-600 dark:border-dark-600 dark:text-gray-300"><p>{{ t('governance.workbench.emptyAsOf', { time: formatGovernanceTime(result.evaluated_at) }) }}</p><p class="mt-2 text-xs text-gray-500 dark:text-gray-400">{{ t('governance.workbench.emptyCaveat') }}</p></div>
    <div class="space-y-3">
      <article v-for="item in items" :key="item.id" data-test="workbench-item" :data-id="item.id" class="min-w-0 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
        <div class="flex flex-wrap items-start gap-3"><div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><h4 class="break-words text-sm font-semibold">{{ item.site_name }}</h4><span class="rounded-full px-2 py-0.5 text-xs" :class="item.severity === 'critical' ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : item.severity === 'warning' ? 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(operationsSeverityKey(item.severity)) }}</span><span class="text-xs text-gray-500 dark:text-gray-400">{{ t(workbenchKindKey(item.kind)) }} · {{ t(workbenchStatusKey(item.status)) }}</span></div><p class="mt-1 break-all text-xs text-gray-500 dark:text-gray-400">{{ item.base_url }}</p><p v-if="item.kind === 'pricing' || item.resource_name !== item.site_name" class="mt-2 break-words text-sm font-medium">{{ resource(item) }}</p></div>
          <button v-if="targetSection(item.target_tab)" :data-test="`workbench-go-${item.id}`" type="button" class="btn btn-secondary w-full text-xs sm:w-auto" :disabled="disabled" @click="navigate(item)">{{ t(`governance.workbench.actions.${item.target_tab}`) }}<Icon name="arrowRight" size="sm" class="ml-2" aria-hidden="true" /></button>
        </div>
        <p class="mt-3 text-sm leading-relaxed text-gray-700 dark:text-gray-200">{{ t(operationReasonKey(item.reason)) }}</p>
        <p v-if="item.shared" class="mt-2 text-xs text-amber-800 dark:text-amber-300">{{ t('governance.workbench.sharedHint') }}</p>
        <dl class="mt-3 flex flex-wrap gap-x-6 gap-y-2 text-xs text-gray-500 dark:text-gray-400"><div><dt class="sr-only">{{ t('governance.workbench.impactLabel') }}</dt><dd>{{ t('governance.workbench.impactCount', { count: item.impact_count }) }}</dd></div><div><dt>{{ t('governance.workbench.observedAt') }}</dt><dd class="mt-1 tabular-nums">{{ formatGovernanceTime(item.observed_at) }}</dd></div><div v-if="item.next_attempt_at"><dt>{{ t('governance.workbench.nextAttempt') }}</dt><dd class="mt-1 tabular-nums">{{ formatGovernanceTime(item.next_attempt_at) }}</dd></div></dl>
      </article>
    </div>
    <nav v-if="result && result.total > result.page_size" class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-500 dark:text-gray-400" :aria-label="t('governance.workbench.title')"><span>{{ t('governance.workbench.pageSummary', { page: shownPage, pages, total: result.total }) }}</span><div class="flex gap-2"><button data-test="workbench-previous" class="btn btn-secondary text-xs" type="button" :disabled="loading || disabled || shownPage <= 1" @click="changePage(shownPage - 1)">{{ t('governance.workbench.previous') }}</button><button data-test="workbench-next" class="btn btn-secondary text-xs" type="button" :disabled="loading || disabled || shownPage >= pages" @click="changePage(shownPage + 1)">{{ t('governance.workbench.next') }}</button></div></nav>
    <footer class="space-y-1 text-xs leading-relaxed text-gray-500 dark:text-gray-400"><p>{{ t('governance.workbench.readOnly') }}</p><p>{{ t('governance.workbench.autoRefresh') }}</p></footer>
  </section>
</template>
