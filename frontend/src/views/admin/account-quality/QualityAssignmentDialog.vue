<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import QualityAccountSelector from './QualityAccountSelector.vue'
import api, { type BindingOverrides, type Policy } from '@/api/admin/account-quality'
import accountsAPI from '@/api/admin/accounts'
import groupsAPI from '@/api/admin/groups'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import type { AdminGroup } from '@/types'
import { qualityError } from './quality'

const props = defineProps<{ policies: Policy[]; accountIds?: number[]; policyId?: number }>()
const emit = defineEmits<{ close: []; saved: [number]; busy: [boolean] }>()
const { t } = useI18n()
const normalizeIds = (values: number[]) => [...new Set(values.filter(id => Number.isSafeInteger(id) && id > 0))]
const chosenIds = ref(normalizeIds(props.accountIds || []))
const policyId = ref(props.policyId || props.policies[0]?.id || 0), mode = ref<'single' | 'selected' | 'group' | 'platform'>(chosenIds.value.length <= 1 ? 'single' : 'selected'), group = ref(''), platform = ref('')
const groups = ref<AdminGroup[]>([]), saving = ref(false), error = ref(''), custom = ref(false)
const selectedPolicy = computed(() => props.policies.find(policy => policy.id === policyId.value))
const policyOptions = computed<SelectOption[]>(() => props.policies.map(policy => ({ value: policy.id, label: `${policy.name} · ${policy.model} · ${policy.platform}` })))
const directSelection = computed(() => mode.value === 'single' || mode.value === 'selected')
const selection = ref({ valid: false, pending: false, platform: '', accountIds: [] as number[] })
const selectionReady = computed(() => selection.value.valid && !selection.value.pending && selection.value.platform === selectedPolicy.value?.platform && selection.value.accountIds.join(',') === chosenIds.value.join(',') && (mode.value !== 'single' || chosenIds.value.length === 1))
const platformOptions = computed(() => CONCRETE_PLATFORM_OPTIONS.filter(item => item.value === selectedPolicy.value?.platform))
const overrides = ref<{ interval_seconds: number | ''; failure_threshold: number | ''; recovery_threshold: number | ''; isolation_scope: '' | 'model' | 'account' }>({ interval_seconds: '', failure_threshold: '', recovery_threshold: '', isolation_scope: '' })
onMounted(async () => { try { groups.value = await groupsAPI.getAll() } catch { /* Direct account selection remains available. */ } })
watch(() => selectedPolicy.value?.platform, value => { platform.value = value || ''; error.value = '' }, { immediate: true })
watch(mode, value => { if (value === 'single' && chosenIds.value.length > 1) chosenIds.value = chosenIds.value.slice(0, 1); error.value = '' })
watch(() => props.accountIds, value => { chosenIds.value = normalizeIds(value || []); mode.value = chosenIds.value.length <= 1 ? 'single' : 'selected' }, { deep: true })
async function save() {
  if (saving.value) return
  const policy = selectedPolicy.value
  if (!policy) { error.value = t('accountQuality.targetRequired'); return }
  if (directSelection.value && (!chosenIds.value.length || !selectionReady.value)) { error.value = t(chosenIds.value.length ? 'accountQuality.accountSelectionInvalid' : 'accountQuality.targetRequired'); return }
  error.value = ''; saving.value = true; emit('busy', true)
  try {
    let targets: number[] = []
    if (directSelection.value) {
      targets = [...chosenIds.value]
    } else {
      if (mode.value === 'group' && !group.value || mode.value === 'platform' && !platform.value) { error.value = t('accountQuality.targetRequired'); return }
      let page = 1
      while (true) {
        if (mode.value === 'platform' && policy.platform !== platform.value) { error.value = t('accountQuality.platformMismatch'); return }
        const result = await accountsAPI.list(page, 100, { lite: '1', include_scheduler_score: '0', platform: policy.platform, ...(mode.value === 'group' ? { group: group.value } : {}) })
        targets.push(...result.items.map(item => item.id))
        if (page * result.page_size >= result.total || !result.items.length) break
        page++
      }
    }
    targets = [...new Set(targets)]
    if (!policyId.value || !targets.length) { error.value = t('accountQuality.targetRequired'); return }
    if (targets.length > 500) { error.value = t('accountQuality.assignmentLimit'); return }
    const effectiveOverrides: BindingOverrides = {}
    if (custom.value) {
      for (const key of ['interval_seconds', 'failure_threshold', 'recovery_threshold'] as const) {
        const value = overrides.value[key]
        if (value !== '') { if (!Number.isInteger(value) || value <= 0) { error.value = t('accountQuality.invalidPolicy'); return } effectiveOverrides[key] = value }
      }
      if (overrides.value.isolation_scope) effectiveOverrides.isolation_scope = overrides.value.isolation_scope
    }
    const value = await api.assign({ policy_id: policy.id, account_ids: targets, source: directSelection.value ? 'selected_accounts' : `${mode.value}:${mode.value === 'group' ? group.value : platform.value}`, ...(custom.value ? { overrides: effectiveOverrides } : {}) })
    emit('saved', value.length)
  } catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { saving.value = false; emit('busy', false) }
}
</script>

