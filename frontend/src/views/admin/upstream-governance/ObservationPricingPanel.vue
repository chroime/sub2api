<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import api, {
  type ObservationPolicy,
  type PricingMode,
  type PricingPoliciesConfiguration,
  type PricingPolicy,
} from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import { formatGovernanceTime } from './format'

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
const previewRows = ref(new Set<number>())
const loading = ref(true)
const savingObservation = ref(false)
const savingPricing = ref<number | null>(null)
const savingNotifications = ref(false)
const error = ref('')
const saved = ref('')
let generation = 0

const busy = computed(() => loading.value || savingObservation.value || savingPricing.value !== null || savingNotifications.value)
const pricingRows = computed(() => pricingDraft.value?.policies ?? [])
const modeOptions = computed(() => [
  { value: 'keep_margin', label: t('governance.pricingModeKeepMargin') },
  { value: 'target_margin', label: t('governance.pricingModeTargetMargin') },
])

function resetDrafts() {
  observationDraft.value = observation.value ? { ...observation.value.policy } : null
  pricingDraft.value = pricing.value ? {
    ...pricing.value,
    policies: pricing.value.policies.map(row => ({ ...row, sources: row.sources.map(source => ({ ...source })) })),
    notifications: { ...pricing.value.notifications, recipients: [...pricing.value.notifications.recipients] },
  } : null
  previewRows.value = new Set()
}

async function load() {
  const request = ++generation
  loading.value = true
  error.value = ''
  saved.value = ''
  emit('busy', true)
  try {
    const [observationValue, pricingValue] = await Promise.all([api.observationPolicy(props.siteId), api.pricingPolicies(props.siteId)])
    if (request !== generation) return
    observation.value = observationValue
    pricing.value = pricingValue
    resetDrafts()
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) {
      loading.value = false
      emit('busy', false)
    }
  }
}

function validSeconds(value: unknown): boolean {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

async function saveObservation() {
  if (!observationDraft.value || !observation.value || busy.value || props.disabled) return
  if (!validSeconds(observationDraft.value.fast_interval_seconds) || !validSeconds(observationDraft.value.full_interval_seconds) || !validSeconds(observationDraft.value.decrease_stability_seconds ?? 60)) {
    error.value = t('governance.intervalPositiveInteger')
    return
  }
  const request = generation
  savingObservation.value = true
  error.value = ''
  saved.value = ''
  emit('busy', true)
  try {
    const value = await api.saveObservationPolicy(props.siteId, { version: observation.value.version, policy: { ...observationDraft.value } })
    if (request !== generation) return
    observation.value = value
    resetDrafts()
    saved.value = 'governance.observationPolicySaved'
    emit('observation-saved', value)
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) {
      savingObservation.value = false
      emit('busy', false)
    }
  }
}

function policyFor(id: number): PricingPolicy | undefined {
  return pricingDraft.value?.policies.find(row => row.local_group_id === id)
}

function togglePreview(id: number) {
  if (props.disabled || busy.value) return
  const next = new Set(previewRows.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  previewRows.value = next
}

async function savePricing(id: number) {
  if (!pricingDraft.value || !pricing.value || busy.value || props.disabled) return
  const row = policyFor(id)
  if (!row) return
  const margin = Number(row.min_margin), buffer = Number(row.safety_buffer)
  if (![margin, buffer].every(Number.isFinite) || margin < 0 || buffer < 0 || margin + buffer >= 1) {
    error.value = t('governance.pricingMarginInvalid')
    return
  }
  if (![row.max_increase_percent, row.decrease_stability_seconds].every(validSeconds) && row.decrease_stability_seconds !== 0) {
    error.value = t('governance.intervalPositiveInteger')
    return
  }
  const request = generation
  savingPricing.value = id
  error.value = ''
  saved.value = ''
  emit('busy', true)
  try {
    const value = await api.savePricingPolicies(props.siteId, {
      ...pricingDraft.value,
      policies: pricingDraft.value.policies.map(item => ({ ...item, sources: item.sources.map(source => ({ ...source })) })),
    })
    if (request !== generation) return
    pricing.value = value
    resetDrafts()
    previewRows.value = new Set()
    saved.value = 'governance.pricingPolicySaved'
    emit('pricing-saved', value)
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) {
      savingPricing.value = null
      emit('busy', false)
    }
  }
}

