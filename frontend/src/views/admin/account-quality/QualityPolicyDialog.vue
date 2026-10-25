<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import api, { type Capability, type Policy, type Template } from '@/api/admin/account-quality'
import { newPolicy, qualityError } from './quality'

const props = defineProps<{ policy?: Policy | null; templates: Template[]; capabilities: Capability[] }>()
const emit = defineEmits<{ close: []; saved: [Policy]; busy: [boolean] }>()
const { t } = useI18n()
const draft = ref<Policy>(props.policy ? { ...props.policy, templates: [...props.policy.templates] } : newPolicy())
if (!props.policy && props.templates.length) draft.value.templates = props.templates.slice(0, Math.min(2, props.templates.length)).map(item => item.id)
const saving = ref(false), error = ref(''), unit = ref<'seconds' | 'minutes'>('minutes')
if (draft.value.interval_seconds % 60 !== 0) unit.value = 'seconds'
const intervalValue = computed({ get: () => draft.value.interval_seconds / (unit.value === 'minutes' ? 60 : 1), set: value => { draft.value.interval_seconds = Number(value) * (unit.value === 'minutes' ? 60 : 1) } })
const platforms = computed(() => [...new Set(['openai', 'anthropic', 'gemini', ...props.capabilities.map(item => item.platform), draft.value.platform])])
const supportedModes = computed(() => [...new Set(props.capabilities.filter(item => item.platform === draft.value.platform).map(item => item.api_mode).filter(Boolean))])
const unavailableSavedMode = computed(() => props.policy?.platform === draft.value.platform && props.policy.api_mode === draft.value.api_mode && !supportedModes.value.includes(draft.value.api_mode) ? draft.value.api_mode : '')
watch(supportedModes, modes => {
  if (!modes.includes(draft.value.api_mode) && !unavailableSavedMode.value) draft.value.api_mode = modes[0] || ''
}, { immediate: true })
watch(() => draft.value.platform, () => {
  if (!supportedModes.value.includes(draft.value.api_mode)) draft.value.api_mode = supportedModes.value[0] || ''
})
const automaticAvailable = computed(() => props.capabilities.some(item => item.platform === draft.value.platform && item.api_mode === draft.value.api_mode && item.automatic_control))
const limits = {
  max_output_tokens: { min: 64, max: 32768, label: 'outputTokens' },
  timeout_seconds: { min: 5, max: 600, label: 'timeout' },
  interval_seconds: { min: 10, max: 604800, label: 'interval' },
  confirm_seconds: { min: 10, max: 86400, label: 'confirmSeconds' },
  failure_threshold: { min: 1, max: 100, label: 'failureThreshold' },
  recovery_threshold: { min: 1, max: 100, label: 'recoveryThreshold' },
  cooldown_seconds: { min: 10, max: 604800, label: 'cooldownSeconds' },
  recovery_interval_seconds: { min: 10, max: 86400, label: 'recoveryInterval' },
  daily_request_limit: { min: 1, max: 10000, label: 'dailyLimit' },
} as const
const numericFields = Object.keys(limits) as (keyof typeof limits)[]
const scheduleFields = ['confirm_seconds', 'failure_threshold', 'recovery_threshold', 'cooldown_seconds', 'recovery_interval_seconds', 'daily_request_limit'] as const
async function save() {
  if (saving.value) return
  const name = draft.value.name.trim(), model = draft.value.model.trim()
  if (!name || new TextEncoder().encode(name).byteLength > 200 || !model || new TextEncoder().encode(model).byteLength > 250 || !draft.value.templates.length || draft.value.templates.length > 4 || new Set(draft.value.templates).size !== draft.value.templates.length || draft.value.templates.some(id => !props.templates.some(template => template.id === id)) || (draft.value.control_mode === 'auto_quarantine' && !automaticAvailable.value)) { error.value = t('accountQuality.invalidPolicy'); return }
  if (!supportedModes.value.includes(draft.value.api_mode)) { error.value = t('accountQuality.invalidProtocol'); return }
  const invalidField = numericFields.find(key => !Number.isSafeInteger(draft.value[key]) || draft.value[key] < limits[key].min || draft.value[key] > limits[key].max)
  if (invalidField) { error.value = t('accountQuality.invalidPolicyRange', { field: t(`accountQuality.${limits[invalidField].label}`), min: limits[invalidField].min, max: limits[invalidField].max }); return }
  saving.value = true; error.value = ''; emit('busy', true)
  try { const value = await api.savePolicy({ ...draft.value, name, model }); emit('saved', value) }
  catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { saving.value = false; emit('busy', false) }
}
</script>

