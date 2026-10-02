<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type ObservationPolicy, type PricingMode, type PricingPoliciesConfiguration,
  type PricingPolicy, type PricingPolicyDraft, type PricingPolicyPreview, type PricingSource,
} from '@/api/admin/upstream-governance'
import { formatGovernanceTime, formatGovernanceRate } from './format'
import { pricingFeedback, pricingStatusKey, observationStatusKey } from './pricing-feedback'

const props = withDefaults(defineProps<{ siteId: number; disabled?: boolean }>(), { disabled: false })
const emit = defineEmits<{
  'observation-saved': [value: ObservationPolicy]
  'pricing-saved': [value: PricingPoliciesConfiguration]
  busy: [value: boolean]
  reload: []
}>()
const { t } = useI18n()
const observation = ref<ObservationPolicy | null>(null)
const pricing = ref<PricingPoliciesConfiguration | null>(null)
const observationDraft = ref<ObservationPolicy['policy'] | null>(null)
const pricingDraft = ref<PricingPoliciesConfiguration | null>(null)
const previews = ref<Record<number, PricingPolicyPreview>>({})
const previewDrafts = new Map<number, PricingPolicyDraft>()
const previewing = ref<number | null>(null)
const loading = ref(true)
const savingObservation = ref(false)
const savingPricing = ref<number | null>(null)
const savingNotifications = ref(false)
const notificationVersion = ref<number | null>(null)
const notificationConflict = ref(false)
const error = ref('')
const errorReason = ref('')
const saved = ref('')
const invalidated = ref(new Set<number>())
let generation = 0

const busy = computed(() => loading.value || previewing.value !== null || savingObservation.value || savingPricing.value !== null || savingNotifications.value)
const pricingRows = computed(() => pricingDraft.value?.policies ?? [])
const failure = computed(() => pricingFeedback(errorReason.value))
const modeOptions = computed(() => [
  { value: 'keep_margin', label: t('governance.pricingModeKeepMargin') },
  { value: 'target_margin', label: t('governance.pricingModeTargetMargin') },
])

