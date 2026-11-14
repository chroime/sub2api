<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import QualityAccountSelector from './QualityAccountSelector.vue'
import api, { type Binding, type BindingOverrides, type Policy } from '@/api/admin/account-quality'
import accountsAPI from '@/api/admin/accounts'
import groupsAPI from '@/api/admin/groups'
import { CONCRETE_PLATFORM_OPTIONS } from '@/constants/platforms'
import type { AdminGroup } from '@/types'
import { qualityError } from './quality'

const props = defineProps<{ policies: Policy[]; accountIds?: number[]; policyId?: number }>()
const emit = defineEmits<{ close: []; saved: [number]; busy: [boolean]; changed: [] }>()
const { t } = useI18n()
const normalizeIds = (values: number[]) => [...new Set(values.filter(id => Number.isSafeInteger(id) && id > 0))]
const chosenIds = ref(normalizeIds(props.accountIds || []))
const policyId = ref(props.policyId || props.policies[0]?.id || 0), mode = ref<'single' | 'selected' | 'group' | 'platform'>(chosenIds.value.length <= 1 ? 'single' : 'selected'), group = ref(''), platform = ref('')
const groups = ref<AdminGroup[]>([]), saving = ref(false), error = ref(''), custom = ref(false)
const selectedPolicy = computed(() => props.policies.find(policy => policy.id === policyId.value))
const policyOptions = computed<SelectOption[]>(() => props.policies.map(policy => ({ value: policy.id, label: `${policy.name} · ${policy.model} · ${policy.platform}` })))
const modeOptions = computed<SelectOption[]>(() => [
  { value: 'single', label: t('accountQuality.singleAccount') },
  { value: 'selected', label: t('accountQuality.selectedAccounts') },
  { value: 'group', label: t('accountQuality.group') },
  { value: 'platform', label: t('accountQuality.platform') }
])
const groupOptions = computed<SelectOption[]>(() => [{ value: '', label: '—', disabled: true }, ...groups.value.map(item => ({ value: String(item.id), label: item.name }))])
const scopeOptions = computed<SelectOption[]>(() => [
  { value: '', label: t('accountQuality.policyDefaults') },
  { value: 'model', label: t('accountQuality.scope_model') },
  { value: 'account', label: t('accountQuality.scope_account') }
])
const directSelection = computed(() => mode.value === 'single' || mode.value === 'selected')
const boundAccountId = computed(() => mode.value === 'single' && chosenIds.value.length === 1 ? chosenIds.value[0]! : 0)
const bindings = ref<Binding[]>([]), bindingsLoading = ref(false), bindingsError = ref(''), notice = ref(''), unbindingId = ref(0)
let bindingsGeneration = 0
async function loadBindings() {
  const generation = ++bindingsGeneration, accountId = boundAccountId.value
  bindings.value = []; bindingsError.value = ''; notice.value = ''
  bindingsLoading.value = Boolean(accountId)
  if (!accountId) return
  try {
    const detail = await api.account(accountId)
    if (generation === bindingsGeneration) bindings.value = (detail.bindings || []).filter(binding => binding.account_id === accountId)
  } catch (cause) {
    if (generation === bindingsGeneration) bindingsError.value = qualityError(cause) || t('accountQuality.loadFailed')
  } finally { if (generation === bindingsGeneration) bindingsLoading.value = false }
}
async function unbind(binding: Binding) {
  if (saving.value || bindingsLoading.value || binding.account_id !== boundAccountId.value) return
  saving.value = true; unbindingId.value = binding.id; error.value = ''; notice.value = ''; emit('busy', true)
  try {
    await api.unbind(binding.id, binding.version)
    bindings.value = bindings.value.filter(item => item.id !== binding.id)
    notice.value = t('accountQuality.unboundSuccess', { policy: binding.policy_name })
    emit('changed')
  } catch (cause) { error.value = qualityError(cause) || t('accountQuality.saveFailed') }
  finally { saving.value = false; unbindingId.value = 0; emit('busy', false) }
}
watch(boundAccountId, loadBindings, { immediate: true })
onUnmounted(() => { bindingsGeneration++ })
const selection = ref({ valid: false, pending: false, platform: '', accountIds: [] as number[] })
const selectionReady = computed(() => selection.value.valid && !selection.value.pending && selection.value.platform === selectedPolicy.value?.platform && selection.value.accountIds.join(',') === chosenIds.value.join(',') && (mode.value !== 'single' || chosenIds.value.length === 1))
const platformOptions = computed(() => CONCRETE_PLATFORM_OPTIONS.filter(item => item.value === selectedPolicy.value?.platform))
const targetPlatformOptions = computed<SelectOption[]>(() => [{ value: '', label: '—', disabled: true }, ...platformOptions.value])
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
  <BaseDialog :show="true" :title="t(boundAccountId ? 'accountQuality.assignPolicy' : 'accountQuality.assignment')" width="normal" :close-on-escape="!saving" :show-close-button="!saving" @close="emit('close')">
    <form id="quality-assignment-form" class="space-y-4" @submit.prevent="save">
      <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
      <p v-if="notice" role="status" class="text-sm text-emerald-600 dark:text-emerald-400">{{ notice }}</p>
      <section v-if="boundAccountId" data-test="quality-bound-policies" class="space-y-2">
        <div class="flex items-center justify-between gap-3">
          <h4 class="text-sm font-medium">{{ t('accountQuality.boundPolicies') }}</h4>
          <button type="button" class="text-xs text-primary-600 hover:underline disabled:opacity-50 dark:text-primary-400" :disabled="saving || bindingsLoading" @click="loadBindings">{{ t('accountQuality.refresh') }}</button>
        </div>
        <p v-if="bindingsLoading" role="status" class="text-sm text-gray-500">{{ t('accountQuality.loading') }}</p>
        <p v-else-if="bindingsError" role="alert" class="text-sm text-red-600">{{ bindingsError }}</p>
        <div v-else-if="bindings.length" class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-600 dark:border-dark-600">
          <div v-for="binding in bindings" :key="binding.id" class="flex items-center justify-between gap-3 px-3 py-2.5">
            <div class="min-w-0 text-sm"><p class="truncate font-medium" :title="binding.policy_name">{{ binding.policy_name }}</p><p class="truncate text-xs text-gray-500" :title="binding.model">{{ binding.model }}</p></div>
            <button type="button" :data-test="`quality-unbind-${binding.id}`" class="shrink-0 rounded-lg px-3 py-2 text-sm font-medium text-red-600 hover:bg-red-50 disabled:opacity-50 dark:text-red-400 dark:hover:bg-red-900/20" :aria-label="t('accountQuality.unbindPolicy', { policy: binding.policy_name })" :disabled="saving || bindingsLoading" @click="unbind(binding)">{{ t(unbindingId === binding.id ? 'accountQuality.unbinding' : 'accountQuality.unbind') }}</button>
          </div>
        </div>
        <p v-else class="text-sm text-gray-500">{{ t('accountQuality.noBoundPolicies') }}</p>
      </section>
      <p class="text-sm leading-relaxed text-gray-500">{{ t('accountQuality.assignmentHint') }}</p>
      <div class="text-sm"><label for="quality-assignment-policy" class="mb-1 block">{{ t('accountQuality.policy') }}</label><Select id="quality-assignment-policy" data-test="quality-assignment-policy" :model-value="policyId || null" :options="policyOptions" searchable :disabled="saving || !policies.length" :aria-label="t('accountQuality.policy')" :placeholder="t('accountQuality.choosePolicy')" :search-placeholder="t('accountQuality.searchPolicy')" @update:model-value="policyId = typeof $event === 'number' ? $event : 0" /></div>
      <p class="text-xs text-gray-500">{{ t('accountQuality.assignmentPlatformHint') }}</p>
      <label class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.assignmentMode') }}</span><Select v-model="mode" data-test="quality-assignment-mode" :options="modeOptions" :searchable="false" :disabled="saving" /></label>
      <div v-if="directSelection" class="text-sm"><p class="mb-1">{{ t(mode === 'single' ? 'accountQuality.singleAccount' : 'accountQuality.selectedAccounts') }}</p><QualityAccountSelector v-model="chosenIds" :platform="selectedPolicy?.platform || ''" :multiple="mode === 'selected'" :disabled="saving" @validation="selection = $event" /></div>
      <label v-else-if="mode === 'group'" class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.group') }}</span><Select v-model="group" data-test="quality-assignment-group" :options="groupOptions" :searchable="false" :disabled="saving" /></label>
      <label v-else class="block text-sm"><span class="mb-1 block">{{ t('accountQuality.platform') }}</span><Select v-model="platform" data-test="quality-assignment-platform" :options="targetPlatformOptions" :searchable="false" :disabled="saving" /></label>
      <label class="flex items-center gap-2 text-sm"><input v-model="custom" :disabled="saving" type="checkbox" class="rounded" />{{ t('accountQuality.overrides') }}</label>
      <div v-if="custom" class="grid gap-3 rounded-lg bg-gray-50 p-3 dark:bg-dark-900 sm:grid-cols-2">
        <p class="text-xs text-gray-500 sm:col-span-2">{{ t('accountQuality.overrideHint') }}</p>
        <label v-for="field in [{ key: 'interval_seconds', label: 'interval' }, { key: 'failure_threshold', label: 'failureThreshold' }, { key: 'recovery_threshold', label: 'recoveryThreshold' }] as const" :key="field.key" class="text-sm"><span class="mb-1 block">{{ t(`accountQuality.${field.label}`) }}</span><input v-model.number="overrides[field.key]" class="input w-full" type="number" min="1" step="1" /></label>
        <label class="text-sm"><span class="mb-1 block">{{ t('accountQuality.scope') }}</span><Select v-model="overrides.isolation_scope" :options="scopeOptions" :searchable="false" :disabled="saving" /></label>
      </div>
      <p class="text-xs text-gray-500">{{ t('accountQuality.automatic') }}: {{ t('accountQuality.off') }}</p>
    </form>
    <template #footer><button type="button" class="btn btn-secondary" :disabled="saving" @click="emit('close')">{{ t('accountQuality.cancel') }}</button><button type="submit" form="quality-assignment-form" data-test="quality-assignment-save" class="btn btn-primary" :disabled="saving || !selectedPolicy || (directSelection && !selectionReady)">{{ t('accountQuality.assignment') }}</button></template>
  </BaseDialog>
</template>
