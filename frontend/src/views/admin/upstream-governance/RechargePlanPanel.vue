<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type RechargePlan, type RechargePolicy } from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'
import { intervalValidationKey } from './interval'
const props = defineProps<{ siteId: number; disabled?: boolean }>()
const emit = defineEmits<{ busy: [value: boolean] }>()
const { t } = useI18n()
const current = ref<RechargePlan | null>(null), draft = ref<RechargePolicy | null>(null), amount = ref(''), budget = ref('')
const loading = ref(false), busy = ref(false), error = ref(''), saved = ref(false)
let generation = 0
const enabled = computed({ get: () => draft.value?.mode === 'plan_only', set: value => { if (draft.value) draft.value.mode = value ? 'plan_only' : 'disabled' } })
function minor(value: string): number | null {
  if (!/^\d+(?:\.\d{1,2})?$/.test(value.trim())) return null
  const [whole = '', fraction = ''] = value.trim().split('.')
  const result = Number(whole) * 100 + Number(fraction.padEnd(2, '0'))
  return Number.isSafeInteger(result) ? result : null
}
const reviewedPolicy = computed(() => draft.value ? { ...draft.value, amount_minor: minor(amount.value), daily_budget_minor: minor(budget.value) } : null)
const dirty = computed(() => !current.value || !reviewedPolicy.value || Object.entries(current.value.policy).some(([key, value]) => reviewedPolicy.value![key as keyof RechargePolicy] !== value))
function install(value: RechargePlan) { current.value = value; draft.value = { ...value.policy }; amount.value = (value.policy.amount_minor / 100).toFixed(2); budget.value = (value.policy.daily_budget_minor / 100).toFixed(2) }
async function load() {
  const request = ++generation
  loading.value = true; error.value = ''; saved.value = false; current.value = null; draft.value = null; busy.value = false
  try { const value = await api.rechargePlan(props.siteId); if (request === generation) install(value) }
  catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) loading.value = false }
}
watch(() => props.siteId, () => { void load() }, { immediate: true })
watch(busy, value => emit('busy', value), { flush: 'sync' })
onUnmounted(() => { generation++; emit('busy', false) })
async function save() {
  if (props.disabled || busy.value || !reviewedPolicy.value || !current.value) return
  const policy = reviewedPolicy.value
  if (policy.amount_minor == null || policy.daily_budget_minor == null) { error.value = t('governance.moneyPrecisionInvalid'); return }
  const intervalError = intervalValidationKey(policy.cooldown_minutes)
  if (intervalError) { error.value = t(intervalError); saved.value = false; return }
  if (policy.amount_minor <= 0 || policy.amount_minor > 1_000_000_000_000 || policy.daily_budget_minor < 0 || policy.daily_budget_minor > 1_000_000_000_000 || !Number.isFinite(policy.threshold) || policy.threshold < 0) { error.value = t('governance.invalid'); return }
  const request = generation
  busy.value = true; error.value = ''; saved.value = false
  try { const value = await api.saveRechargePlan(props.siteId, { version: current.value.version, policy: policy as RechargePolicy }); if (request === generation) { install(value); saved.value = true } }
  catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) busy.value = false }
}
async function evaluate() {
  if (props.disabled || busy.value || dirty.value) return
  const request = generation
  busy.value = true; error.value = ''
  try { const value = await api.evaluateRechargePlan(props.siteId); if (request === generation) install(value) }
  catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) busy.value = false }
}
const reasonKeys: Record<string, string> = { provider_unavailable: 'rechargeProviderUnavailable', disabled: 'rechargeDisabled', policy_disabled: 'rechargeDisabled', balance_unavailable: 'balanceUnavailable', balance_stale: 'balanceStale', snapshot_outdated: 'balanceSnapshotOutdated', collection_disabled: 'balanceCollectionDisabled', collection_failed: 'balanceCollectionFailed', session_unavailable: 'balanceSessionUnavailable', reauth_required: 'reauth', balance_unit_changed: 'balanceUnitChanged', threshold_not_reached: 'rechargeThresholdNotReached', above_threshold: 'rechargeThresholdNotReached', balance_above_threshold: 'rechargeThresholdNotReached', daily_budget_exceeded: 'rechargeBudgetExceeded', budget_exceeded: 'rechargeBudgetExceeded', cooldown: 'rechargeCooldownActive', cooldown_active: 'rechargeCooldownActive' }
const money = (value: number, currency: 'USD' | 'CNY') => new Intl.NumberFormat(undefined, { style: 'currency', currency }).format(value / 100)
</script>
<template>
  <section class="min-w-0 space-y-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
    <div class="flex flex-wrap items-start gap-3"><div class="min-w-0 flex-1"><h3 class="font-semibold">{{ t('governance.rechargePlanTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.rechargePlanHint') }}</p></div><span v-if="current" class="rounded-full bg-gray-100 px-2.5 py-1 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t(current.status === 'disabled' ? 'governance.rechargeDisabled' : 'governance.rechargeBlocked') }}</span></div>
    <p class="rounded-lg bg-amber-50 p-3 text-xs leading-relaxed text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">{{ t('governance.rechargeProviderUnavailable') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p><p v-if="saved" role="status" class="text-sm text-primary-700 dark:text-primary-300">{{ t('governance.rechargePlanSaved') }}</p><p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('common.loading') }}</p>
    <button v-if="!loading && !draft" type="button" class="btn btn-secondary" :disabled="disabled" @click="load">{{ t('common.refresh') }}</button>
    <form v-if="draft" class="space-y-4" @submit.prevent="save"><fieldset :disabled="busy || disabled" class="min-w-0 space-y-4"><label class="flex items-center gap-2 text-sm font-medium"><input v-model="enabled" data-test="recharge-enabled" type="checkbox" />{{ t('governance.enableRechargePlan') }}</label>
      <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">{{ t('governance.rechargeThreshold') }} · {{ draft.unit.toUpperCase() }}<input v-model.number="draft.threshold" data-test="recharge-threshold" type="number" min="0" step="any" required class="input mt-1 w-full" /></label><label class="text-sm">{{ t('governance.rechargeCurrency') }}<select v-model="draft.currency" data-test="recharge-currency" class="input mt-1 w-full"><option value="USD">USD</option><option value="CNY">CNY</option></select></label><label class="text-sm">{{ t('governance.rechargeAmount') }} · {{ draft.currency }}<input v-model="amount" data-test="recharge-amount" inputmode="decimal" type="text" required class="input mt-1 w-full" /></label><label class="text-sm">{{ t('governance.rechargeDailyBudget') }} · {{ draft.currency }}<input v-model="budget" data-test="recharge-budget" inputmode="decimal" type="text" required class="input mt-1 w-full" /></label><label class="text-sm">{{ t('governance.rechargeCooldown') }}<input v-model.number="draft.cooldown_minutes" data-test="recharge-cooldown" type="number" min="1" step="1" required class="input mt-1 w-full" /></label></div>
      <p class="text-xs leading-relaxed text-gray-500">{{ t('governance.rechargeUnitsHint') }}</p><div class="flex flex-wrap gap-2"><button data-test="save-recharge" class="btn btn-primary">{{ t('common.save') }}</button><button data-test="recharge-evaluate" type="button" class="btn btn-secondary" :disabled="dirty" @click="evaluate">{{ t('governance.evaluateRechargePlan') }}</button></div><p v-if="dirty" class="text-xs text-gray-500">{{ t('governance.saveBeforeEvaluate') }}</p>
    </fieldset></form>
    <div v-if="current?.evaluation" data-test="recharge-evaluation" class="space-y-2 rounded-lg bg-gray-50 p-4 text-sm dark:bg-dark-900"><p class="font-medium">{{ t('governance.rechargeEvaluation') }} · {{ t(current.evaluation.policy_matched ? 'governance.rechargePolicyMatched' : 'governance.rechargePolicyUnmatched') }}</p><p>{{ t('governance.rechargeAmount') }} {{ money(current.evaluation.amount_minor, current.evaluation.currency) }} · {{ t('governance.rechargeBudgetRemaining') }} {{ money(current.evaluation.daily_budget_remaining_minor, current.evaluation.currency) }}</p><p v-for="reason in current.evaluation.reasons" :key="reason" class="text-xs text-amber-700 dark:text-amber-400">{{ t('governance.' + (reasonKeys[reason] || 'rechargeEvaluationBlocked')) }}</p><p class="text-xs tabular-nums text-gray-500">{{ formatGovernanceTime(current.evaluation.evaluated_at) }}</p></div>
  </section>
</template>