function editablePolicy(row: PricingPolicy): PricingPolicyDraft {
  return { enabled: row.enabled, mode: row.mode, min_margin: row.min_margin, safety_buffer: row.safety_buffer,
    max_increase_percent: row.max_increase_percent, decrease_stability_seconds: row.decrease_stability_seconds }
}
function cadence(policy: ObservationPolicy['policy']): ObservationPolicy['policy'] {
  return { enabled: policy.enabled, fast_interval_seconds: policy.fast_interval_seconds, full_interval_seconds: policy.full_interval_seconds }
}
function clearPreviews() {
  previews.value = {}
  previewDrafts.clear()
  invalidated.value = new Set()
}
function beginAction() {
  error.value = ''
  errorReason.value = ''
  saved.value = ''
  emit('busy', true)
}
function reportFailure(e: unknown) {
  const value = e as { reason?: unknown; status?: number }
  errorReason.value = typeof value?.reason === 'string' && /^[a-z_]{1,64}$/.test(value.reason)
    ? value.reason : value?.status === 409 ? 'stale_preview' : 'operation_failed'
}
/** Merge authoritative state while retaining unsaved settings outside the write scope. */
function mergePricing(value: PricingPoliciesConfiguration, savedGroup?: number, notificationsSaved = false) {
  const previous = pricingDraft.value
  const drafts = new Map(previous?.policies.map(row => [row.local_group_id, row]))
  const notification = !notificationsSaved && previous ? previous.notifications : value.notifications
  if (!previous || notificationsSaved) {
    notificationVersion.value = value.version
    notificationConflict.value = false
  } else if (notificationVersion.value !== value.version) {
    notificationConflict.value = true
  }
  pricing.value = value
  pricingDraft.value = {
    ...value,
    policies: value.policies.map(row => {
      const draft = drafts.get(row.local_group_id)
      return { ...row, ...(draft && row.local_group_id !== savedGroup ? editablePolicy(draft) : {}), sources: (row.sources || []).map(source => ({ ...source })) }
    }),
    notifications: { ...notification, recipients: [...notification.recipients] },
  }
  clearPreviews()
}
async function load(preserveDrafts = false) {
  const request = ++generation
  loading.value = true
  previewing.value = null
  savingPricing.value = null
  savingNotifications.value = false
  savingObservation.value = false
  beginAction()
  clearPreviews()
  if (!preserveDrafts) { observation.value = null; pricing.value = null; observationDraft.value = null; pricingDraft.value = null; notificationVersion.value = null; notificationConflict.value = false }
  try {
    const [observationValue, pricingValue] = await Promise.all([api.observationPolicy(props.siteId), api.pricingPolicies(props.siteId)])
    if (request !== generation) return
    observation.value = observationValue
    if (!preserveDrafts || !observationDraft.value) observationDraft.value = cadence(observationValue.policy)
    mergePricing(pricingValue)
    if (preserveDrafts) saved.value = 'governance.reliability.draftsPreserved'
  } catch (e) {
    if (request === generation) reportFailure(e)
  } finally {
    if (request === generation) { loading.value = false; emit('busy', false) }
  }
}
function validSeconds(value: unknown): boolean {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}
async function saveObservation() {
  if (!observationDraft.value || !observation.value || busy.value || props.disabled) return
  if (!validSeconds(observationDraft.value.fast_interval_seconds) || !validSeconds(observationDraft.value.full_interval_seconds)) {
    errorReason.value = ''; error.value = t('governance.intervalPositiveInteger'); return
  }
  const request = generation
  savingObservation.value = true
  beginAction()
  try {
    const value = await api.saveObservationPolicy(props.siteId, { version: observation.value.version, policy: cadence(observationDraft.value) })
    if (request !== generation) return
    observation.value = value
    observationDraft.value = cadence(value.policy)
    clearPreviews()
    saved.value = 'governance.reliability.observationSaved'
    emit('observation-saved', value)
  } catch (e) { if (request === generation) reportFailure(e) }
  finally { if (request === generation) { savingObservation.value = false; emit('busy', false) } }
}
function validatePolicy(row: PricingPolicy): boolean {
  const margin = Number(row.min_margin), buffer = Number(row.safety_buffer)
  if (![margin, buffer].every(Number.isFinite) || margin < 0 || buffer < 0 || margin + buffer >= 1) {
    error.value = t('governance.pricingMarginInvalid'); return false
  }
  if (!Number.isFinite(row.max_increase_percent) || row.max_increase_percent < 0 || !Number.isSafeInteger(row.decrease_stability_seconds) || row.decrease_stability_seconds < 0) {
    error.value = t('governance.invalid'); return false
  }
  return true
}
async function previewPricing(id: number) {
  const row = pricingRows.value.find(item => item.local_group_id === id)
  if (!row || busy.value || props.disabled) return
  delete previews.value[id]
  previewDrafts.delete(id)
  errorReason.value = ''; error.value = ''
  if (!validatePolicy(row)) return
  const request = generation
  const draft = editablePolicy(row)
  previewing.value = id
  beginAction()
  try {
    const value = await api.previewPricingPolicy(props.siteId, id, { policy: draft })
    if (request !== generation) return
    if (value.local_group_id !== id || !value.fingerprint) { reportFailure({ reason: 'operation_failed' }); return }
    previews.value[id] = value
    previewDrafts.set(id, draft)
    invalidated.value.delete(id)
  } catch (e) { if (request === generation) reportFailure(e) }
  finally { if (request === generation) { previewing.value = null; emit('busy', false) } }
}
async function savePricing(id: number) {
  const row = pricingRows.value.find(item => item.local_group_id === id)
  const preview = previews.value[id], draft = previewDrafts.get(id)
  if (!row || !preview || !draft || preview.blocked || busy.value || props.disabled) return
  if (JSON.stringify(editablePolicy(row)) !== JSON.stringify(draft)) { updatePolicy(row, 'mode', row.mode); return }
  const request = generation
  savingPricing.value = id
  beginAction()
  try {
    const value = await api.savePricingPolicy(props.siteId, id, { policy: draft, fingerprint: preview.fingerprint })
    if (request !== generation) return
    mergePricing(value, id)
    saved.value = 'governance.reliability.policySaved'
    emit('pricing-saved', value)
  } catch (e) {
    if (request === generation) { delete previews.value[id]; previewDrafts.delete(id); reportFailure(e) }
  } finally { if (request === generation) { savingPricing.value = null; emit('busy', false) } }
}
async function saveNotifications() {
  if (!pricingDraft.value || notificationVersion.value === null || notificationConflict.value || busy.value || props.disabled) return
  const request = generation
  savingNotifications.value = true
  beginAction()
  try {
    const notification = pricingDraft.value.notifications
    const recipients = [...new Set(notification.recipients.flatMap(value => value.split(/[\n,]/)).map(value => value.trim()).filter(Boolean))]
    const value = await api.savePricingNotifications(props.siteId, { version: notificationVersion.value, notifications: { ...notification, recipients } })
    if (request !== generation) return
    mergePricing(value, undefined, true)
    saved.value = 'governance.reliability.notificationsSaved'
    emit('pricing-saved', value)
  } catch (e) {
    if (request === generation) {
      reportFailure(e)
      if (errorReason.value === 'stale_preview' || (e as { status?: number })?.status === 409) notificationConflict.value = true
    }
  }
  finally { if (request === generation) { savingNotifications.value = false; emit('busy', false) } }
}
async function reloadNotifications() {
  if (busy.value || props.disabled) return
  const request = generation
  savingNotifications.value = true
  beginAction()
  try {
    const value = await api.pricingPolicies(props.siteId)
    if (request === generation) mergePricing(value, undefined, true)
  } catch (e) { if (request === generation) reportFailure(e) }
  finally { if (request === generation) { savingNotifications.value = false; emit('busy', false) } }
}
function updatePolicy<K extends keyof PricingPolicyDraft>(row: PricingPolicy, key: K, value: PricingPolicy[K]) {
  row[key] = value
  if (previews.value[row.local_group_id]) invalidated.value.add(row.local_group_id)
  delete previews.value[row.local_group_id]
  previewDrafts.delete(row.local_group_id)
  saved.value = ''
}
function ratioAsPercent(value: number): number | '' {
  return Number.isFinite(value) ? Math.round(value * 10000) / 100 : ''
}
function percentAsRatio(value: string): number {
  return value.trim() && Number.isFinite(Number(value)) ? Number(value) / 100 : Number.NaN
}
function percent(value: number | null | undefined): string {
  return value != null && Number.isFinite(value) ? `${(value * 100).toFixed(2)}%` : '—'
}
function savedRow(id: number): PricingPolicy | undefined { return pricing.value?.policies.find(row => row.local_group_id === id) }
function currentStatus(row: PricingPolicy) { return savedRow(row.local_group_id) || row }
function sourceLabel(source: PricingSource): string {
  return source.source_name || t('governance.reliability.sourceFallback', { id: source.source_id })
}
function affectedSiteCount(preview: PricingPolicyPreview): number { return new Set(preview.bindings.map(binding => binding.site_id)).size }
onMounted(() => load())
watch(() => props.siteId, () => load())
onUnmounted(() => { generation++; emit('busy', false) })
</script>

