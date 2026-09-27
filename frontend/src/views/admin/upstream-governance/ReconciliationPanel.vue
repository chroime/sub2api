<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type Reconciliation, type ReconciliationPreview, type ReconciliationResult, type ReconciliationRow } from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'
const props = defineProps<{ siteId: number; refreshKey?: number | string; disabled?: boolean }>()
const emit = defineEmits<{ busy: [value: boolean]; applied: []; loaded: [value: Reconciliation] }>()
const { t } = useI18n()
const data = ref<Reconciliation | null>(null), preview = ref<ReconciliationPreview | null>(null), result = ref<ReconciliationResult | null>(null)
const selected = ref<number[]>([]), reviewed = ref<number[]>([]), loading = ref(false), busy = ref(false), error = ref('')
let generation = 0
const eligible = (row: ReconciliationRow) => row.action !== 'none' && (row.state === 'ready' || row.state === 'review')
const rows = computed(() => preview.value ? preview.value.rows.filter(row => reviewed.value.includes(row.binding_id)) : data.value?.rows.filter(row => row.action !== 'none' || row.state !== 'ready') || [])
const canApply = computed(() => !!preview.value && reviewed.value.length > 0 && reviewed.value.every(id => preview.value!.rows.some(row => row.binding_id === id && eligible(row))))
const stateKeys = { ready: 'reconcileStateReady', review: 'reconcileStateReview', conflict: 'reconcileStateConflict', unavailable: 'reconcileStateUnavailable' }
const actionKeys = { update: 'reconcileUpdate', pause: 'reconcilePause', restore: 'reconcileRestore', none: 'reconcileNoChange' }
const fieldKeys: Record<string, string> = { name: 'accountName', account_name: 'accountName', remote_group_name: 'remoteGroup', rate_multiplier: 'costRate', cost_multiplier: 'costRate', status: 'accountStatus', schedulable: 'accountSchedulable', rate_source: 'rateSource' }
const reasonKeys: Record<string, string> = { native_rate_sync_active: 'nativeRateSyncActive', rate_increase_exceeds_limit: 'rateIncreaseReview', rate_increase_review: 'rateIncreaseReview', account_changed: 'accountChangedReview', account_missing: 'accountUnavailable', local_account_missing: 'accountUnavailable', account_identity_changed: 'accountChangedReview', managed_rate_changed: 'accountChangedReview', managed_name_changed: 'accountChangedReview', pause_owner_changed: 'accountChangedReview', account_unavailable_for_restore: 'accountChangedReview', rate_owned_elsewhere: 'rateManagedElsewhere', catalog_incomplete: 'catalogIncomplete', catalog_stale: 'catalogStale', unknown_upstream_rate: 'unknownRate', invalid_upstream_rate: 'unknownRate', upstream_group_missing: 'upstreamGroupMissing', group_missing: 'upstreamGroupMissing', missing_confirmation_pending: 'missingConfirmationPending', missing_observed: 'missingConfirmationPending' }
function display(value: unknown, field: string) {
  if (value == null) return '—'
  if (field === 'rate_source' && (value === 'native' || value === 'governance')) return t(value === 'native' ? 'governance.rateSourceNative' : 'governance.rateSourceGovernance')
  if (field === 'schedulable' && typeof value === 'boolean') return t(value ? 'governance.schedulingEnabled' : 'governance.schedulingPaused')
  return typeof value === 'object' ? JSON.stringify(value) : String(value)
}
const reason = (row: ReconciliationRow) => row.reason ? t('governance.' + (reasonKeys[row.reason] || (row.state === 'conflict' ? 'accountChangedReview' : row.state === 'unavailable' ? 'accountUnavailable' : row.state === 'review' ? 'changeRequiresReview' : 'changeReadyHint'))) : ''
watch(busy, value => emit('busy', value), { flush: 'sync' })
async function load() {
  const request = ++generation, id = props.siteId
  data.value = null; preview.value = null; result.value = null; selected.value = []; reviewed.value = []; error.value = ''; loading.value = true; busy.value = false
  try { const value = await api.reconciliation(id); if (request === generation) { data.value = value; emit('loaded', value) } }
  catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) loading.value = false }
}
watch(() => [props.siteId, props.refreshKey], () => { void load() }, { immediate: true })
onUnmounted(() => { generation++; emit('busy', false) })
async function prepare() {
  if (props.disabled || busy.value || !selected.value.length || selected.value.length > 100 || !selected.value.every(id => data.value?.rows.some(row => row.binding_id === id && eligible(row)))) return
  const request = generation, ids = [...selected.value]
  busy.value = true; error.value = ''; result.value = null
  try { const value = await api.reconcilePreview(props.siteId); if (request === generation) { preview.value = value; reviewed.value = ids } }
  catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) busy.value = false }
}
async function apply() {
  if (props.disabled || busy.value || !preview.value || !canApply.value) return
  if (Date.parse(preview.value.expires_at) <= Date.now()) { preview.value = null; error.value = t('governance.stale'); return }
  const request = generation
  busy.value = true; error.value = ''
  try {
    const value = await api.applyReconciliation(props.siteId, preview.value.id, [...reviewed.value])
    if (request !== generation) return
    result.value = value
    emit('applied')
    if (reviewed.value.every(id => value.items.some(item => item.binding_id === id && item.status === 'applied'))) {
      preview.value = null; selected.value = []; reviewed.value = []; data.value = null; loading.value = true
      const refreshed = await api.reconciliation(props.siteId)
      if (request === generation) { data.value = refreshed; emit('loaded', refreshed) }
    }
  } catch (e) { if (request === generation) { error.value = t(errorKey(e)); if ((e as { status?: number }).status === 409) preview.value = null } }
  finally { if (request === generation) { busy.value = false; loading.value = false } }
}
</script>
<template>
  <section class="min-w-0 space-y-4" data-test="reconciliation-panel">
    <div class="flex flex-wrap items-start gap-3"><div class="min-w-0 flex-1"><h3 class="font-semibold">{{ t(preview ? 'governance.reconcileFrozen' : 'governance.pendingChanges') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.reconciliationHint') }}</p></div><button type="button" class="btn btn-secondary text-sm" :disabled="busy || loading || disabled" @click="load">{{ t('common.refresh') }}</button></div>
    <p v-if="error" role="alert" class="rounded-lg bg-red-50 p-3 text-sm text-red-600 dark:bg-red-900/10">{{ error }}</p>
    <p v-if="loading" role="status" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</p>
    <div v-else-if="data && !rows.length" class="rounded-xl border border-dashed border-primary-200 bg-primary-50/30 p-6 dark:border-primary-800 dark:bg-primary-900/10"><p class="font-medium text-primary-800 dark:text-primary-300">{{ t('governance.noPendingChanges') }}</p><p class="mt-1 text-sm text-gray-500">{{ t('governance.noPendingChangesHint') }}</p></div>
    <p v-if="data?.observed_at && !loading" class="text-xs text-gray-500">{{ t('governance.snapshot') }} {{ formatGovernanceTime(data.observed_at) }}</p>
    <p v-if="preview" class="text-sm text-amber-700 dark:text-amber-400">{{ t('governance.expires') }} {{ formatGovernanceTime(preview.expires_at) }}</p>
    <article v-for="row in rows" :key="row.binding_id" class="min-w-0 rounded-xl border border-gray-200 p-4 dark:border-dark-600">
      <div class="flex items-start gap-3"><input v-if="!preview" v-model="selected" data-test="reconcile-select" type="checkbox" :value="row.binding_id" :disabled="busy || disabled || !eligible(row) || (selected.length >= 100 && !selected.includes(row.binding_id))" :aria-label="row.remote_group_name || row.account_name" class="mt-1 h-4 w-4 shrink-0" /><div class="min-w-0 flex-1"><div class="flex flex-wrap items-center gap-2"><h4 class="break-all text-sm font-medium">{{ row.remote_group_name || row.remote_group_id }}</h4><span class="rounded px-2 py-0.5 text-xs" :class="row.state === 'ready' ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'bg-amber-50 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300'">{{ t('governance.' + stateKeys[row.state]) }}</span><span class="text-xs text-gray-500">{{ t('governance.' + actionKeys[row.action]) }}</span></div><p class="mt-1 break-all text-xs text-gray-500">{{ row.account_name }} · #{{ row.account_id }}</p><p v-if="reason(row)" class="mt-2 text-xs leading-relaxed text-gray-500">{{ reason(row) }}</p></div></div>
      <dl v-if="row.changes.length" class="mt-3 space-y-2 rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-900"><div v-for="change in row.changes" :key="change.field" class="grid min-w-0 gap-1 sm:grid-cols-[140px_minmax(0,1fr)]"><dt class="text-gray-500">{{ t('governance.' + (fieldKeys[change.field] || 'changeDetails')) }}</dt><dd class="min-w-0 break-all"><span class="text-gray-500">{{ display(change.before, change.field) }}</span><span aria-hidden="true" class="mx-2">→</span><span class="font-medium">{{ display(change.after, change.field) }}</span></dd></div></dl>
    </article>
    <p v-if="preview && !canApply" role="alert" class="text-sm text-amber-700">{{ t('governance.reconcileRefreshRequired') }}</p>
    <p v-if="selected.length >= 100 && !preview" role="status" class="text-xs text-amber-700">{{ t('governance.reconcileSelectionLimit') }}</p><div v-if="rows.length" class="flex flex-wrap gap-2"><button v-if="!preview" data-test="reconcile-preview" type="button" class="btn btn-primary" :disabled="busy || disabled || !selected.length" @click="prepare">{{ t('governance.previewSelectedChanges') }}</button><template v-else><button data-test="reconcile-apply" type="button" class="btn btn-primary" :disabled="busy || disabled || !canApply" @click="apply">{{ t(result ? 'governance.retry' : 'governance.applySelectedChanges') }}</button><button type="button" class="btn btn-secondary" :disabled="busy || disabled" @click="preview = null; result = null">{{ t('governance.backToChanges') }}</button></template></div>
    <div v-if="result" aria-live="polite" class="space-y-2 rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-900"><p class="font-medium">{{ t('governance.results') }}</p><p v-for="item in result.items" :key="item.binding_id">#{{ item.account_id }} · {{ t(item.status === 'applied' ? 'governance.success' : 'governance.failed') }}<span v-if="item.error"> · {{ t(errorKey({ reason: item.error })) }}</span></p></div>
  </section>
</template>