<template>
  <BaseDialog :show="true" :title="t(policy ? 'accountQuality.editPolicy' : 'accountQuality.createPolicy')" width="wide" :close-on-escape="!saving" :show-close-button="!saving" @close="emit('close')">
    <form id="account-quality-policy-form" class="space-y-5" @submit.prevent="save">
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <div class="flex flex-wrap items-end justify-between gap-4">
        <label class="min-w-48 flex-1 text-sm"><span class="mb-1 block">{{ t('accountQuality.name') }}</span><input v-model="draft.name" data-test="quality-policy-name" class="input w-full" maxlength="200" required /></label>
        <label class="flex min-h-11 items-center gap-2 text-sm"><input v-model="draft.enabled" data-test="quality-policy-enabled" type="checkbox" class="rounded" />{{ t('accountQuality.enabled') }}</label>
      </div>
      <fieldset class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
        <legend class="pr-2 text-sm font-semibold">{{ t('accountQuality.modelConfig') }}</legend>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.platform') }}</span><select v-model="draft.platform" data-test="quality-policy-platform" class="input w-full"><option v-for="platform in platforms" :key="platform" :value="platform">{{ platform }}</option></select></label>
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.protocol') }}</span><select v-model="draft.api_mode" data-test="quality-policy-protocol" class="input w-full" required><option v-if="unavailableSavedMode" :value="unavailableSavedMode" disabled>{{ unavailableSavedMode }} · {{ t('accountQuality.protocolUnavailable') }}</option><option v-else-if="!supportedModes.length" value="" disabled>{{ t('accountQuality.noProtocols') }}</option><option v-for="mode in supportedModes" :key="mode" :value="mode">{{ mode }}</option></select></label>
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.model') }}</span><input v-model="draft.model" data-test="quality-policy-model" class="input w-full" maxlength="250" required /></label>
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.effort') }}</span><select v-model="draft.effort" data-test="quality-policy-effort" class="input w-full"><option value="">{{ t('accountQuality.effortUnspecified') }}</option><option v-for="effort in ['low', 'medium', 'high']" :key="effort" :value="effort">{{ t(`accountQuality.${effort}`) }}</option></select></label>
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.outputTokens') }}</span><input v-model.number="draft.max_output_tokens" name="max_output_tokens" class="input w-full" type="number" :min="limits.max_output_tokens.min" :max="limits.max_output_tokens.max" step="1" required /></label>
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.timeout') }}</span><input v-model.number="draft.timeout_seconds" name="timeout_seconds" class="input w-full" type="number" :min="limits.timeout_seconds.min" :max="limits.timeout_seconds.max" step="1" required /></label>
        </div>
        <div><p class="mb-2 text-sm">{{ t('accountQuality.templates') }}</p><div class="flex flex-wrap gap-3"><label v-for="template in templates" :key="template.id" class="flex items-start gap-2 rounded-lg border border-gray-200 p-3 text-sm dark:border-dark-600"><input v-model="draft.templates" :value="template.id" type="checkbox" class="mt-0.5 rounded" /><span>{{ template.name }} <span class="text-xs text-gray-400">{{ template.version }}</span><span v-if="template.description" class="mt-1 block max-w-xs text-xs text-gray-500">{{ template.description }}</span></span></label></div></div>
      </fieldset>
      <fieldset class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700">
        <legend class="pr-2 text-sm font-semibold">{{ t('accountQuality.cadence') }}</legend>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.interval') }}</span><span class="flex gap-2"><input v-model.number="intervalValue" name="interval_seconds" class="input w-full min-w-0" type="number" :min="limits.interval_seconds.min / (unit === 'minutes' ? 60 : 1)" :max="limits.interval_seconds.max / (unit === 'minutes' ? 60 : 1)" :step="unit === 'minutes' ? 'any' : '1'" required /><select v-model="unit" data-test="quality-policy-interval-unit" class="input"><option value="seconds">{{ t('accountQuality.seconds') }}</option><option value="minutes">{{ t('accountQuality.minutes') }}</option></select></span></label>
          <label v-for="field in scheduleFields" :key="field" class="text-sm"><span class="mb-1 block">{{ t(`accountQuality.${limits[field].label}`) }}</span><input v-model.number="draft[field]" :name="field" class="input w-full" type="number" :min="limits[field].min" :max="limits[field].max" step="1" required /></label>
        </div>
      </fieldset>
      <div class="grid gap-3 border-t border-gray-100 pt-4 dark:border-dark-700 sm:grid-cols-2">
        <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.controlMode') }}</span><select v-model="draft.control_mode" data-test="quality-policy-mode" class="input w-full"><option v-for="mode in ['record_only', 'record_notify', 'auto_quarantine']" :key="mode" :value="mode" :disabled="mode === 'auto_quarantine' && !automaticAvailable">{{ t(`accountQuality.${mode}`) }}</option></select></label>
        <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.scope') }}</span><select v-model="draft.isolation_scope" class="input w-full"><option value="model">{{ t('accountQuality.scope_model') }}</option><option value="account">{{ t('accountQuality.scope_account') }}</option></select></label>
        <p class="text-xs leading-relaxed text-gray-500 sm:col-span-2">{{ t('accountQuality.modeHint') }}</p>
      </div>
    </form>
    <template #footer><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('accountQuality.cancel') }}</button><button type="submit" form="account-quality-policy-form" data-test="quality-policy-save" class="btn btn-primary" :disabled="saving">{{ t('accountQuality.save') }}</button></template>
  </BaseDialog>
</template>