<template>
  <div class="space-y-5" data-test="observation-pricing-panel">
    <div v-if="error || errorReason" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-4 text-sm text-red-800 dark:border-red-900 dark:bg-red-900/10 dark:text-red-200">
      <template v-if="errorReason"><p class="font-semibold">{{ t(failure.titleKey) }}</p><p class="mt-1">{{ t(failure.detailKey) }}</p><p class="mt-2">{{ t(failure.actionKey) }}</p><button class="mt-3 underline underline-offset-2 disabled:opacity-50" type="button" :disabled="busy || props.disabled" @click="load(true)">{{ t('common.refresh') }}</button></template>
      <p v-else>{{ error }}</p>
    </div>
    <p v-if="saved" role="status" class="rounded-xl border border-primary-100 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-900/40 dark:bg-primary-900/10 dark:text-primary-300">{{ t(saved) }}</p>
    <p v-if="loading" role="status" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600">{{ t('common.loading') }}</p>

    <section v-if="observationDraft" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
      <div class="flex flex-wrap items-start justify-between gap-3"><div><h3 class="font-semibold">{{ t('governance.observationPolicyTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.observationPolicyHint') }}</p></div><span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium dark:bg-dark-700">{{ t(observation?.policy.enabled ? 'governance.observationOn' : 'governance.observationOff') }}</span></div>
      <form data-test="observation-form" class="mt-5 space-y-4" @submit.prevent="saveObservation">
        <fieldset :disabled="busy || props.disabled" class="space-y-4">
          <label class="governance-checkbox-label flex items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm font-medium dark:bg-dark-900"><input v-model="observationDraft.enabled" data-test="observation-enabled" type="checkbox" class="governance-checkbox" />{{ t('governance.enableSecondObservation') }}</label>
          <div class="grid gap-4 sm:grid-cols-2">
            <label class="text-sm">{{ t('governance.fastObservationInterval') }}<div class="mt-1 flex items-center gap-2"><input v-model.number="observationDraft.fast_interval_seconds" data-test="fast-interval-seconds" class="input w-full" type="number" min="1" step="1" required /><span class="text-xs text-gray-500">{{ t('governance.seconds') }}</span></div></label>
            <label class="text-sm">{{ t('governance.fullCollectionInterval') }}<div class="mt-1 flex items-center gap-2"><input v-model.number="observationDraft.full_interval_seconds" data-test="full-interval-seconds" class="input w-full" type="number" min="1" step="1" required /><span class="text-xs text-gray-500">{{ t('governance.seconds') }}</span></div></label>
          </div>
          <p class="text-xs leading-relaxed text-gray-500">{{ t('governance.reliability.observationScopeHint') }}</p>
          <div class="grid gap-3 rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-900 sm:grid-cols-3">
            <p>{{ t('governance.lastFastObservation') }}<strong class="mt-1 block font-medium text-gray-700 dark:text-gray-200">{{ formatGovernanceTime(observation?.status?.last_fast_observed_at) }}</strong></p>
            <p>{{ t('governance.nextFastObservation') }}<strong class="mt-1 block font-medium text-gray-700 dark:text-gray-200">{{ formatGovernanceTime(observation?.status?.next_fast_observation_at) }}</strong></p>
            <p>{{ t('governance.observationStatus') }}<strong class="mt-1 block font-medium text-gray-700 dark:text-gray-200">{{ t(observationStatusKey(observation?.status?.fast_observe_status || '')) }}</strong></p>
          </div>
          <div v-if="observation?.status?.fast_observe_error" data-test="observation-error" class="rounded-lg bg-amber-50 p-3 text-xs leading-relaxed text-amber-800 dark:bg-amber-900/20 dark:text-amber-300"><p class="font-semibold">{{ t(pricingFeedback(observation.status.fast_observe_error).titleKey) }}</p><p class="mt-1">{{ t(pricingFeedback(observation.status.fast_observe_error).detailKey) }}</p><p class="mt-2">{{ t(pricingFeedback(observation.status.fast_observe_error).actionKey) }}</p></div>
          <button data-test="save-observation" class="btn btn-primary" type="submit">{{ t('common.save') }}</button>
        </fieldset>
      </form>
    </section>

    <section v-if="pricingDraft" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
      <div class="flex items-start justify-between gap-3"><div><h3 class="font-semibold">{{ t('governance.pricingProtectionTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.reliability.pricingScopeHint') }}</p></div><Icon name="shield" size="md" class="shrink-0 text-primary-600" /></div>
      <p v-if="!pricingRows.length" class="mt-4 rounded-lg border border-dashed border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600">{{ t('governance.noPricingPolicies') }}</p>
      <article v-for="row in pricingRows" :key="row.local_group_id" class="mt-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600" :data-test="`pricing-row-${row.local_group_id}`">
        <header class="flex flex-wrap items-start gap-3">
          <div class="min-w-0 flex-1"><h4 class="break-words font-semibold">{{ row.local_group_name }}</h4><p class="mt-1 break-words text-xs text-gray-500">{{ t('governance.pricingSources') }}：{{ row.sources.map(sourceLabel).join('、') || t('governance.unknown') }}</p></div>
          <span :data-test="`pricing-status-${row.local_group_id}`" class="rounded-full px-2.5 py-1 text-xs" :class="currentStatus(row).protected ? 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(pricingStatusKey(currentStatus(row))) }}</span>
        </header>
        <div class="mt-4 grid grid-cols-2 gap-3 rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-900 lg:grid-cols-4">
          <p class="text-xs text-gray-500">{{ t('governance.currentCost') }}<strong class="mt-1 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ formatGovernanceRate(row.current_cost) }}</strong></p>
          <p class="text-xs text-gray-500">{{ t('governance.currentSale') }}<strong class="mt-1 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ formatGovernanceRate(row.current_sale) }}</strong></p>
          <p class="text-xs text-gray-500">{{ t('governance.reliability.savedTarget') }}<strong class="mt-1 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ formatGovernanceRate(row.target_sale) }}</strong></p>
          <p class="text-xs text-gray-500">{{ t('governance.effectiveMargin') }}<strong class="mt-1 block text-sm font-medium text-gray-900 dark:text-gray-100">{{ percent(row.current_cost != null && row.current_sale ? (row.current_sale - row.current_cost) / row.current_sale : null) }}</strong></p>
        </div>
        <div v-if="!currentStatus(row).enabled || currentStatus(row).protected || currentStatus(row).manual_owner" :data-test="`pricing-protection-${row.local_group_id}`" class="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3 text-xs leading-relaxed text-amber-900 dark:border-amber-800 dark:bg-amber-900/10 dark:text-amber-200">
          <template v-for="feedback in [pricingFeedback(!currentStatus(row).enabled ? 'policy_disabled' : currentStatus(row).manual_owner ? 'manual_owner' : currentStatus(row).protection_reason || '')]" :key="feedback.titleKey"><p class="font-semibold">{{ t(feedback.titleKey) }}</p><p class="mt-1">{{ t(feedback.detailKey) }}</p><p class="mt-2">{{ t(feedback.actionKey) }}</p></template>
        </div>
        <fieldset :disabled="busy || props.disabled" class="mt-4 space-y-4">
          <label class="governance-checkbox-label flex items-center gap-2 text-sm"><input type="checkbox" class="governance-checkbox" :checked="row.enabled" :data-test="`pricing-enabled-${row.local_group_id}`" @change="updatePolicy(row, 'enabled', ($event.target as HTMLInputElement).checked)" />{{ t('governance.reliability.enablePricing') }}</label>
          <div class="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <label class="text-sm">{{ t('governance.pricingMode') }}<Select class="mt-1" :model-value="row.mode" :options="modeOptions" :disabled="busy || props.disabled" :aria-label="t('governance.pricingMode')" @update:model-value="value => updatePolicy(row, 'mode', value as PricingMode)" /></label>
            <label class="text-sm">{{ t('governance.minimumMargin') }}<input :value="ratioAsPercent(row.min_margin)" data-test="pricing-min-margin" class="input mt-1 w-full" type="number" min="0" max="99.99" step="0.01" @input="updatePolicy(row, 'min_margin', percentAsRatio(($event.target as HTMLInputElement).value))" /></label>
            <label class="text-sm">{{ t('governance.safetyBuffer') }}<input :value="ratioAsPercent(row.safety_buffer)" data-test="pricing-safety-buffer" class="input mt-1 w-full" type="number" min="0" max="99.99" step="0.01" @input="updatePolicy(row, 'safety_buffer', percentAsRatio(($event.target as HTMLInputElement).value))" /></label>
            <label class="text-sm">{{ t('governance.maxRateIncrease') }}<input :value="row.max_increase_percent" data-test="pricing-max-increase" class="input mt-1 w-full" type="number" min="0" step="any" @input="updatePolicy(row, 'max_increase_percent', ($event.target as HTMLInputElement).value.trim() ? Number(($event.target as HTMLInputElement).value) : NaN)" /></label>
            <label class="text-sm">{{ t('governance.decreaseStability') }}（{{ t('governance.seconds') }}）<input :value="row.decrease_stability_seconds" data-test="pricing-decrease-stability" class="input mt-1 w-full" type="number" min="0" step="1" @input="updatePolicy(row, 'decrease_stability_seconds', ($event.target as HTMLInputElement).value.trim() ? Number(($event.target as HTMLInputElement).value) : NaN)" /></label>
          </div>
          <p class="text-xs leading-relaxed text-gray-500">{{ t('governance.reliability.savePolicyHint') }}</p>
          <div class="flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-sm" :data-test="`pricing-preview-${row.local_group_id}`" @click="previewPricing(row.local_group_id)">{{ t(previewing === row.local_group_id ? 'governance.reliability.previewLoading' : 'governance.reliability.previewAction') }}</button><button v-if="previews[row.local_group_id] && !previews[row.local_group_id].blocked" type="button" class="btn btn-primary text-sm" :data-test="`pricing-apply-${row.local_group_id}`" @click="savePricing(row.local_group_id)">{{ t('governance.reliability.savePolicyAction') }}</button></div>
        </fieldset>
        <p v-if="invalidated.has(row.local_group_id)" class="mt-3 text-xs text-amber-700 dark:text-amber-300">{{ t('governance.reliability.previewInvalidated') }}</p>
        <section v-if="previews[row.local_group_id]" :data-test="`pricing-preview-result-${row.local_group_id}`" class="mt-4 rounded-lg border border-primary-200 bg-primary-50/60 p-4 dark:border-primary-800 dark:bg-primary-900/10" aria-live="polite">
          <p class="text-xs font-medium text-primary-800 dark:text-primary-200">{{ t('governance.reliability.previewReadOnly') }}</p>
          <dl class="mt-3 grid grid-cols-2 gap-3 text-sm lg:grid-cols-4">
            <div><dt class="text-xs text-gray-500">{{ t('governance.currentSale') }}</dt><dd class="mt-1 font-semibold tabular-nums">{{ formatGovernanceRate(previews[row.local_group_id].current_sale) }}</dd></div>
            <div><dt class="text-xs text-gray-500">{{ t('governance.reliability.previewCost') }}</dt><dd class="mt-1 font-semibold tabular-nums">{{ formatGovernanceRate(previews[row.local_group_id].current_cost) }}</dd></div>
            <div><dt class="text-xs text-gray-500">{{ t('governance.reliability.previewTarget') }}</dt><dd class="mt-1 font-semibold tabular-nums">{{ formatGovernanceRate(previews[row.local_group_id].target_sale) }}</dd></div>
            <div><dt class="text-xs text-gray-500">{{ t('governance.reliability.previewMargin') }}</dt><dd class="mt-1 font-semibold tabular-nums">{{ percent(previews[row.local_group_id].projected_margin) }}</dd></div>
          </dl>
          <div v-for="feedback in [pricingFeedback(previews[row.local_group_id].reason)]" :key="feedback.titleKey" class="mt-4 text-xs leading-relaxed text-gray-600 dark:text-gray-300"><p class="font-semibold">{{ t(feedback.titleKey) }}</p><p class="mt-1">{{ t(feedback.detailKey) }}</p><p class="mt-2">{{ t(feedback.actionKey) }}</p></div>
          <div class="mt-4 border-t border-primary-200 pt-3 dark:border-primary-800"><h5 class="text-xs font-semibold">{{ t('governance.reliability.impactTitle') }}</h5><p v-if="affectedSiteCount(previews[row.local_group_id]) > 1" class="mt-2 text-xs text-amber-800 dark:text-amber-300">{{ t('governance.reliability.sharedGroupWarning', { count: affectedSiteCount(previews[row.local_group_id]) }) }}</p><ul class="mt-2 space-y-1 text-xs text-gray-600 dark:text-gray-300"><li v-for="binding in previews[row.local_group_id].bindings" :key="binding.binding_id" class="break-words">{{ t('governance.reliability.impactBinding', { site: binding.site_name, group: binding.remote_group_id, account: binding.account_id }) }}</li></ul><p v-if="!previews[row.local_group_id].bindings.length" class="mt-2 text-xs text-gray-500">{{ t('governance.reliability.noAffectedBindings') }}</p></div>
        </section>
        <details class="mt-3 text-xs text-gray-500"><summary class="cursor-pointer">{{ t('governance.reliability.technicalDetails') }}</summary><p class="mt-2 break-all">{{ currentStatus(row).protection_reason || '—' }}</p><ul class="mt-2 space-y-2"><li v-for="source in (previews[row.local_group_id]?.sources || row.sources)" :key="source.source_id" class="break-words"><span>{{ sourceLabel(source) }} · {{ source.eligible && source.comparable && !source.unknown ? formatGovernanceRate(source.cost) : t(source.eligible ? 'governance.reliability.sourceNotComparable' : 'governance.reliability.sourceExcluded') }}</span><span class="ml-2">{{ formatGovernanceTime(source.observed_at) }}</span></li></ul></details>
      </article>
    </section>

    <section v-if="pricingDraft" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
      <div><h3 class="font-semibold">{{ t('governance.pricingNotificationsTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.pricingNotificationsHint') }}</p></div>
      <div v-if="notificationConflict" role="alert" class="mt-4 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900 dark:border-amber-800 dark:bg-amber-900/10 dark:text-amber-200"><p>{{ t('governance.reliability.notificationConflict') }}</p><p class="mt-2 text-xs">{{ t('governance.reliability.reloadNotificationsHint') }}</p><button data-test="reload-notifications" class="btn btn-secondary mt-3 text-sm" type="button" :disabled="busy || props.disabled" @click="reloadNotifications">{{ t('governance.reliability.reloadNotifications') }}</button></div>
      <form data-test="notification-form" class="mt-4 space-y-4" @submit.prevent="saveNotifications"><fieldset :disabled="busy || props.disabled" class="space-y-4"><label class="governance-checkbox-label flex items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm font-medium dark:bg-dark-900"><input v-model="pricingDraft.notifications.enabled" data-test="notification-enabled" type="checkbox" class="governance-checkbox" />{{ t('governance.enablePricingNotifications') }}</label><div class="grid gap-3 sm:grid-cols-2"><label v-for="key in (['group_changes', 'rate_changes', 'pricing_changes', 'protection_changes'] as const)" :key="key" class="governance-checkbox-label flex items-center gap-2 text-sm"><input v-model="pricingDraft.notifications[key]" type="checkbox" class="governance-checkbox" />{{ t(`governance.notification_${key}`) }}</label></div><label class="block text-sm">{{ t('governance.notificationRecipients') }}<textarea :value="pricingDraft.notifications.recipients.join('\n')" data-test="notification-recipients" class="input mt-1 min-h-24 w-full" :placeholder="t('governance.emailRecipientsPlaceholder')" @input="pricingDraft.notifications.recipients = ($event.target as HTMLTextAreaElement).value.split(/[\n,]/)" /></label><button class="btn btn-primary" type="submit">{{ t('common.save') }}</button></fieldset></form>
    </section>
  </div>
</template>