function recipients(value: string) {
  return value.split(/[\n,]/).map(item => item.trim()).filter(Boolean).filter((item, index, list) => list.indexOf(item) === index)
}

async function saveNotifications() {
  if (!pricingDraft.value || !pricing.value || busy.value || props.disabled) return
  const request = generation
  savingNotifications.value = true
  error.value = ''
  saved.value = ''
  emit('busy', true)
  try {
    const value = await api.savePricingPolicies(props.siteId, {
      ...pricingDraft.value,
      notifications: {
        ...pricingDraft.value.notifications,
        recipients: recipients(pricingDraft.value.notifications.recipients.join('\n')),
      },
    })
    if (request !== generation) return
    pricing.value = value
    resetDrafts()
    saved.value = 'governance.notificationPolicySaved'
    emit('pricing-saved', value)
  } catch (e) {
    if (request === generation) error.value = t(errorKey(e))
  } finally {
    if (request === generation) {
      savingNotifications.value = false
      emit('busy', false)
    }
  }
}

function updatePolicy<K extends keyof PricingPolicy>(row: PricingPolicy, key: K, value: PricingPolicy[K]) {
  row[key] = value
}

onMounted(load)
onUnmounted(() => { generation++; emit('busy', false) })
</script>

<template>
  <div class="space-y-5" data-test="observation-pricing-panel">
    <p v-if="error" role="alert" class="rounded-xl border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-900/10 dark:text-red-300">{{ error }}</p>
    <p v-if="saved" role="status" class="rounded-xl border border-primary-100 bg-primary-50 p-3 text-sm text-primary-800 dark:border-primary-900/40 dark:bg-primary-900/10 dark:text-primary-300">{{ t(saved) }}</p>
    <p v-if="loading" role="status" class="rounded-xl border border-dashed border-gray-200 p-5 text-sm text-gray-500 dark:border-dark-600">{{ t('common.loading') }}</p>

    <section v-if="observationDraft" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
      <div class="flex flex-wrap items-start justify-between gap-3"><div><h3 class="font-semibold">{{ t('governance.observationPolicyTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.observationPolicyHint') }}</p></div><span class="rounded-full px-2.5 py-1 text-xs font-medium" :class="observationDraft.enabled ? 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ t(observationDraft.enabled ? 'governance.observationOn' : 'governance.observationOff') }}</span></div>
      <form data-test="observation-form" class="mt-5 space-y-4" @submit.prevent="saveObservation">
        <fieldset :disabled="busy || props.disabled" class="space-y-4">
          <label class="governance-checkbox-label flex items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm font-medium dark:bg-dark-900"><input v-model="observationDraft.enabled" data-test="observation-enabled" type="checkbox" class="governance-checkbox" />{{ t('governance.enableSecondObservation') }}</label>
          <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">{{ t('governance.fastObservationInterval') }}<div class="mt-1 flex items-center gap-2"><input v-model.number="observationDraft.fast_interval_seconds" data-test="fast-interval-seconds" class="input w-full" type="number" min="1" step="1" required /><span class="text-xs text-gray-500">{{ t('governance.seconds') }}</span></div></label><label class="text-sm">{{ t('governance.fullCollectionInterval') }}<div class="mt-1 flex items-center gap-2"><input v-model.number="observationDraft.full_interval_seconds" data-test="full-interval-seconds" class="input w-full" type="number" min="1" step="1" required /><span class="text-xs text-gray-500">{{ t('governance.seconds') }}</span></div></label></div>
          <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">{{ t('governance.decreaseStability') }}<div class="mt-1 flex items-center gap-2"><input v-model.number="observationDraft.decrease_stability_seconds" data-test="decrease-stability-seconds" class="input w-full" type="number" min="0" step="1" required /><span class="text-xs text-gray-500">{{ t('governance.seconds') }}</span></div></label><label class="text-sm">{{ t('governance.maxRateIncrease') }}<div class="mt-1 flex items-center gap-2"><input v-model.number="observationDraft.max_rate_increase_percent" data-test="max-rate-increase-percent" class="input w-full" type="number" min="0" step="any" required /><span class="text-xs text-gray-500">%</span></div></label></div>
          <div class="grid gap-3 rounded-lg bg-gray-50 p-3 text-xs text-gray-500 dark:bg-dark-900 sm:grid-cols-3"><p>{{ t('governance.lastFastObservation') }}<strong class="mt-1 block font-medium text-gray-700 dark:text-gray-200">{{ formatGovernanceTime(observation?.status?.last_fast_observed_at) }}</strong></p><p>{{ t('governance.nextFastObservation') }}<strong class="mt-1 block font-medium text-gray-700 dark:text-gray-200">{{ formatGovernanceTime(observation?.status?.next_fast_observation_at) }}</strong></p><p>{{ t('governance.observationStatus') }}<strong class="mt-1 block font-medium text-gray-700 dark:text-gray-200">{{ observation?.status?.fast_observe_status || t('governance.unknown') }}</strong></p></div>
          <button data-test="save-observation" class="btn btn-primary" type="submit">{{ t('common.save') }}</button>
        </fieldset>
      </form>
    </section>

    <section v-if="pricingDraft" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
      <div class="flex flex-wrap items-start justify-between gap-3"><div><h3 class="font-semibold">{{ t('governance.pricingProtectionTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.pricingProtectionHint') }}</p></div><Icon name="shield" size="md" class="text-primary-600" /></div>
      <p v-if="!pricingRows.length" class="mt-4 rounded-lg border border-dashed border-gray-200 p-4 text-sm text-gray-500 dark:border-dark-600">{{ t('governance.noPricingPolicies') }}</p>
      <div v-for="row in pricingRows" :key="row.local_group_id" class="mt-4 rounded-xl border border-gray-200 p-4 dark:border-dark-600" :data-test="`pricing-row-${row.local_group_id}`">
        <div class="flex flex-wrap items-start gap-3"><div class="min-w-0 flex-1"><p class="break-words font-medium">{{ row.local_group_name }}</p><p class="mt-1 text-xs text-gray-500">{{ t('governance.pricingSources') }}：{{ row.sources.map(source => source.source_name || source.source_id).join('、') || t('governance.unknown') }}</p></div><span class="rounded-full px-2 py-1 text-xs" :class="row.protected ? 'bg-red-50 text-red-700 dark:bg-red-900/20 dark:text-red-300' : row.manual_owner ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-primary-50 text-primary-700 dark:bg-primary-900/20 dark:text-primary-300'">{{ t(row.protected ? 'governance.pricingProtected' : row.manual_owner ? 'governance.pricingManual' : 'governance.pricingManaged') }}</span></div>
        <div class="mt-4 grid gap-3 text-sm sm:grid-cols-4"><p>{{ t('governance.currentCost') }}<strong class="mt-1 block font-medium">{{ row.current_cost ?? '—' }}</strong></p><p>{{ t('governance.currentSale') }}<strong class="mt-1 block font-medium">{{ row.current_sale ?? '—' }}</strong></p><p>{{ t('governance.targetSale') }}<strong class="mt-1 block font-medium text-primary-700 dark:text-primary-300">{{ row.target_sale ?? '—' }}</strong></p><p>{{ t('governance.effectiveMargin') }}<strong class="mt-1 block font-medium">{{ row.current_cost != null && row.current_sale ? `${((row.current_sale - row.current_cost) / row.current_sale * 100).toFixed(2)}%` : '—' }}</strong></p></div>
        <div class="mt-4 grid gap-4 sm:grid-cols-2 lg:grid-cols-4"><label class="text-sm">{{ t('governance.pricingMode') }}<Select class="mt-1" :model-value="row.mode" :options="modeOptions" :disabled="busy || props.disabled" :aria-label="t('governance.pricingMode')" @update:model-value="value => updatePolicy(row, 'mode', value as PricingMode)" /></label><label class="text-sm">{{ t('governance.minimumMargin') }}<input :value="row.min_margin" data-test="pricing-min-margin" class="input mt-1 w-full" type="number" min="0" max="0.9999" step="0.0001" @input="updatePolicy(row, 'min_margin', Number(($event.target as HTMLInputElement).value))" /></label><label class="text-sm">{{ t('governance.safetyBuffer') }}<input :value="row.safety_buffer" data-test="pricing-safety-buffer" class="input mt-1 w-full" type="number" min="0" max="0.9999" step="0.0001" @input="updatePolicy(row, 'safety_buffer', Number(($event.target as HTMLInputElement).value))" /></label><label class="text-sm">{{ t('governance.maxRateIncrease') }}<input :value="row.max_increase_percent" class="input mt-1 w-full" type="number" min="0" step="any" @input="updatePolicy(row, 'max_increase_percent', Number(($event.target as HTMLInputElement).value))" /></label></div>
        <div class="mt-4 flex flex-wrap gap-2"><button type="button" class="btn btn-secondary text-sm" :disabled="busy || props.disabled" :data-test="`pricing-preview-${row.local_group_id}`" @click="togglePreview(row.local_group_id)">{{ t('governance.pricingPreview') }}</button><button v-if="previewRows.has(row.local_group_id)" type="button" class="btn btn-primary text-sm" :disabled="busy || props.disabled" :data-test="`pricing-apply-${row.local_group_id}`" @click="savePricing(row.local_group_id)">{{ t('governance.pricingApply') }}</button></div>
        <p v-if="previewRows.has(row.local_group_id)" class="mt-3 rounded-lg bg-primary-50 p-3 text-xs text-primary-800 dark:bg-primary-900/20 dark:text-primary-300">{{ t('governance.pricingPreviewHint', { before: row.current_sale ?? '—', after: row.target_sale ?? '—' }) }}</p><p v-if="row.protected && row.protection_reason" class="mt-3 rounded-lg bg-red-50 p-3 text-xs text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ row.protection_reason }}</p>
      </div>
    </section>

    <section v-if="pricingDraft" class="rounded-xl border border-gray-200 p-4 dark:border-dark-600 sm:p-5">
      <div><h3 class="font-semibold">{{ t('governance.pricingNotificationsTitle') }}</h3><p class="mt-1 text-sm text-gray-500">{{ t('governance.pricingNotificationsHint') }}</p></div>
      <form data-test="notification-form" class="mt-4 space-y-4" @submit.prevent="saveNotifications"><fieldset :disabled="busy || props.disabled" class="space-y-4"><label class="governance-checkbox-label flex items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm font-medium dark:bg-dark-900"><input v-model="pricingDraft.notifications.enabled" data-test="notification-enabled" type="checkbox" class="governance-checkbox" />{{ t('governance.enablePricingNotifications') }}</label><div class="grid gap-3 sm:grid-cols-2"><label v-for="key in (['group_changes', 'rate_changes', 'pricing_changes', 'protection_changes'] as const)" :key="key" class="governance-checkbox-label flex items-center gap-2 text-sm"><input v-model="pricingDraft.notifications[key]" type="checkbox" class="governance-checkbox" />{{ t(`governance.notification_${key}`) }}</label></div><label class="text-sm">{{ t('governance.notificationRecipients') }}<textarea :value="pricingDraft.notifications.recipients.join('\n')" data-test="notification-recipients" class="input mt-1 min-h-24 w-full" :placeholder="t('governance.emailRecipientsPlaceholder')" @input="pricingDraft.notifications.recipients = ($event.target as HTMLTextAreaElement).value.split(/[\n,]/)" /></label><button class="btn btn-primary" type="submit">{{ t('common.save') }}</button></fieldset></form>
    </section>
  </div>
</template>
