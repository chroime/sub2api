<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Icon from '@/components/icons/Icon.vue'
import { getModelsByPlatform } from '@/composables/useModelWhitelist'
import type { ManagedKey, RemoteGroup, Transport } from '@/api/admin/upstream-governance'
import api, { type ModelAPIMode, type ModelBatchInput, type ModelEffort, type ModelPolicy, type ModelRun, type ModelRunPage, type ModelStats, type ModelTestConfig, type ModelTestTemplate } from '@/api/admin/upstream-model-monitoring'
import ModelRunDetail from './ModelRunDetail.vue'
import ModelComparison from './ModelComparison.vue'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'
import { intervalValidationKey } from './interval'

const props = defineProps<{ siteId: number; remoteGroups: RemoteGroup[]; managedKeys: ManagedKey[]; collectedAt?: string; disabled?: boolean }>()
const emit = defineEmits<{ busy: [value: boolean]; manageKeys: [] }>()
const { t } = useI18n()
const mt = (key: string) => t(`governance.modelMonitoring.${key}`)
const failure = (value: unknown) => {
  const reason = (value as { reason?: string })?.reason
  return reason === 'model_budget_exhausted' ? mt('budgetExhausted') : reason === 'model_group_gone' ? mt('groupGone') : t(errorKey(value))
}
const policies = ref<ModelPolicy[]>([]), runs = ref<ModelRunPage | null>(null)
const loading = ref(false), busy = ref(false), error = ref(''), notice = ref(''), editorOpen = ref(false)
const page = ref(1), batchId = ref(''), detailId = ref(''), deleteId = ref<number | null>(null)
const comparison = ref<{ batchId: string; template: ModelTestTemplate; sample: number } | null>(null)
const stats = ref<ModelStats | null>(null), statsError = ref(''), statsDays = ref(7), statsExpanded = ref(false)
let statsGeneration = 0
const editingPolicy = ref<ModelPolicy | null>(null)
const policyName = ref(''), interval = ref(1440), dailyLimit = ref(100), notifyEnabled = ref(false), recipients = ref(''), failureThreshold = ref(3), takeOverLegacy = ref(false)
const modes: ModelAPIMode[] = ['chat_completions', 'responses', 'anthropic', 'gemini']
const efforts: ModelEffort[] = ['low', 'medium', 'high']
const templates: ModelTestTemplate[] = ['candy', 'pelican', 'token_audit', 'context', 'probe']
const statuses = new Set(['queued', 'running', 'succeeded', 'failed', 'cancelled', 'indeterminate', 'skipped'])
const initialConfig = (): ModelTestConfig => ({ managed_key_id: 0, platform: 'openai', model: '', api_mode: 'chat_completions', efforts: ['medium'], templates: ['candy', 'token_audit'], samples: 1, concurrency: 1, max_output_tokens: 4096, timeout_seconds: 180, first_content_timeout_seconds: 60, idle_timeout_seconds: 30, input_tokens: 1024, tokenizer: 'auto', token_tolerance_percent: 10 })
const config = ref<ModelTestConfig>(initialConfig())
let generation = 0, refreshGeneration = 0, timer: ReturnType<typeof setTimeout> | undefined
let pendingStart: { fingerprint: string; requestId: string } | null = null
const locked = computed(() => props.disabled || busy.value)
const readyKeys = computed(() => props.managedKeys.filter(key => key.site_id === props.siteId && key.has_key))
const groupNames = computed(() => new Map(props.remoteGroups.map(group => [group.id, group.name])))
const keyOptions = computed(() => readyKeys.value.map(key => ({ value: key.id, platform: key.platform, label: `${groupNames.value.get(key.remote_group_id) || mt('unnamedGroup')} · ${key.platform}`, group: key.remote_group_id })))
const selectedKey = computed(() => readyKeys.value.find(key => key.id === config.value.managed_key_id))
const selectedGroup = computed(() => props.remoteGroups.find(group => group.id === selectedKey.value?.remote_group_id))
const usingCandidateModels = computed(() => !!selectedGroup.value && !selectedGroup.value.models?.length)
const modelOptions = computed(() => {
  const models = usingCandidateModels.value
    ? getModelsByPlatform(selectedKey.value!.platform).filter(model => !/(?:image|video|audio|realtime|imagine|cogview)/i.test(model))
    : selectedGroup.value?.models || []
  return [...new Set([...models, ...(config.value.model ? [config.value.model] : [])])].map(value => ({ value, label: value }))
})
function modeAvailable(mode: ModelAPIMode) {
  const platform = selectedKey.value?.platform
  if (!platform || platform === 'antigravity') return true
  if (mode === 'anthropic' || mode === 'gemini') return platform === mode
  return platform !== 'anthropic' && platform !== 'gemini'
}
const modeOptions = computed(() => modes.map(value => ({ value, label: mt(`mode_${value}`), platform: value === 'anthropic' ? 'anthropic' : value === 'gemini' ? 'gemini' : 'openai', disabled: !modeAvailable(value) })))
const tokenizerOptions = computed(() => ['auto', 'o200k_base', 'cl100k_base', 'none'].map(value => ({ value, label: value === 'auto' || value === 'none' ? mt(`tokenizer_${value}`) : value })))
const requestCount = computed(() => config.value.efforts.length * config.value.templates.length * (Number.isInteger(config.value.samples) ? config.value.samples : 0))
const counts = computed(() => runs.value?.counts || Object.fromEntries([...statuses].map(status => [status, runs.value?.items.filter(run => run.status === status).length || 0])))
const runningCount = computed(() => (counts.value.queued || 0) + (counts.value.running || 0))
const completedCount = computed(() => Object.entries(counts.value).reduce((sum, [status, count]) => sum + (status === 'queued' || status === 'running' ? 0 : count), 0))
const pages = computed(() => Math.max(1, Math.ceil((runs.value?.total || 0) / (runs.value?.page_size || 20))))
const progress = computed(() => runs.value?.total ? Math.min(100, Math.round(100 * completedCount.value / runs.value.total)) : 0)
const activePolicyCount = computed(() => policies.value.filter(policy => policy.enabled).length)
const stateLabel = (state: string) => statuses.has(state) ? mt(`state_${state}`) : state
const countValue = (value: number | null | undefined) => value == null ? mt('unknown') : String(value)
const keyLabel = (id: number) => {
  const key = props.managedKeys.find(value => value.id === id)
  return key ? groupNames.value.get(key.remote_group_id) || `${mt('groupKey')} #${id}` : `${mt('groupKey')} #${id}`
}
const statsGroups = computed(() => statsExpanded.value ? stats.value?.groups || [] : (stats.value?.groups || []).slice(0, 6))
const statsWindowOptions = computed(() => [1, 7, 30].map(value => ({ value, label: mt(`window_${value}`) })))
async function refreshStats() {
  const request = generation, statsRequest = ++statsGeneration, id = props.siteId, days = statsDays.value
  try { const value = await api.stats(id, days); if (request === generation && statsRequest === statsGeneration) { stats.value = value; statsError.value = '' } }
  catch { if (request === generation && statsRequest === statsGeneration) statsError.value = mt('statsUnavailable') }
}
function changeStatsWindow(value: unknown) { if (typeof value === 'number' && [1, 7, 30].includes(value)) { statsDays.value = value; stats.value = null; void refreshStats() } }
function clearTimer() { if (timer !== undefined) { clearTimeout(timer); timer = undefined } }
function schedulePoll() {
  clearTimer()
  if (runningCount.value || policies.value.some(policy => policy.enabled)) timer = setTimeout(() => { void refresh(true) }, 4000)
}
async function refresh(silent = false) {
  if (busy.value) { schedulePoll(); return }
  clearTimer()
  const request = generation, refreshId = ++refreshGeneration, id = props.siteId
  if (!silent) { loading.value = true; error.value = '' }
  const results = await Promise.allSettled([api.policies(id), api.runs(id, page.value, batchId.value)])
  if (request !== generation || refreshId !== refreshGeneration) return
  if (results[0].status === 'fulfilled') policies.value = results[0].value || []
  else error.value = failure(results[0].reason)
  if (results[1].status === 'fulfilled') runs.value = results[1].value
  else error.value = failure(results[1].reason)
  loading.value = false
  void refreshStats()
  schedulePoll()
}
function changeKey(value: unknown) {
  if (locked.value) return
  const key = readyKeys.value.find(item => item.id === value)
  if (!key) return
  config.value.managed_key_id = key.id; config.value.platform = key.platform
  config.value.api_mode = key.platform === 'anthropic' ? 'anthropic' : key.platform === 'gemini' ? 'gemini' : 'chat_completions'
  const models = props.remoteGroups.find(group => group.id === key.remote_group_id)?.models || []
  config.value.model = models[0] || ''
}
function changeModel(value: unknown) { if (!locked.value && typeof value === 'string') config.value.model = value }
function changeMode(value: unknown) { if (!locked.value && modes.includes(value as ModelAPIMode) && modeAvailable(value as ModelAPIMode)) config.value.api_mode = value as ModelAPIMode }
function changeTokenizer(value: unknown) { if (!locked.value && ['auto', 'none', 'o200k_base', 'cl100k_base'].includes(String(value))) config.value.tokenizer = value as ModelTestConfig['tokenizer'] }
function newConfiguration() {
  if (locked.value) return
  editingPolicy.value = null; config.value = initialConfig(); policyName.value = ''; interval.value = 1440; dailyLimit.value = 100
  notifyEnabled.value = false; recipients.value = ''; failureThreshold.value = 3; takeOverLegacy.value = false
  error.value = ''; notice.value = ''; editorOpen.value = true
  if (readyKeys.value[0]) changeKey(readyKeys.value[0].id)
}
watch(readyKeys, keys => { if (!config.value.managed_key_id && keys[0]) changeKey(keys[0].id) })
watch(() => props.siteId, () => {
  generation++; refreshGeneration++; clearTimer(); pendingStart = null
  policies.value = []; runs.value = null; loading.value = false; busy.value = false; error.value = ''; notice.value = ''
  page.value = 1; batchId.value = ''; detailId.value = ''; deleteId.value = null; editorOpen.value = false
  comparison.value = null; statsGeneration++; stats.value = null; statsError.value = ''; statsExpanded.value = false
  editingPolicy.value = null; config.value = initialConfig()
  if (readyKeys.value[0]) changeKey(readyKeys.value[0].id)
  void refresh()
}, { immediate: true })
watch(busy, value => emit('busy', value), { flush: 'sync' })
onUnmounted(() => { generation++; refreshGeneration++; clearTimer(); emit('busy', false) })
function integer(value: number, maximum: number) { return Number.isInteger(value) && value > 0 && value <= maximum }
function validatedConfig(): ModelTestConfig | null {
  const value = config.value
  if (!selectedKey.value) { error.value = mt('selectKeyFirst'); return null }
  if (!value.model.trim() || value.model.length > 256 || !modes.includes(value.api_mode) || !modeAvailable(value.api_mode) || !value.efforts.length || !value.templates.length || value.efforts.some(item => !efforts.includes(item)) || value.templates.some(item => !templates.includes(item))) { error.value = mt('invalidConfiguration'); return null }
  if (!integer(value.samples, 100) || !integer(value.concurrency, 32) || requestCount.value > 300 || !integer(value.max_output_tokens, 65536) || !integer(value.timeout_seconds, 1800) || (!integer(value.input_tokens, 500000) || (value.templates.some(item => item === 'context' || item === 'token_audit') && value.input_tokens < 64)) || !integer(value.first_content_timeout_seconds || 0, 1800) || !integer(value.idle_timeout_seconds || 0, 1800) || !Number.isFinite(value.token_tolerance_percent) || value.token_tolerance_percent < 0 || value.token_tolerance_percent > 100 || !['auto', 'none', 'o200k_base', 'cl100k_base'].includes(value.tokenizer)) { error.value = mt('invalidLimits'); return null }
  if (value.first_content_timeout_seconds! > value.timeout_seconds || value.idle_timeout_seconds! > value.timeout_seconds) { error.value = mt('timeoutOrderInvalid'); return null }
  return { ...value, platform: selectedKey.value.platform, model: value.model.trim(), efforts: [...value.efforts], templates: [...value.templates] }
}
async function mutate(action: () => Promise<void>) {
  if (locked.value) return
  const request = generation
  busy.value = true; refreshGeneration++; clearTimer(); error.value = ''; notice.value = ''
  try { await action() } catch (e) { if (request === generation) error.value = failure(e) }
  finally { if (request === generation) { busy.value = false; void refresh(true) } }
}
async function start() {
  if (locked.value) return
  const value = validatedConfig()
  if (!value) return
  const request = generation, id = props.siteId, fingerprint = JSON.stringify(value)
  if (!pendingStart || pendingStart.fingerprint !== fingerprint) pendingStart = { fingerprint, requestId: crypto.randomUUID() }
  const input: ModelBatchInput = { request_id: pendingStart.requestId, config: value }
  await mutate(async () => {
    const batch = await api.startBatch(id, input)
    if (request !== generation) return
    pendingStart = null; batchId.value = batch.id; page.value = 1; notice.value = mt('batchStarted')
    runs.value = { items: [], total: batch.total, page: 1, page_size: 20, counts: { queued: batch.total } }
  })
}
async function savePolicy(enableNow = false) {
  if (locked.value) return
  const value = validatedConfig()
  if (!value) return
  const intervalError = intervalValidationKey(interval.value)
  if (intervalError) { error.value = t(intervalError); return }
  if (!integer(dailyLimit.value, 1000000) || !integer(failureThreshold.value, 100)) { error.value = mt('invalidPolicy'); return }
  if (dailyLimit.value < requestCount.value) { error.value = mt('dailyBudgetTooSmall'); return }
  const request = generation, id = props.siteId
  const input: ModelPolicy = { id: editingPolicy.value?.id || 0, site_id: id, version: editingPolicy.value?.version || 0, name: policyName.value.trim() || `${groupNames.value.get(selectedKey.value!.remote_group_id) || mt('unnamedGroup')} · ${value.model}`, config: value, enabled: enableNow || editingPolicy.value?.enabled || false, interval_minutes: interval.value, daily_request_limit: dailyLimit.value, notify_enabled: notifyEnabled.value, recipients: [...new Set(recipients.value.split(/[\s,;，；]+/).map(item => item.trim()).filter(Boolean))], failure_threshold: failureThreshold.value, take_over_legacy: takeOverLegacy.value }
  await mutate(async () => {
    const policy = await api.savePolicy(id, input)
    if (request !== generation) return
    editingPolicy.value = policy; policyName.value = policy.name; takeOverLegacy.value = false; notice.value = mt(enableNow ? 'policyEnabled' : 'policySaved')
  })
}
function editPolicy(policy: ModelPolicy) {
  if (locked.value) return
  installPolicy(policy)
}
function installPolicy(policy: ModelPolicy) {
  editingPolicy.value = policy; config.value = { ...initialConfig(), ...policy.config, efforts: [...policy.config.efforts], templates: [...policy.config.templates] }
  config.value.first_content_timeout_seconds ||= 60; config.value.idle_timeout_seconds ||= 30
  policyName.value = policy.name; interval.value = policy.interval_minutes; dailyLimit.value = policy.daily_request_limit
  notifyEnabled.value = policy.notify_enabled; recipients.value = policy.recipients.join('\n'); failureThreshold.value = policy.failure_threshold; takeOverLegacy.value = false
  editorOpen.value = true; error.value = ''; notice.value = ''
}
async function togglePolicy(policy: ModelPolicy) {
  const request = generation, id = props.siteId
  await mutate(async () => {
    try {
      const value = await api.savePolicy(id, { ...policy, enabled: !policy.enabled, take_over_legacy: false })
      if (request === generation && editingPolicy.value?.id === value.id) { editingPolicy.value = value; takeOverLegacy.value = false }
    } catch (e) {
      if (!policy.enabled && (e as { status?: number })?.status === 409) {
        const latest = await api.policies(id).catch(() => null)
        if (request === generation) {
          installPolicy(latest?.find(value => value.id === policy.id) || policy)
          notice.value = mt('enableConflictHint')
        }
      }
      throw e
    }
  })
}
async function removePolicy(policy: ModelPolicy) {
  const request = generation, id = props.siteId
  await mutate(async () => { await api.deletePolicy(id, policy.id, policy.version); if (request === generation) { deleteId.value = null; if (editingPolicy.value?.id === policy.id) { editingPolicy.value = null; editorOpen.value = false } } })
}
async function cancelBatch() {
  if (!batchId.value) return
  const id = props.siteId, batch = batchId.value
  await mutate(() => api.cancelBatch(id, batch))
}
function showBatch(id: string) { if (locked.value) return; batchId.value = id; page.value = 1; void refresh() }
function changePage(value: number) { if (locked.value || loading.value) return; page.value = value; void refresh() }
function reviewed(value: ModelRun) { if (runs.value) runs.value.items = runs.value.items.map(run => run.id === value.id ? value : run) }
function compare(run: ModelRun) { comparison.value = { batchId: run.batch_id, template: run.request.template, sample: run.request.sample } }
function comparisonDetail(id: string) { comparison.value = null; detailId.value = id }
</script>

