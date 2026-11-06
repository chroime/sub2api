<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import api, { type AutomationConfiguration, type AutomationPolicy } from '@/api/admin/upstream-governance'
import { errorKey } from './feedback'
import SettingsCard from './SettingsCard.vue'
const props = defineProps<{ siteId: number; configuration: AutomationConfiguration | null; disabled?: boolean }>()
const emit = defineEmits<{ saved: [value: AutomationConfiguration]; busy: [value: boolean]; reload: [] }>()
const { t } = useI18n()
const draft = ref<AutomationPolicy | null>(null), version = ref(0), busy = ref(false), error = ref(''), saved = ref(false)
const open = ref(false)
const options = ['sync_rate', 'sync_name', 'pause_missing', 'restore_returned'] as const
let generation = 0
watch(() => [props.siteId, props.configuration] as const, ([siteId], previous) => {
  generation++
  draft.value = props.configuration ? { ...props.configuration.policy } : null
  version.value = props.configuration?.version ?? 0
  error.value = ''
  if (siteId !== previous?.[0]) { saved.value = false; busy.value = false; open.value = false }
}, { immediate: true })
watch(busy, value => emit('busy', value), { flush: 'sync' })
onUnmounted(() => { generation++; emit('busy', false) })
async function save() {
  if (!draft.value || busy.value || props.disabled) return
  if (!Number.isInteger(draft.value.missing_confirmations) || draft.value.missing_confirmations < 2 || draft.value.missing_confirmations > 10 || !Number.isFinite(draft.value.max_rate_increase_percent) || draft.value.max_rate_increase_percent < 0 || draft.value.max_rate_increase_percent > 10000) {
    error.value = t('governance.invalid'); return
  }
  const request = generation
  busy.value = true; error.value = ''; saved.value = false
  try {
    const value = await api.saveAutomation(props.siteId, { version: version.value, policy: { ...draft.value } })
    if (request !== generation) return
    version.value = value.version
    emit('saved', value)
    saved.value = true
    open.value = false
  } catch (e) { if (request === generation) error.value = t(errorKey(e)) }
  finally { if (request === generation) busy.value = false }
}
</script>
<template>
  <SettingsCard v-model:open="open" test-id="automation-card" :title="t('governance.automationTitle')" :status="t(!configuration ? 'governance.automationUnknown' : configuration.policy.enabled ? 'governance.automationOn' : 'governance.automationOff')" :tone="configuration?.policy.enabled ? 'on' : 'off'" :disabled="disabled || busy || !draft" :busy="busy" :error="error">
    <p v-if="!draft" class="text-gray-500">{{ t('governance.automationUnavailable') }} <button type="button" class="text-primary-700 dark:text-primary-300" :disabled="disabled" @click="emit('reload')">{{ t('common.refresh') }}</button></p>
    <template v-else>
      <p v-if="saved" role="status" class="mb-2 text-primary-700 dark:text-primary-300">{{ t('governance.automationSaved') }}</p>
      <ul data-test="automation-summary" class="flex flex-wrap gap-1.5 text-xs">
        <li v-for="option in options" :key="option" class="rounded-full px-2 py-0.5" :class="configuration?.policy[option] ? 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-200' : 'bg-gray-50 text-gray-400 line-through dark:bg-dark-900'">{{ t(`governance.automation_${option}`) }}</li>
      </ul>
      <p class="mt-2 text-xs text-gray-500">{{ t('governance.settingsCard.automationLimits', { count: configuration?.policy.missing_confirmations ?? 0, percent: configuration?.policy.max_rate_increase_percent ?? 0 }) }}</p>
    </template>
    <template v-if="draft" #form>
      <p class="text-sm text-gray-500">{{ t('governance.automationHint') }}</p>
      <form class="space-y-4" @submit.prevent="save">
      <fieldset :disabled="busy || disabled" class="min-w-0 space-y-4">
        <label class="governance-checkbox-label flex items-center gap-3 rounded-lg bg-gray-50 p-3 text-sm font-medium dark:bg-dark-900"><input v-model="draft.enabled" data-test="automation-enabled" type="checkbox" class="governance-checkbox" />{{ t('governance.enableAutomation') }}</label>
        <div class="grid gap-3 sm:grid-cols-2">
          <label v-for="option in options" :key="option" class="governance-checkbox-label flex items-start gap-2 text-sm leading-6"><input v-model="draft[option]" type="checkbox" class="governance-checkbox mt-1" :data-test="'automation-' + option" /><span class="min-w-0">{{ t(`governance.automation_${option}`) }}</span></label>
        </div>
        <p class="text-xs leading-relaxed text-gray-500">{{ t('governance.automationOwnershipHint') }}</p>
        <div class="grid gap-4 sm:grid-cols-2"><label class="text-sm">{{ t('governance.missingConfirmations') }}<input v-model.number="draft.missing_confirmations" data-test="missing-confirmations" type="number" min="2" max="10" step="1" required class="input mt-1 w-full" /></label><label class="text-sm">{{ t('governance.maxRateIncrease') }}<input v-model.number="draft.max_rate_increase_percent" data-test="max-rate-increase" type="number" min="0" max="10000" step="any" required class="input mt-1 w-full" /></label></div>
        <p class="text-xs leading-relaxed text-gray-500">{{ t('governance.automationReviewHint') }}</p>
        <button data-test="save-automation" class="btn btn-primary">{{ t('common.save') }}</button>
      </fieldset>
      </form>
    </template>
  </SettingsCard>
</template>