<template>
  <BaseDialog :show="true" :title="t('accountQuality.assignment')" width="normal" :close-on-escape="!saving" :show-close-button="!saving" @close="emit('close')">
    <form id="quality-assignment-form" class="space-y-4" @submit.prevent="save">
      <p class="text-sm leading-relaxed text-gray-500">{{ t('accountQuality.assignmentHint') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <div class="text-sm"><label for="quality-assignment-policy" class="mb-1 block">{{ t('accountQuality.policy') }}</label><Select id="quality-assignment-policy" data-test="quality-assignment-policy" :model-value="policyId || null" :options="policyOptions" searchable :disabled="saving || !policies.length" :aria-label="t('accountQuality.policy')" :placeholder="t('accountQuality.choosePolicy')" :search-placeholder="t('accountQuality.searchPolicy')" @update:model-value="policyId = typeof $event === 'number' ? $event : 0" /></div>
      <p class="text-xs text-gray-500">{{ t('accountQuality.assignmentPlatformHint') }}</p>
      <label class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.assignmentMode') }}</span><select v-model="mode" data-test="quality-assignment-mode" :disabled="saving" class="input w-full"><option value="single">{{ t('accountQuality.singleAccount') }}</option><option value="selected">{{ t('accountQuality.selectedAccounts') }}</option><option value="group">{{ t('accountQuality.group') }}</option><option value="platform">{{ t('accountQuality.platform') }}</option></select></label>
      <div v-if="directSelection" class="text-sm"><p class="mb-1">{{ t(mode === 'single' ? 'accountQuality.singleAccount' : 'accountQuality.selectedAccounts') }}</p><QualityAccountSelector v-model="chosenIds" :platform="selectedPolicy?.platform || ''" :multiple="mode === 'selected'" :disabled="saving" @validation="selection = $event" /></div>
      <label v-else-if="mode === 'group'" class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.group') }}</span><select v-model="group" data-test="quality-assignment-group" :disabled="saving" class="input w-full" required><option value="" disabled>—</option><option v-for="item in groups" :key="item.id" :value="String(item.id)">{{ item.name }}</option></select></label>
      <label v-else class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.platform') }}</span><select v-model="platform" data-test="quality-assignment-platform" :disabled="saving" class="input w-full" required><option value="" disabled>—</option><option v-for="item in platformOptions" :key="item.value" :value="item.value">{{ item.label }}</option></select></label>
      <label class="flex items-center gap-2 text-sm"><input v-model="custom" :disabled="saving" type="checkbox" class="rounded" />{{ t('accountQuality.overrides') }}</label>
      <div v-if="custom" class="grid gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-900 sm:grid-cols-2">
        <p class="text-xs text-gray-500 sm:col-span-2">{{ t('accountQuality.overrideHint') }}</p>
        <label v-for="field in [{ key: 'interval_seconds', label: 'interval' }, { key: 'failure_threshold', label: 'failureThreshold' }, { key: 'recovery_threshold', label: 'recoveryThreshold' }] as const" :key="field.key" class="text-sm"><span class="mb-1 block">{{ t(`accountQuality.${field.label}`) }}</span><input v-model.number="overrides[field.key]" class="input w-full" type="number" min="1" step="1" /></label>
        <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.scope') }}</span><select v-model="overrides.isolation_scope" class="input w-full"><option value="">{{ t('accountQuality.policyDefaults') }}</option><option value="model">{{ t('accountQuality.scope_model') }}</option><option value="account">{{ t('accountQuality.scope_account') }}</option></select></label>
      </div>
      <p class="text-xs text-gray-500">{{ t('accountQuality.automatic') }}: {{ t('accountQuality.off') }}</p>
    </form>
    <template #footer><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('accountQuality.cancel') }}</button><button type="submit" form="quality-assignment-form" data-test="quality-assignment-save" class="btn btn-primary" :disabled="saving || !selectedPolicy || (directSelection && !selectionReady)">{{ t('accountQuality.assignment') }}</button></template>
  </BaseDialog>
</template>