<template>
  <section class="min-w-0 space-y-5" data-test="model-monitor-panel">
    <header class="flex flex-wrap items-start justify-between gap-3"><div class="min-w-0 flex-1"><h3 class="font-semibold">{{ mt('title') }}</h3><p class="mt-1 max-w-3xl text-xs leading-relaxed text-gray-500">{{ mt('description') }}</p></div><div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-sm" :disabled="locked || loading" :aria-label="t('common.refresh')" @click="refresh()"><Icon name="refresh" size="sm" /></button><button type="button" data-test="model-new" class="btn btn-primary text-sm" :disabled="locked" @click="newConfiguration">{{ mt('newTest') }}</button></div></header>
    <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ error }}</p><p v-if="notice" role="status" class="rounded-xl bg-primary-50 p-3 text-sm text-primary-700 dark:bg-primary-900/20 dark:text-primary-300">{{ notice }}</p>
    <div class="grid grid-cols-2 gap-3 xl:grid-cols-4"><div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('scheduledPolicies') }}</p><p class="mt-1 text-xl font-semibold tabular-nums">{{ activePolicyCount }}<span class="ml-1 text-xs font-normal text-gray-400">/ {{ policies.length }}</span></p></div><div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('inProgress') }}</p><p class="mt-1 text-xl font-semibold tabular-nums">{{ runningCount }}</p></div><div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('finished') }}</p><p class="mt-1 text-xl font-semibold tabular-nums">{{ completedCount }}</p></div><div class="rounded-xl bg-gray-50 p-4 dark:bg-dark-900"><p class="text-xs text-gray-500">{{ mt('needsAttention') }}</p><p class="mt-1 text-xl font-semibold tabular-nums">{{ (counts.failed || 0) + (counts.indeterminate || 0) }}</p></div></div>
    <p v-if="!readyKeys.length" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600">{{ mt('noKeys') }} <button type="button" class="font-medium text-primary-700 dark:text-primary-300" :disabled="locked" @click="emit('manageKeys')">{{ t('governance.viewKeys') }}</button></p>
    <section v-if="editorOpen" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5" data-test="model-editor">
      <div class="mb-4 flex items-center justify-between gap-3"><h4 class="font-semibold">{{ editingPolicy ? mt('editPolicy') : mt('configureTest') }}</h4><button type="button" class="rounded p-1 text-gray-500" :aria-label="t('common.close')" :disabled="locked" @click="editorOpen = false"><Icon name="x" size="sm" /></button></div>
      <form class="space-y-5" @submit.prevent="start"><fieldset :disabled="locked" class="min-w-0 space-y-5">
        <div class="grid gap-4 lg:grid-cols-3">
          <div class="min-w-0"><label :for="`model-key-${siteId}`" class="mb-1 block text-sm font-medium">{{ mt('groupKey') }}</label><Select :id="`model-key-${siteId}`" data-test="model-key" :model-value="config.managed_key_id || null" :options="keyOptions" :disabled="locked" :aria-label="mt('groupKey')" @update:model-value="changeKey"><template #selected="{ option }"><span v-if="option" class="flex items-center gap-2"><PlatformIcon :platform="option.platform as Transport" size="sm" /><span class="truncate">{{ option.label }}</span></span><span v-else>{{ mt('selectKeyFirst') }}</span></template><template #option="{ option }"><span class="flex min-w-0 items-center gap-2"><PlatformIcon :platform="option.platform as Transport" size="sm" /><span class="break-words">{{ option.label }}</span></span></template></Select></div>
          <div class="min-w-0"><label :for="`model-name-${siteId}`" class="mb-1 block text-sm font-medium">{{ t('governance.model') }}</label><Select :id="`model-name-${siteId}`" data-test="model-name" :model-value="config.model" :options="modelOptions" searchable creatable :disabled="locked" :aria-label="t('governance.model')" :placeholder="mt('modelPlaceholder')" @update:model-value="changeModel" /></div>
          <div class="min-w-0"><label :for="`model-mode-${siteId}`" class="mb-1 block text-sm font-medium">{{ mt('apiMode') }}</label><Select :id="`model-mode-${siteId}`" data-test="model-mode" :model-value="config.api_mode" :options="modeOptions" :disabled="locked" :aria-label="mt('apiMode')" @update:model-value="changeMode"><template #selected="{ option }"><span v-if="option" class="flex items-center gap-2"><PlatformIcon :platform="option.platform as Transport" size="sm" />{{ option.label }}</span></template><template #option="{ option }"><span class="flex items-center gap-2"><PlatformIcon :platform="option.platform as Transport" size="sm" />{{ option.label }}</span></template></Select></div>
        </div>
        <p class="-mt-2 text-xs leading-relaxed" :class="usingCandidateModels ? 'text-amber-700 dark:text-amber-300' : 'text-gray-500'">{{ mt(usingCandidateModels ? 'candidateModelsHint' : 'modelCandidatesHint') }}<span v-if="collectedAt && !usingCandidateModels"> · {{ formatGovernanceTime(collectedAt) }}</span></p>
        <div class="grid gap-5 lg:grid-cols-[1fr_2fr]"><fieldset class="space-y-2"><legend class="mb-2 text-sm font-medium">{{ mt('efforts') }}</legend><div class="flex flex-wrap gap-2"><label v-for="effort in efforts" :key="effort" class="governance-checkbox-label flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm" :class="config.efforts.includes(effort) ? 'border-primary-300 bg-primary-50 dark:border-primary-700 dark:bg-primary-900/20' : 'border-gray-200 dark:border-dark-600'"><input v-model="config.efforts" :data-test="`model-effort-${effort}`" type="checkbox" class="governance-checkbox" :value="effort" />{{ mt(`effort_${effort}`) }}</label></div><p class="text-xs leading-relaxed text-gray-500">{{ mt('effortHint') }}</p></fieldset><fieldset><legend class="mb-2 text-sm font-medium">{{ mt('templates') }}</legend><div class="flex flex-wrap gap-2"><label v-for="template in templates" :key="template" class="governance-checkbox-label flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 text-sm" :class="config.templates.includes(template) ? 'border-primary-300 bg-primary-50 dark:border-primary-700 dark:bg-primary-900/20' : 'border-gray-200 dark:border-dark-600'"><input v-model="config.templates" :data-test="`model-template-${template}`" type="checkbox" class="governance-checkbox" :value="template" />{{ mt(`template_${template}`) }}</label></div></fieldset></div>
        <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm font-medium">{{ mt('samples') }}<input v-model.number="config.samples" data-test="model-samples" type="number" min="1" max="100" step="1" required class="input mt-1 w-full" /></label><label class="text-sm font-medium">{{ mt('concurrency') }}<input v-model.number="config.concurrency" data-test="model-concurrency" type="number" min="1" max="32" step="1" required class="input mt-1 w-full" /></label></div>
        <p class="-mt-2 text-xs text-gray-500">{{ mt('concurrencyHint') }}</p>
        <details class="rounded-lg bg-gray-50 p-3 dark:bg-dark-900"><summary class="cursor-pointer text-sm font-medium">{{ mt('advanced') }}</summary><div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-3"><label class="text-sm">{{ mt('maxOutput') }}<input v-model.number="config.max_output_tokens" data-test="model-max-output" type="number" min="1" max="65536" step="1" required class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('timeoutSeconds') }}<input v-model.number="config.timeout_seconds" data-test="model-timeout" type="number" min="1" max="1800" step="1" required class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('inputTokensRequested') }}<input v-model.number="config.input_tokens" data-test="model-input-tokens" type="number" :min="config.templates.some(item => item === 'context' || item === 'token_audit') ? 64 : 1" max="500000" step="1" required class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('firstContentTimeout') }}<input v-model.number="config.first_content_timeout_seconds" data-test="model-first-timeout" type="number" min="1" :max="config.timeout_seconds" step="1" required class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('idleTimeout') }}<input v-model.number="config.idle_timeout_seconds" data-test="model-idle-timeout" type="number" min="1" :max="config.timeout_seconds" step="1" required class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('tolerance') }}<input v-model.number="config.token_tolerance_percent" data-test="model-tolerance" type="number" min="0" max="100" step="any" required class="input mt-1 w-full" /></label><div><label :for="`model-tokenizer-${siteId}`" class="mb-1 block text-sm">Tokenizer</label><Select :id="`model-tokenizer-${siteId}`" :model-value="config.tokenizer" :options="tokenizerOptions" :disabled="locked" aria-label="Tokenizer" @update:model-value="changeTokenizer" /></div></div><p class="mt-3 text-xs leading-relaxed text-gray-500">{{ mt('inputBudgetHint') }}</p></details>
        <div class="flex flex-wrap items-center justify-between gap-3 rounded-xl bg-primary-50 p-4 dark:bg-primary-900/20"><div><p class="text-sm font-medium" data-test="model-request-count">{{ mt('requestPreview') }}：{{ requestCount }} {{ mt('requests') }}</p><p class="mt-1 text-xs text-gray-500">{{ config.efforts.length }} {{ mt('effortUnits') }} × {{ config.templates.length }} {{ mt('templateUnits') }} × {{ config.samples }} {{ mt('sampleUnits') }} · {{ mt('maxOutputPreview') }} {{ requestCount * (Number(config.max_output_tokens) || 0) }}</p></div><button data-test="model-start" class="btn btn-primary" :disabled="locked || !readyKeys.length">{{ busy ? t('common.loading') : mt('runNow') }}</button></div>
        <p class="text-xs leading-relaxed text-gray-500">{{ mt('billingHint') }}</p>
        <details class="border-t border-gray-100 pt-4 dark:border-dark-700" :open="!!editingPolicy"><summary class="cursor-pointer text-sm font-medium">{{ mt('scheduledConfiguration') }}</summary><div class="mt-4 grid gap-4 sm:grid-cols-2"><label class="text-sm">{{ mt('policyName') }}<input v-model="policyName" data-test="model-policy-name" maxlength="120" class="input mt-1 w-full" /></label><label class="text-sm">{{ t('governance.interval') }}<input v-model.number="interval" data-test="model-policy-interval" type="number" min="1" step="1" class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('dailyLimit') }}<input v-model.number="dailyLimit" data-test="model-daily-limit" type="number" min="1" max="1000000" step="1" class="input mt-1 w-full" /></label><label class="text-sm">{{ mt('failureThreshold') }}<input v-model.number="failureThreshold" type="number" min="1" max="100" step="1" class="input mt-1 w-full" /></label><label class="governance-checkbox-label flex items-center gap-2 text-sm"><input v-model="notifyEnabled" data-test="model-notify" type="checkbox" class="governance-checkbox" />{{ mt('emailNotifications') }}</label><label class="governance-checkbox-label flex items-center gap-2 text-sm"><input v-model="takeOverLegacy" data-test="model-take-over" type="checkbox" class="governance-checkbox" />{{ mt('takeOverLegacy') }}</label><label v-if="notifyEnabled" class="text-sm sm:col-span-2">{{ mt('recipients') }}<textarea v-model="recipients" data-test="model-recipients" class="input mt-1 min-h-20 w-full" /></label></div><p class="mt-3 text-xs leading-relaxed text-gray-500">{{ mt('scheduleHint') }}</p><div class="mt-3 flex flex-wrap gap-2"><button type="button" data-test="model-policy-save" class="btn btn-secondary" :disabled="locked || !readyKeys.length" @click="savePolicy()">{{ mt('savePolicy') }}</button><button v-if="editingPolicy && !editingPolicy.enabled" type="button" data-test="model-policy-save-enable" class="btn btn-primary" :disabled="locked || !readyKeys.length" @click="savePolicy(true)">{{ mt('saveAndEnable') }}</button></div></details>
      </fieldset></form>
    </section>
    <section class="space-y-3" data-test="model-stats">
      <div class="flex flex-wrap items-center justify-between gap-3"><div><h4 class="font-semibold">{{ mt('trends') }}</h4><p class="mt-1 text-xs text-gray-500">{{ mt('trendScope') }}</p></div><Select class="w-36" data-test="model-stats-window" :model-value="statsDays" :options="statsWindowOptions" :aria-label="mt('timeWindow')" @update:model-value="changeStatsWindow" /></div>
      <p v-if="statsError" role="status" class="text-xs text-amber-700 dark:text-amber-300">{{ statsError }}</p><p v-else-if="!statsGroups.length" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600">{{ mt('noStats') }}</p>
      <details v-for="group in statsGroups" :key="`${group.config_hash}:${group.template}:${group.effort}:${group.template_version}:${group.adapter_version}`" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600"><summary class="cursor-pointer text-sm"><span class="font-medium">{{ group.model }}</span><span class="ml-2 text-xs text-gray-500">{{ mt(`template_${group.template}`) }} · {{ group.effort }} · {{ group.samples }} {{ mt('sampleUnits') }}</span></summary><div class="mt-4 space-y-3"><p class="text-xs text-gray-500">{{ group.api_mode }} · {{ keyLabel(group.managed_key_id) }} · {{ group.template_version || mt('unknown') }} / {{ group.adapter_version || mt('unknown') }}</p><div class="grid grid-cols-2 gap-3 text-xs sm:grid-cols-4"><p>{{ mt('successSamples') }} <strong>{{ group.successes }} / {{ group.samples }}</strong></p><p>{{ mt('failedSamples') }} <strong>{{ group.failures }}</strong></p><p>TTFT p50 <strong>{{ group.comparable && (group.ttft_samples || 0) >= 5 ? countValue(group.p50_ttft_ms) : mt('insufficientSamples') }}</strong><span v-if="group.comparable && (group.ttft_samples || 0) >= 5 && group.p50_ttft_ms != null"> ms</span></p><p>TTFT p95 <strong>{{ group.comparable && (group.ttft_samples || 0) >= 5 ? countValue(group.p95_ttft_ms) : mt('insufficientSamples') }}</strong><span v-if="group.comparable && (group.ttft_samples || 0) >= 5 && group.p95_ttft_ms != null"> ms</span></p></div><div class="flex gap-1 overflow-x-auto py-1" :aria-label="mt('healthMatrix')"><span v-for="point in group.points" :key="point.at" role="img" :aria-label="`${formatGovernanceTime(point.at)}: ${point.successes}/${point.samples}, ${point.failures} ${mt('failedSamples')}`" :title="`${formatGovernanceTime(point.at)} · ${mt('successSamples')} ${point.successes}/${point.samples} · ${mt('failedSamples')} ${point.failures} · TTFT ${countValue(point.ttft_ms)}`" class="h-7 min-w-3 flex-1 rounded-sm" :class="point.failures > 0 ? 'bg-red-400 dark:bg-red-600' : point.successes > 0 ? 'bg-emerald-400 dark:bg-emerald-600' : 'bg-gray-200 dark:bg-dark-600'" /></div><div class="flex flex-wrap gap-3 text-xs text-gray-500"><span>● {{ mt('matrixSuccess') }}</span><span class="text-red-600 dark:text-red-300">● {{ mt('matrixFailure') }}</span><span>{{ mt('unknown') }} {{ group.unknown }} · {{ mt('latestRun') }} {{ formatGovernanceTime(group.last_run_at) }}</span></div></div></details>
      <button v-if="(stats?.groups.length || 0) > 6" type="button" class="text-sm font-medium text-primary-700 dark:text-primary-300" @click="statsExpanded = !statsExpanded">{{ mt(statsExpanded ? 'collapseStats' : 'expandStats') }}</button><p v-if="stats?.truncated" class="text-xs text-gray-500">{{ mt('statsTruncated') }}</p>
    </section>
    <section class="space-y-3"><div class="flex items-center justify-between gap-3"><h4 class="font-semibold">{{ mt('policies') }}</h4><span class="text-xs text-gray-500">{{ policies.length }}</span></div><p v-if="!policies.length" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600">{{ mt('noPolicies') }}</p><article v-for="policy in policies" :key="policy.id" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600"><div class="flex flex-wrap items-center gap-3"><PlatformIcon :platform="policy.config.platform" size="md" /><div class="min-w-0 flex-1"><p class="break-words text-sm font-semibold">{{ policy.name }}</p><p class="mt-1 break-all text-xs text-gray-500">{{ policy.config.model }} · {{ policy.config.efforts.join(' / ') }} · {{ policy.interval_minutes }} {{ t('governance.minutes') }}</p><p v-if="policy.enabled" class="mt-1 text-xs text-gray-500">{{ mt('nextRun') }} {{ formatGovernanceTime(policy.next_run_at) }}</p></div><span class="rounded-full px-2 py-1 text-xs" :class="policy.enabled ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300' : 'bg-gray-100 text-gray-500 dark:bg-dark-700'">{{ mt(policy.enabled ? 'enabled' : 'paused') }}</span><div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-xs" :disabled="locked" :data-test="`model-policy-edit-${policy.id}`" @click="editPolicy(policy)">{{ t('common.edit') }}</button><button type="button" class="btn btn-secondary text-xs" :disabled="locked" :data-test="`model-policy-toggle-${policy.id}`" @click="togglePolicy(policy)">{{ mt(policy.enabled ? 'pause' : 'enable') }}</button><button type="button" class="rounded-lg p-2 text-gray-400 hover:text-red-600" :disabled="locked" :aria-label="t('common.delete')" @click="deleteId = policy.id"><Icon name="trash" size="sm" /></button></div></div><p v-if="policy.last_error" class="mt-2 break-all text-xs text-amber-700 dark:text-amber-300">{{ policy.last_error }}</p><p v-if="policy.notify_error" class="mt-2 break-all text-xs text-red-600">{{ mt('notificationFailure') }}：{{ policy.notify_error }}</p><p v-else-if="policy.notify_at" class="mt-2 text-xs text-gray-500">{{ mt('lastNotification') }} {{ formatGovernanceTime(policy.notify_at) }}</p><div v-if="deleteId === policy.id" class="mt-3 flex flex-wrap items-center gap-3 rounded-lg bg-red-50 p-3 text-sm dark:bg-red-900/20"><span class="mr-auto">{{ mt('deletePolicyHint') }}</span><button type="button" class="btn btn-danger text-xs" :disabled="locked" :data-test="`model-policy-delete-${policy.id}`" @click="removePolicy(policy)">{{ t('common.delete') }}</button><button type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="deleteId = null">{{ t('common.cancel') }}</button></div></article></section>
    <section class="space-y-3"><div class="flex flex-wrap items-center justify-between gap-3"><div><h4 class="font-semibold">{{ batchId ? mt('batchProgress') : mt('runHistory') }}</h4><p v-if="batchId" class="mt-1 break-all text-xs text-gray-400">{{ batchId }}</p></div><div class="flex flex-wrap gap-2"><button v-if="batchId" type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="showBatch('')">{{ mt('allRuns') }}</button><button v-if="batchId && runningCount" type="button" data-test="model-batch-cancel" class="btn btn-secondary text-xs" :disabled="locked" @click="cancelBatch">{{ mt('cancelBatch') }}</button></div></div>
      <div v-if="batchId" class="space-y-2"><div class="flex justify-between text-xs text-gray-500"><span>{{ completedCount }} / {{ runs?.total || 0 }} {{ mt('finished') }}</span><span>{{ progress }}%</span></div><div role="progressbar" :aria-label="mt('batchProgress')" :aria-valuenow="progress" aria-valuemin="0" aria-valuemax="100" class="h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-dark-700"><div class="h-full rounded-full bg-primary-500 transition-[width] motion-reduce:transition-none" :style="{ width: `${progress}%` }" /></div><p class="text-xs text-gray-500">{{ mt('cancelHint') }}</p></div>
      <p v-if="loading" role="status" class="py-3 text-center text-sm text-gray-500">{{ t('common.loading') }}</p><p v-else-if="!runs?.items.length" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600">{{ mt('noRuns') }}</p>
      <article v-for="run in runs?.items || []" :key="run.id" class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-600"><PlatformIcon :platform="run.request.config.platform" size="md" /><div class="min-w-0 flex-1"><p class="break-all text-sm font-semibold">{{ run.request.config.model }}</p><p class="mt-1 text-xs text-gray-500">{{ keyLabel(run.request.config.managed_key_id) }} · {{ mt(`template_${run.request.template}`) }} · {{ run.request.effort }} · {{ mt('sample') }} {{ run.request.sample }}</p><time class="mt-1 block text-xs tabular-nums text-gray-400">{{ formatGovernanceTime(run.created_at) }}</time></div><div class="text-xs text-gray-500"><p class="font-medium" :class="run.status === 'failed' || run.status === 'indeterminate' ? 'text-red-600 dark:text-red-300' : ''">{{ stateLabel(run.status) }}</p><p class="mt-1">TTFT {{ countValue(run.result?.ttft_ms) }}<span v-if="run.result?.ttft_ms != null"> ms</span> · {{ countValue(run.result?.duration_ms) }}<span v-if="run.result?.duration_ms != null"> ms</span></p><p class="mt-1">{{ mt('manualReview') }}：{{ mt(`review_${run.review || 'pending'}`) }}</p></div><div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-xs" :data-test="`model-run-${run.id}`" @click="detailId = run.id">{{ mt('details') }}</button><button type="button" class="btn btn-secondary text-xs" :data-test="`model-compare-${run.id}`" @click="compare(run)">{{ mt('compare') }}</button><button v-if="!batchId" type="button" class="btn btn-secondary text-xs" :disabled="locked" @click="showBatch(run.batch_id)">{{ mt('viewBatch') }}</button></div></article>
      <div v-if="runs?.total" class="flex items-center justify-end gap-3 text-xs text-gray-500"><button type="button" class="btn btn-secondary text-xs" :disabled="locked || loading || page <= 1" @click="changePage(page - 1)">{{ t('governance.previous') }}</button><span>{{ page }} / {{ pages }}</span><button type="button" class="btn btn-secondary text-xs" :disabled="locked || loading || page >= pages" @click="changePage(page + 1)">{{ t('governance.next') }}</button></div>
    </section>
    <ModelComparison v-if="comparison" :site-id="siteId" :batch-id="comparison.batchId" :template="comparison.template" :sample="comparison.sample" @close="comparison = null" @detail="comparisonDetail" />
    <ModelRunDetail v-if="detailId" :site-id="siteId" :run-id="detailId" :disabled="disabled" @close="detailId = ''" @reviewed="reviewed" />
  </section>
</template>
